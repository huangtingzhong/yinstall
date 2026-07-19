package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// stepBackupSeed merges the legacy MSH-105 (primary backup) and MSH-106
// (secondary restore+fetch) into a single round-robin step. Each host runs
// only its half: primary backs up user databases; secondary fetches the
// backup from primary admin share and restores WITH NORECOVERY.
func stepBackupSeed() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Backup and Restore Seed",
		Description: "Full backup on primary, fetch + restore WITH NORECOVERY on secondary",
		Tags:        []string{"mssql-ha", "mirror", "backup", "restore"},
		PreCheck: func(ctx *runner.StepContext) error {
			if commonmssql.MirrorSkipSeed(ctx) {
				return runner.NewStepSkippedError("M-012: --mirror-skip-seed")
			}
			if commonmssql.IsPrimaryHost(ctx) {
				return m012PreCheckBackup(ctx)
			}
			if commonmssql.IsSecondaryHost(ctx) {
				return m012PreCheckRestore(ctx)
			}
			return runner.NewStepSkippedError("M-012: not primary or secondary")
		},
		Action: func(ctx *runner.StepContext) error {
			if commonmssql.MirrorSkipSeed(ctx) {
				return runner.NewStepSkippedError("M-012: --mirror-skip-seed")
			}
			if commonmssql.IsPrimaryHost(ctx) {
				return m012BackupPrimary(ctx)
			}
			if commonmssql.IsSecondaryHost(ctx) {
				return m012RestoreSecondary(ctx)
			}
			return runner.NewStepSkippedError("M-012: not primary or secondary")
		},
	}
}

func m012PreCheckBackup(ctx *runner.StepContext) error {
	dbs, err := ensureMirrorTargetDBs(ctx)
	if err != nil {
		return err
	}
	if allMirrorDBsMatch(ctx, dbs, func(db string) bool { return mirrorDBSynchronized(ctx, db) }) {
		return runner.NewStepSkippedError("M-012: mirroring already established for all target databases")
	}
	for _, db := range dbs {
		if mirrorDBSynchronized(ctx, db) {
			continue
		}
		st, err := queryMirrorDBStatus(ctx, db)
		if err != nil {
			return err
		}
		if err := commonmssql.ValidatePrimaryMirrorSeed(st); err != nil {
			return err
		}
	}
	return nil
}

func m012PreCheckRestore(ctx *runner.StepContext) error {
	dbs, err := ensureMirrorTargetDBs(ctx)
	if err != nil {
		return err
	}
	if allMirrorDBsMatch(ctx, dbs, func(db string) bool { return mirrorDBConfigured(ctx, db) }) {
		return runner.NewStepSkippedError("M-012: mirroring already configured for all target databases on secondary")
	}
	force := ctx.IsForceStep() || ctx.ForceAll
	for _, db := range dbs {
		if mirrorDBConfigured(ctx, db) {
			continue
		}
		st, err := queryMirrorDBStatus(ctx, db)
		if err != nil {
			return err
		}
		if err := commonmssql.ValidateSecondaryMirrorRestore(st, commonmssql.MirrorDropExistingSecondary(ctx), force); err != nil {
			return err
		}
	}
	return nil
}

func m012BackupPrimary(ctx *runner.StepContext) error {
	if err := discoverMirrorWorkDir(ctx); err != nil {
		return err
	}
	dbs, err := ensureMirrorTargetDBs(ctx)
	if err != nil {
		return err
	}
	backupDir := commonmssql.MirrorBackupBaseDir(ctx)
	bkEntry, _ := commonmssql.EnsureInstanceResolved(ctx)
	sqlAccount := commonmssql.SQLServiceAccountName(bkEntry.Name)
	mkdir := fmt.Sprintf(`powershell -NoProfile -Command "$d='%s'; New-Item -ItemType Directory -Force -Path $d | Out-Null; icacls $d /grant '%s:(OI)(CI)F' 2>$null | Out-Null"`,
		strings.ReplaceAll(backupDir, "'", "''"), sqlAccount)
	if !ctx.DryRun && !ctx.Precheck {
		if _, err := ctx.ExecuteWithCheck(mkdir, false); err != nil {
			return err
		}
	}
	for _, db := range dbs {
		if mirrorDBSynchronized(ctx, db) {
			ctx.Logger.Info("M-012: skip backup for %s (mirroring already established)", db)
			continue
		}
		ts := commonmssql.MirrorBackupTimestamp()
		backupPath := commonmssql.MirrorNewBackupPath(ctx, db, ts)
		mshLogPhase(ctx, "mirror-backup-start", db+" -> "+backupPath)
		if err := commonmssql.RunSqlcmdQueries(ctx, "M-012 backup "+db, []string{commonmssql.BackupMirrorDBSQL(db, backupPath)}); err != nil {
			return err
		}
		ctx.SetResult(commonmssql.MirrorBackupPathResultKey(db), backupPath)
		mshLogPhase(ctx, "mirror-backup-done", backupPath)
	}
	return nil
}

func m012RestoreSecondary(ctx *runner.StepContext) error {
	if err := discoverMirrorWorkDir(ctx); err != nil {
		return err
	}
	dbs, err := ensureMirrorTargetDBs(ctx)
	if err != nil {
		return err
	}
	primary := commonmssql.ResolvePrimaryHost(ctx)
	user := commonmssql.HAAdminUser(ctx, primary)
	pass := commonmssql.HAAdminPassword(ctx, primary)
	entry, _ := commonmssql.EnsureInstanceResolved(ctx)

	for _, db := range dbs {
		if mirrorDBConfigured(ctx, db) {
			ctx.Logger.Info("M-012: skip restore for %s (already configured for mirroring)", db)
			continue
		}
		localBackup, remoteBackup, skipFetch := commonmssql.MirrorRestoreSource(ctx, db)
		mshLogPhase(ctx, "mirror-restore-start", db+" <- "+localBackup)

		if commonmssql.MirrorDropExistingSecondary(ctx) && (ctx.IsForceStep() || ctx.ForceAll) {
			st, err := queryMirrorDBStatus(ctx, db)
			if err != nil {
				return err
			}
			if st.Exists && !st.IsMirroring() && !st.IsRestoring() {
				ctx.Logger.Warn("M-012: dropping existing database %s on secondary only (primary untouched)", db)
				dropSQL := commonmssql.DropMirrorSecondaryDBSQL(db)
				if err := commonmssql.RunSqlcmdQueries(ctx, "M-012 drop secondary db "+db, []string{dropSQL}); err != nil {
					return err
				}
			}
		}

		if ctx.DryRun || ctx.Precheck {
			continue
		}
		if !skipFetch {
			sqlAccount := commonmssql.SQLServiceAccountName(entry.Name)
			if err := commonmssql.FetchBackupFromPrimary(ctx, "M-012 fetch backup from primary "+db, localBackup, remoteBackup, primary, user, pass, sqlAccount); err != nil {
				return err
			}
		} else {
			check := fmt.Sprintf(`powershell -NoProfile -Command "if (-not (Test-Path -LiteralPath '%s')) { throw 'mirror restore source not found: %s' }"`,
				strings.ReplaceAll(localBackup, "'", "''"), strings.ReplaceAll(localBackup, "'", "''"))
			ctx.LogScriptPreview("powershell", "M-012 verify restore source "+db, check)
			if _, err := ctx.ExecuteWithCheck(check, false); err != nil {
				return err
			}
		}

		if err := commonmssql.RestoreDBNorecoveryWithMove(ctx, "M-012 restore "+db, db, localBackup); err != nil {
			return err
		}
		mshLogPhase(ctx, "mirror-restore-done", db)
	}
	return nil
}
