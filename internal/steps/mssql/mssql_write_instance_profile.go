package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepWriteInstanceProfile() *runner.Step {
	return &runner.Step{
		Name: "Write Instance Profile",
		Tags: []string{"mssql", "mssql-instance", "env"},
		Action: func(ctx *runner.StepContext) error {
			if _, err := commonmssql.DiscoverSqlcmdPath(ctx); err != nil && !ctx.DryRun && !ctx.Precheck {
				return err
			}
			return commonmssql.WriteInstanceProfileEnv(ctx)
		},
	}
}
