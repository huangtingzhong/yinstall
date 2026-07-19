package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepResolveInstance() *runner.Step {
	return &runner.Step{
		Name:        "Resolve Instance Target",
		Description: "Resolve SQL instance and TCP port from registry (install requires explicit instance)",
		Tags:        []string{"mssql", "mssql-both", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			if _, err := commonmssql.ResolveInstanceTarget(ctx, commonmssql.ResolveModeInstallNew); err != nil {
				return err
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			_, err := commonmssql.ResolveInstanceTarget(ctx, commonmssql.ResolveModeInstallNew)
			return err
		},
	}
}
