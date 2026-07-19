package mssql_mirror

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepRemovePartner() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Remove Partner",
		Description: "ALTER DATABASE SET PARTNER OFF on primary",
		Tags:        []string{"mssql-ha", "mirror", "remove"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("M-052 runs on primary only")
			}
			dbs, err := ensureMirrorRemoveDBs(ctx)
			if err != nil {
				return err
			}
			pending := 0
			for _, db := range dbs {
				if mirrorDBHasPartner(ctx, db) {
					pending++
				}
			}
			if pending == 0 {
				return runner.NewStepSkippedError("M-052: no mirroring partner on target databases")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			dbs, err := ensureMirrorRemoveDBs(ctx)
			if err != nil {
				return err
			}
			for _, db := range dbs {
				if !mirrorDBHasPartner(ctx, db) {
					ctx.Logger.Info("M-052: skip %s (no mirroring partner)", db)
					continue
				}
				mshLogPhase(ctx, "mirror-remove-start", db)
				sql := commonmssql.RemoveMirrorPartnerSQL(db)
				if err := commonmssql.RunSqlcmdQueries(ctx, "M-052 remove partner "+db, []string{sql}); err != nil {
					return err
				}
				out, err := commonmssql.QuerySqlcmdScalar(ctx, "M-052 verify removed "+db, commonmssql.MirrorHasPartnerScalarSQL(db))
				if err != nil {
					return err
				}
				if commonmssql.ParseSqlcmdBoolScalar(out) {
					return fmt.Errorf("M-052: database %s still has mirroring partner after SET PARTNER OFF", db)
				}
				mshLogPhase(ctx, "mirror-remove-done", db)
			}
			return nil
		},
	}
}
