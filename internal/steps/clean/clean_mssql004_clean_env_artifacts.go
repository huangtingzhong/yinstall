package clean

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepCleanMssql004CleanEnvArtifacts() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MSSQL-004",
		Name:        "Clean MSSQL Env Artifacts",
		Description: "Remove yinstall env files and machine YINSTALL_* variables",
		Tags:        []string{"clean", "mssql"},
		Dangerous:   true,
		Action: func(ctx *runner.StepContext) error {
			layout := mssqlCleanLayout(ctx)
			return commonmssql.CleanOperatorArtifacts(ctx, layout.AdminBase, layout.Port)
		},
	}
}
