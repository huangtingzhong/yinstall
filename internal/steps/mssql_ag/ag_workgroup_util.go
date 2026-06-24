package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func discoverHAWorkDir(ctx *runner.StepContext) error {
	hostKey := commonmssql.HAHostKey(ctx.Executor.Host())
	if v, ok := ctx.Results[commonmssql.HAWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
		return nil
	}
	if wd := strings.TrimSpace(ctx.GetParamString("mirror_work_dir", "")); wd != "" {
		commonmssql.SetHAWorkDir(ctx, hostKey, wd)
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		work := commonmssql.HAWorkDir(ctx)
		commonmssql.SetHAWorkDir(ctx, hostKey, work)
		return nil
	}
	cmd := commonmssql.SqlcmdQueryCommand(ctx, commonmssql.DiscoverHAWorkDirSQL())
	res, err := ctx.Execute(cmd, false)
	if err != nil || res == nil || res.GetExitCode() != 0 {
		work := commonmssql.DefaultHAWorkDirFallback()
		commonmssql.SetHAWorkDir(ctx, hostKey, work)
		return nil
	}
	base, err := commonmssql.ParseHAWorkDirFromSqlcmd(res.GetStdout())
	if err != nil {
		work := commonmssql.DefaultHAWorkDirFallback()
		commonmssql.SetHAWorkDir(ctx, hostKey, work)
		return nil
	}
	work := commonmssql.JoinWinPath(base, commonmssql.HAWorkSubdir)
	commonmssql.SetHAWorkDir(ctx, hostKey, work)
	return nil
}

func haCertLocalReady(ctx *runner.StepContext, kind commonmssql.HAEndpointKind) (bool, string, error) {
	if ctx.DryRun || ctx.Precheck {
		return false, "", nil
	}
	if any, err := commonmssql.AnyAGDatabaseReplicaActive(ctx); err != nil {
		return false, "", err
	} else if any && !ctx.IsForceStep() {
		return true, "HADR infrastructure already in use (existing AG database replica)", nil
	}
	if ctx.IsForceStep() && commonmssql.ShouldBypassHACertSkip(ctx) {
		return false, "", nil
	}
	hostKey := commonmssql.HAHostKey(ctx.Executor.Host())
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "HADR local cert ready", commonmssql.LocalCertReadySQL(kind, hostKey))
	if err != nil {
		return false, "", err
	}
	if !commonmssql.ParseSqlcmdBoolScalar(stdout) {
		return false, "", nil
	}
	stdout, err = commonmssql.QuerySqlcmdScalar(ctx, "HADR endpoint ready", commonmssql.EndpointReadySQL(kind))
	if err != nil {
		return false, "", err
	}
	if !commonmssql.ParseSqlcmdBoolScalar(stdout) {
		return false, "", nil
	}
	matches, err := haLocalCertMatchesExport(ctx, kind, hostKey)
	if err != nil {
		return false, "", err
	}
	if matches {
		return true, "local HADR cert and endpoint already configured", nil
	}
	return false, "", nil
}

func haPartnerTrustReady(ctx *runner.StepContext, kind commonmssql.HAEndpointKind, partnerKey string) (bool, string, error) {
	if ctx.DryRun || ctx.Precheck {
		return false, "", nil
	}
	partnerKey = strings.TrimSpace(partnerKey)
	if partnerKey == "" {
		return false, "", nil
	}
	matches, err := partnerCertTrustMatchesShare(ctx, kind, partnerKey, "HADR partner trust")
	if err != nil {
		return false, "", err
	}
	if matches {
		return true, "partner HADR certificate thumbprint matches published share cert", nil
	}

	// Distinguish "cert not found" (new partner, safe to exchange) from
	// "cert exists but thumbprint mismatch" (existing partner changed,
	// requires force to avoid breaking an active AG).
	certName := commonmssql.HACertName(kind, partnerKey)
	loginName := commonmssql.HALoginName(kind, partnerKey)
	dbThumb, _ := certThumbprintFromSQL(ctx, "HADR partner trust db", certName)
	hasLogin, _ := partnerLoginExists(ctx, loginName)
	certKnown := hasLogin && dbThumb != ""

	any, err := commonmssql.AnyAGDatabaseReplicaActive(ctx)
	if err != nil {
		return false, "", err
	}
	if any && certKnown && !ctx.IsForceStep() {
		return false, "", commonmssql.ForceHaCertsRequiredError("A-008")
	}
	return false, "", nil
}

// dbAlreadyInAG reports whether the given database is already a member of any
// availability group on the local SQL instance (is_local=1). Used by A-014
// to skip restore/add-manual when the secondary already has the database.
func dbAlreadyInAG(ctx *runner.StepContext, dbName string) bool {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return false
	}
	dbName = SQLQuote(dbName)
	ag := commonmssql.AGName(ctx)
	query := fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.dm_hadr_database_replica_states drs
  JOIN sys.databases d ON d.database_id = drs.database_id
  JOIN sys.availability_groups g ON g.group_id = drs.group_id
  WHERE d.name = N'%s' AND drs.is_local = 1 AND g.name = N'%s'
) THEN 1 ELSE 0 END;`, dbName, SQLQuote(ag))
	out, err := commonmssql.QuerySqlcmdScalarOptional(ctx, "A-014 db already in AG", query)
	if err != nil || out == "" {
		return false
	}
	return commonmssql.SqlcmdScalarIsOne(out)
}

// SQLQuote escapes a name for use in a SQL N'...' literal.
func SQLQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

func a014ManualBackupPrimary(ctx *runner.StepContext, dbs []string) error {
	if !commonmssql.IsPrimaryHost(ctx) {
		return runner.NewStepSkippedError("A-014 backup runs on primary only")
	}
	if err := discoverHAWorkDir(ctx); err != nil {
		return err
	}
	backupDir := commonmssql.HAWorkDir(ctx)
	entry, _ := commonmssql.EnsureInstanceResolved(ctx)
	mkdir := commonmssql.BackupDirMkdirPowerShell(backupDir, entry.Name)
	if !ctx.DryRun && !ctx.Precheck {
		if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+mkdir+`"`, false); err != nil {
			return err
		}
	}
	for _, db := range dbs {
		ts := commonmssql.MirrorBackupTimestamp()
		backupPath := commonmssql.JoinWinPath(backupDir, commonmssql.MirrorBackupFileName(db, ts))
		if err := commonmssql.RunSqlcmdQueries(ctx, "A-014 backup "+db, []string{commonmssql.BackupMirrorDBSQL(db, backupPath)}); err != nil {
			return err
		}
		ctx.SetResult(commonmssql.MirrorBackupPathResultKey(db), backupPath)
	}
	return nil
}

func a014ManualRestoreSecondary(ctx *runner.StepContext, dbs []string) error {
	if !commonmssql.IsSecondaryHost(ctx) {
		return runner.NewStepSkippedError("A-014 restore runs on secondary only")
	}
	if !commonmssql.IsListedReplicaHost(ctx) {
		ctx.Logger.Info("A-014: skip restore on %s (not in -t; existing AG member)", commonmssql.TargetHost(ctx))
		return nil
	}
	for _, db := range dbs {
		// Idempotent: skip if this secondary already has the database in the AG.
		if dbAlreadyInAG(ctx, db) {
			ctx.Logger.Info("A-014: skip restore %s (already in AG on this secondary)", db)
			continue
		}
		if err := discoverHAWorkDir(ctx); err != nil {
			return err
		}
		primary := commonmssql.ResolvePrimaryHost(ctx)
		user := commonmssql.HAAdminUser(ctx, primary)
		pass := commonmssql.HAAdminPassword(ctx, primary)
		entry, _ := commonmssql.EnsureInstanceResolved(ctx)
		localBackup, remoteBackup, skipFetch := commonmssql.MirrorRestoreSource(ctx, db)
		if ctx.DryRun || ctx.Precheck {
			continue
		}
		if !skipFetch {
			sqlAccount := commonmssql.SQLServiceAccountName(entry.Name)
			if err := commonmssql.FetchBackupFromPrimary(ctx, "A-014 fetch backup "+db, localBackup, remoteBackup, primary, user, pass, sqlAccount); err != nil {
				return err
			}
		}
		if err := commonmssql.RestoreDBNorecoveryWithMove(ctx, "A-014 restore "+db, db, localBackup); err != nil {
			return err
		}
	}
	return nil
}

func a014ManualLogChain(ctx *runner.StepContext, dbs []string, phase string) error {
	switch phase {
	case "log-backup":
		if !commonmssql.IsPrimaryHost(ctx) {
			return runner.NewStepSkippedError("A-014 log-backup primary only")
		}
		for _, db := range dbs {
			full := commonmssql.MirrorPrimaryBackupPath(ctx, db)
			logPath := commonmssql.MirrorLogBackupPathFromFull(full)
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-014 log backup "+db, []string{commonmssql.BackupMirrorLogSQL(db, logPath)}); err != nil {
				return err
			}
			ctx.SetResult(commonmssql.MirrorLogBackupPathResultKey(db), logPath)
		}
	case "log-restore-secondary":
		if !commonmssql.IsSecondaryHost(ctx) {
			return runner.NewStepSkippedError("A-014 log-restore secondary only")
		}
		if !commonmssql.IsListedReplicaHost(ctx) {
			ctx.Logger.Info("A-014: skip log restore on %s (not in -t; existing AG member)", commonmssql.TargetHost(ctx))
			return nil
		}
		primary := commonmssql.ResolvePrimaryHost(ctx)
		user := commonmssql.HAAdminUser(ctx, primary)
		pass := commonmssql.HAAdminPassword(ctx, primary)
		entry, _ := commonmssql.EnsureInstanceResolved(ctx)
		for _, db := range dbs {
			if dbAlreadyInAG(ctx, db) {
				ctx.Logger.Info("A-014: skip log restore %s (already in AG on this secondary)", db)
				continue
			}
			localLog, remoteLog := commonmssql.MirrorLogRestoreSource(ctx, db)
			if !ctx.DryRun && !ctx.Precheck {
				sqlAccount := commonmssql.SQLServiceAccountName(entry.Name)
				if err := commonmssql.FetchBackupFromPrimary(ctx, "A-014 fetch log "+db, localLog, remoteLog, primary, user, pass, sqlAccount); err != nil {
					return err
				}
			}
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-014 log restore "+db, []string{commonmssql.RestoreMirrorLogSQL(db, localLog)}); err != nil {
				return err
			}
		}
	}
	return nil
}
