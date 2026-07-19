package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepPostVerify() *runner.Step {
	return &runner.Step{
		Name:        "Post Mirror Verify",
		Description: "Verify mirroring and print database status summary",
		Tags:        []string{"mssql-ha", "mirror", "verify"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("M-014 runs on primary only")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			return printMirrorStatusSummary(ctx)
		},
	}
}
