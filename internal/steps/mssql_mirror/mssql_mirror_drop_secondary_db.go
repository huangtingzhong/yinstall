package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepDropSecondaryDb() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Drop Secondary DB",
		Description: "Drop former mirror database(s) on secondary after partner off",
		Tags:        []string{"mssql-ha", "mirror", "remove", "drop"},
		Dangerous:   true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsSecondaryHost(ctx) {
				return runner.NewStepSkippedError("M-054 runs on secondary only")
			}
			if !commonmssql.MirrorDropSecondaryDB(ctx) {
				return runner.NewStepSkippedError("M-054: --mirror-drop-secondary-db not set")
			}
			dbs, err := ensureMirrorRemoveDBs(ctx)
			if err != nil {
				return err
			}
			pending := 0
			for _, db := range dbs {
				st, err := queryMirrorDBStatus(ctx, db)
				if err != nil {
					return err
				}
				if st.Exists {
					pending++
				}
			}
			if pending == 0 {
				return runner.NewStepSkippedError("M-054: no target databases on secondary to drop")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if !commonmssql.IsSecondaryHost(ctx) {
				return runner.NewStepSkippedError("M-054 runs on secondary only")
			}
			dbs, err := ensureMirrorRemoveDBs(ctx)
			if err != nil {
				return err
			}
			for _, db := range dbs {
				st, err := queryMirrorDBStatus(ctx, db)
				if err != nil {
					return err
				}
				if !st.Exists {
					ctx.Logger.Info("M-054: skip %s (not on secondary)", db)
					continue
				}
				mshLogPhase(ctx, "mirror-drop-secondary-start", db)
				if err := dropMirrorSecondaryDB(ctx, ctx.CurrentStepID, db); err != nil {
					return err
				}
				mshLogPhase(ctx, "mirror-drop-secondary-done", db)
			}
			return nil
		},
	}
}
