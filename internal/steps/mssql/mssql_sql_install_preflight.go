package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepSqlInstallPreflight() *runner.Step {
	return &runner.Step{
		Name: "SQL Install Preflight",
		Tags: []string{"mssql", "mssql-instance", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			if err := commonmssql.PhaseB1Complete(ctx); err != nil {
				return err
			}
			inst := commonmssql.LayoutInstanceName(ctx)
			if err := commonmssql.InstanceConflict(ctx, inst); err != nil {
				return err
			}
			layout := commonmssql.ResolveLayoutFromContext(ctx)
			if err := commonmssql.ValidateProgramDataPaths(layout); err != nil {
				return err
			}
			if layout.UseProgramCustom && layout.SetupProductMajor > 0 {
				if omit, err := commonmssql.ShouldOmitInstallSharedDir(ctx, layout.SetupProductMajor, inst); err != nil {
					return err
				} else if omit {
					ctx.Logger.Info("MS-002: same product major already installed; INSTALLSHAREDDIR will be omitted for %s", inst)
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mssqlLogPhase(ctx, "preflight-sql-start", "")
			mssqlLogPhase(ctx, "preflight-sql-done", "")
			return nil
		},
	}
}
