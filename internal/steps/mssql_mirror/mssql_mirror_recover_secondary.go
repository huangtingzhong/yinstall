package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepRecoverSecondary() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Recover Secondary DB",
		Description: "RESTORE DATABASE WITH RECOVERY on secondary after partner off",
		Tags:        []string{"mssql-ha", "mirror", "remove", "recover"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsSecondaryHost(ctx) {
				return runner.NewStepSkippedError("M-053 runs on secondary only")
			}
			if commonmssql.MirrorDropSecondaryDB(ctx) {
				return runner.NewStepSkippedError("M-053: --mirror-drop-secondary-db set (drop replaces recover)")
			}
			if !ctx.GetParamBool("mirror_recover_secondary", true) {
				return runner.NewStepSkippedError("M-053: --mirror-recover-secondary=false")
			}
			dbs, err := ensureMirrorRemoveDBs(ctx)
			if err != nil {
				return err
			}
			pending := 0
			for _, db := range dbs {
				if mirrorDBRestoring(ctx, db) {
					pending++
				}
			}
			if pending == 0 {
				return runner.NewStepSkippedError("M-053: no target databases in RESTORING state")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			dbs, err := ensureMirrorRemoveDBs(ctx)
			if err != nil {
				return err
			}
			for _, db := range dbs {
				if !mirrorDBRestoring(ctx, db) {
					ctx.Logger.Info("M-053: skip %s (not in RESTORING state)", db)
					continue
				}
				mshLogPhase(ctx, "mirror-recover-start", db)
				sql := commonmssql.RecoverMirroredDBSQL(db)
				if err := commonmssql.RunSqlcmdQueries(ctx, "M-053 recover secondary "+db, []string{sql}); err != nil {
					return err
				}
				mshLogPhase(ctx, "mirror-recover-done", db)
			}
			return nil
		},
	}
}
