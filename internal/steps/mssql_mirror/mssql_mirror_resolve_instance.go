package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepResolveInstance() *runner.Step {
	return &runner.Step{
		Name:        "Resolve Instance Target",
		Description: "Resolve SQL instance/port from registry, verify service running, probe sqlcmd auth",
		Tags:        []string{"mssql-ha", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			if _, err := commonmssql.EnsureHAInstanceTarget(ctx); err != nil {
				return err
			}
			if commonmssql.ReplicaSQLPendingInstall(ctx.Results) {
				return nil
			}
			_, err := commonmssql.EnsureSqlcmdAuth(ctx)
			return err
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun {
				return nil
			}
			if ctx.Precheck {
				return nil
			}
			if _, err := commonmssql.EnsureHAInstanceTarget(ctx); err != nil {
				return err
			}
			if commonmssql.ReplicaSQLPendingInstall(ctx.Results) {
				return nil
			}
			_, err := commonmssql.EnsureSqlcmdAuth(ctx)
			return err
		},
	}
}
