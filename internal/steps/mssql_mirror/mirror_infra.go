package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func mirrorInfraLocalReady(ctx *runner.StepContext) (bool, string, error) {
	if any, err := mirrorAnyDatabaseMirroring(ctx); err != nil {
		return false, "", err
	} else if any && !ctx.IsForceStep() {
		return true, "mirror infrastructure already in use (existing mirrored database)", nil
	}
	if ctx.IsForceStep() && commonmssql.ShouldBypassHACertSkip(ctx) {
		return false, "", nil
	}
	if ctx.DryRun || ctx.Precheck {
		return false, "", nil
	}
	hostKey := commonmssql.MirrorHostKey(ctx.Executor.Host())
	endOut, err := commonmssql.QuerySqlcmdScalar(ctx, "mirror infra endpoint", commonmssql.MirrorEndpointReadySQL())
	if err != nil {
		return false, "", err
	}
	certOut, err := commonmssql.QuerySqlcmdScalar(ctx, "mirror infra local cert", commonmssql.MirrorLocalCertReadySQL(hostKey))
	if err != nil {
		return false, "", err
	}
	endStarted := commonmssql.ParseSqlcmdBoolScalar(endOut)
	hasCert := commonmssql.ParseSqlcmdBoolScalar(certOut)
	if !endStarted || !hasCert {
		return false, "", nil
	}
	matchesExport, err := haLocalCertMatchesExport(ctx, commonmssql.HAEndpointMirror, hostKey)
	if err != nil {
		return false, "", err
	}
	if !matchesExport {
		return false, "", nil
	}
	partnerKey := commonmssql.MirrorHostKey(commonmssql.MirrorPartnerHost(ctx))
	if partnerKey != "" {
		trust, err := partnerCertTrustMatchesShare(ctx, commonmssql.HAEndpointMirror, partnerKey, "M-008")
		if err != nil {
			return false, "", err
		}
		if trust {
			return true, "mirror certificate, endpoint, and partner trust already configured", nil
		}
	}
	return true, "mirror endpoint and local certificate already configured", nil
}

func mirrorPartnerTrustReady(ctx *runner.StepContext, partnerKey string) (bool, string, error) {
	partnerKey = strings.TrimSpace(partnerKey)
	if partnerKey == "" {
		return false, "", nil
	}
	if ctx.DryRun || ctx.Precheck {
		return false, "", nil
	}
	matches, err := partnerCertTrustMatchesShare(ctx, commonmssql.HAEndpointMirror, partnerKey, "mirror partner trust")
	if err != nil {
		return false, "", err
	}
	if matches {
		return true, "partner certificate thumbprint matches published share cert", nil
	}
	any, err := mirrorAnyDatabaseMirroring(ctx)
	if err != nil {
		return false, "", err
	}
	if any && !ctx.IsForceStep() {
		certName := commonmssql.HACertName(commonmssql.HAEndpointMirror, partnerKey)
		loginName := commonmssql.HALoginName(commonmssql.HAEndpointMirror, partnerKey)
		hasLogin, err := partnerLoginExists(ctx, loginName)
		dbThumb, err2 := certThumbprintFromSQL(ctx, "mirror partner trust db", certName)
		if err == nil && err2 == nil && hasLogin && dbThumb != "" {
			return true, "partner trust established (mirroring active)", nil
		}
		return false, "", commonmssql.ForceHaCertsRequiredError("M-010")
	}
	return false, "", nil
}

func mirrorCertPublishedToPartnerShare(ctx *runner.StepContext, selfKey, partnerKey string) (bool, error) {
	if ctx.DryRun || ctx.Precheck {
		return false, nil
	}
	if err := discoverMirrorWorkDir(ctx); err != nil {
		return false, err
	}
	partnerShareCert := commonmssql.AdminShareMirrorCertPath(ctx, partnerKey, selfKey)
	user := commonmssql.HAAdminUser(ctx, partnerKey)
	pass := commonmssql.HAAdminPassword(ctx, partnerKey)
	partnerUNC := commonmssql.AdminShareUNC(partnerKey)
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	cmd := fmt.Sprintf(`powershell -NoProfile -Command "$p='%s'; $u='%s'; $pw='%s'; $f='%s'; if ($pw) { net use $p /user:$u $pw 2>$null | Out-Null }; if ((Test-Path -LiteralPath $f) -and ((Get-Item -LiteralPath $f).Length -gt 0)) { '1' } else { '0' }"`,
		esc(partnerUNC), esc(user), esc(pass), esc(partnerShareCert))
	res, err := ctx.Execute(cmd, false)
	if err != nil || res == nil {
		return false, err
	}
	return strings.Contains(strings.TrimSpace(res.GetStdout()), "1"), nil
}

func mirrorCertPublishReady(ctx *runner.StepContext) (bool, string, error) {
	if any, err := mirrorAnyDatabaseMirroring(ctx); err != nil {
		return false, "", err
	} else if any && !ctx.IsForceStep() {
		return true, "certificate already published (existing mirrored database)", nil
	}
	localReady, _, err := mirrorInfraLocalReady(ctx)
	if err != nil {
		return false, "", err
	}
	if !localReady {
		return false, "", nil
	}
	selfKey := commonmssql.MirrorHostKey(ctx.Executor.Host())
	partnerKey := commonmssql.MirrorHostKey(commonmssql.MirrorPartnerHost(ctx))
	if partnerKey == "" || strings.EqualFold(selfKey, partnerKey) {
		return false, "", nil
	}
	matches, err := selfCertMatchesOnPartnerShare(ctx, selfKey, partnerKey)
	if err != nil {
		return false, "", err
	}
	if matches {
		return true, "local certificate thumbprint matches partner admin share", nil
	}
	return false, "", nil
}

func m013LogBackup(ctx *runner.StepContext, dbs []string) error {
	if !commonmssql.IsPrimaryHost(ctx) {
		return runner.NewStepSkippedError("M-013 log-backup runs on primary only")
	}
	if commonmssql.MirrorSkipSeed(ctx) {
		return runner.NewStepSkippedError("M-013 log-backup: --mirror-skip-seed")
	}
	if err := discoverMirrorWorkDir(ctx); err != nil {
		return err
	}
	for _, db := range dbs {
		if mirrorDBSynchronized(ctx, db) {
			ctx.Logger.Info("M-013: skip log backup for %s (mirroring already established)", db)
			continue
		}
		logPath := commonmssql.MirrorPrimaryLogBackupPath(ctx, db)
		if logPath == "" {
			return fmt.Errorf("M-013: cannot resolve log backup path for %s (run M-012 first)", db)
		}
		mshLogPhase(ctx, "mirror-log-backup-start", db+" -> "+logPath)
		if ctx.DryRun || ctx.Precheck {
			continue
		}
		if err := commonmssql.RunSqlcmdQueries(ctx, "M-013 log backup "+db, []string{commonmssql.BackupMirrorLogSQL(db, logPath)}); err != nil {
			return err
		}
		ctx.SetResult(commonmssql.MirrorLogBackupPathResultKey(db), logPath)
		mshLogPhase(ctx, "mirror-log-backup-done", logPath)
	}
	return nil
}

func m013LogRestorePartnerSecondary(ctx *runner.StepContext, dbs []string, partnerAddr string) error {
	if !commonmssql.IsSecondaryHost(ctx) {
		return runner.NewStepSkippedError("M-013 log-restore-partner-secondary runs on secondary only")
	}
	if err := discoverMirrorWorkDir(ctx); err != nil {
		return err
	}
	primary := commonmssql.ResolvePrimaryHost(ctx)
	user := commonmssql.HAAdminUser(ctx, primary)
	pass := commonmssql.HAAdminPassword(ctx, primary)
	entry, _ := commonmssql.EnsureInstanceResolved(ctx)

	for _, db := range dbs {
		if mirrorDBSynchronized(ctx, db) {
			ctx.Logger.Info("M-013: skip secondary partner for %s (mirroring already established)", db)
			continue
		}
		if !commonmssql.MirrorSkipSeed(ctx) {
			localLog, remoteLog := commonmssql.MirrorLogRestoreSource(ctx, db)
			if localLog == "" || remoteLog == "" {
				return fmt.Errorf("M-013: missing log backup path for %s (run M-013 log-backup on primary first)", db)
			}
			mshLogPhase(ctx, "mirror-log-restore-start", db+" <- "+localLog)
			if !ctx.DryRun && !ctx.Precheck {
				sqlAccount := commonmssql.SQLServiceAccountName(entry.Name)
				if err := commonmssql.FetchBackupFromPrimary(ctx, "M-013 fetch log backup from primary "+db, localLog, remoteLog, primary, user, pass, sqlAccount); err != nil {
					return err
				}
				if err := commonmssql.RunSqlcmdQueries(ctx, "M-013 restore log "+db, []string{commonmssql.RestoreMirrorLogSQL(db, localLog)}); err != nil {
					return err
				}
			}
			mshLogPhase(ctx, "mirror-log-restore-done", db)
		}
		mshLogPhase(ctx, "mirror-partner-secondary", db+" -> "+partnerAddr)
		if ctx.DryRun || ctx.Precheck {
			continue
		}
		if err := commonmssql.RunSqlcmdQueries(ctx, "M-013 set partner secondary "+db, []string{commonmssql.SetMirrorPartnerSQL(db, partnerAddr)}); err != nil {
			return err
		}
	}
	return nil
}

func m013PartnerPrimary(ctx *runner.StepContext, dbs []string, partnerAddr string) error {
	if !commonmssql.IsPrimaryHost(ctx) {
		return runner.NewStepSkippedError("M-013 partner-primary runs on primary only")
	}
	for _, db := range dbs {
		if mirrorDBSynchronized(ctx, db) {
			ctx.Logger.Info("M-013: skip primary partner for %s (mirroring already established)", db)
			continue
		}
		mshLogPhase(ctx, "mirror-partner-primary", db+" -> "+partnerAddr)
		if ctx.DryRun || ctx.Precheck {
			continue
		}
		if err := commonmssql.RunSqlcmdQueries(ctx, "M-013 set partner primary "+db, []string{commonmssql.SetMirrorPartnerSQL(db, partnerAddr)}); err != nil {
			return err
		}
		verify := commonmssql.VerifyMirrorSQL(db)
		if err := commonmssql.RunSqlcmdQueries(ctx, "M-013 verify "+db, []string{verify}); err != nil {
			return err
		}
		mshLogPhase(ctx, "mirror-verify-done", db)
	}
	return nil
}
