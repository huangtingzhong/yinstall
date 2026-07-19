package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepWriteSqlToolsEnv() *runner.Step {
	return &runner.Step{
		Name: "Write SQL Tools Env",
		Tags: []string{"mssql", "mssql-instance"},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				commonmssql.DiscoverSqlcmdPath(ctx)
			}
			return commonmssql.WriteSQLToolsEnv(ctx)
		},
	}
}
