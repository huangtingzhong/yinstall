package mssql_ag

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepDropSecondaryDb() *runner.Step {
	return &runner.Step{
		Name:        "AG Drop Secondary DB",
		Description: "Drop former AG database(s) on secondary after availability group removed",
		Tags:        []string{"mssql-ha", "ag", "remove", "drop"},
		Dangerous:   true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsSecondaryHost(ctx) {
				return runner.NewStepSkippedError("A-053 runs on secondary only")
			}
			if !commonmssql.MirrorDropSecondaryDB(ctx) {
				return runner.NewStepSkippedError("A-053: --ag-drop-secondary-db not set")
			}
			dbs, err := ensureAGRemoveDBs(ctx)
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
				return runner.NewStepSkippedError("A-053: no target databases on secondary to drop")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if !commonmssql.IsSecondaryHost(ctx) {
				return runner.NewStepSkippedError("A-053 runs on secondary only")
			}
			dbs, err := ensureAGRemoveDBs(ctx)
			if err != nil {
				return err
			}
			for _, db := range dbs {
				st, err := queryMirrorDBStatus(ctx, db)
				if err != nil {
					return err
				}
				if !st.Exists {
					ctx.Logger.Info("A-053: skip %s (not on secondary)", db)
					continue
				}
				mshLogPhase(ctx, "ag-drop-secondary-start", db)
				if err := dropMirrorSecondaryDB(ctx, ctx.CurrentStepID, db); err != nil {
					return err
				}
				mshLogPhase(ctx, "ag-drop-secondary-done", db)
			}
			return nil
		},
	}
}
