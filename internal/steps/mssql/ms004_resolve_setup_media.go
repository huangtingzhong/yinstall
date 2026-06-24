package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS004ResolveSetupMedia() *runner.Step {
	resolve := func(ctx *runner.StepContext) error {
		return commonmssql.ResolveAndStoreSetupMedia(ctx)
	}
	return &runner.Step{
		ID:       "MS-004",
		Name:     "Resolve Setup Media",
		Tags:     []string{"mssql", "mssql-both", "mssql-software", "mssql-instance"},
		PreCheck: resolve,
		Action: func(ctx *runner.StepContext) error {
			mssqlLogPhase(ctx, "plan", "MS-004 resolve media")
			return resolve(ctx)
		},
	}
}
