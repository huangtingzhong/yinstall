package mssql_ag

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// StepA002CheckPrimary validates primary SQL connectivity and records instance version.
func StepA002CheckPrimary() *runner.Step {
	return &runner.Step{
		ID:          "A-002",
		Name:        "Check Primary SQL",
		Description: "Verify primary SQL Server is reachable and collect version for replica install planning",
		Tags:        []string{"mssql-ha", "primary", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("A-002 runs on primary only")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			mshLogPhase(ctx, "plan", "A-002 check primary SQL")
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-002 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return fmt.Errorf("primary SQL not reachable: %w", err)
			}
			host := ctx.Executor.Host()
			stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-002 instance version", commonmssql.MirrorInstanceInfoSQL())
			if err != nil {
				return fmt.Errorf("primary instance version: %w", err)
			}
			info, err := commonmssql.ParseMirrorInstanceInfo(host, stdout)
			if err != nil {
				return err
			}
			commonmssql.StorePrimaryInstanceInfo(ctx.Results, info)
			ctx.SetResult("primary_mssql_version", info.ProductVersion)
			ctx.Logger.Info("Primary SQL: host=%s ProductVersion=%s Edition=%s major=%s",
				host, info.ProductVersion, info.Edition, info.ProductMajorVersion)
			return nil
		},
	}
}
