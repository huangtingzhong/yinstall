package mssql_ag

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepPostVerify() *runner.Step {
	return &runner.Step{
		Name: "Post HA Verify",
		Tags: []string{"mssql-ha", "verify"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("A-015 runs on primary only")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			ag := commonmssql.AGName(ctx)
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-015 verify AG", []string{commonmssql.VerifyAvailabilityGroupSQL(ag)}); err != nil {
				return err
			}
			mshLogPhase(ctx, "verify-done", ag)
			return printAGStatusSummary(ctx)
		},
	}
}
