package mssql

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS016VerifyInstallation() *runner.Step {
	return &runner.Step{
		ID:   "MS-016",
		Name: "Verify Installation",
		Tags: []string{"mssql", "mssql-instance"},
		Action: func(ctx *runner.StepContext) error {
			if _, err := commonmssql.EnsureInstanceResolved(ctx); err != nil {
				return err
			}
			if err := commonmssql.PrepareSqlcmdSession(ctx); err != nil {
				return err
			}
			cmd := commonmssql.SqlcmdQueryCommand(ctx, "SELECT 1")
			ctx.LogScriptPreview("sqlcmd", "MS-016 verify", cmd)
			if ctx.DryRun || ctx.Precheck {
				ctx.Logger.Info("MS-016 would run: %s", cmd)
				return nil
			}
			_, err := ctx.ExecuteWithCheck(cmd, false)
			if err != nil {
				return fmt.Errorf("sqlcmd verify: %w", err)
			}
			ctx.SetResult("mssql_version_running", "verified")
			if !hasCustomSQLScript(ctx) {
				return printMssqlInstallSummary(ctx, "MS-016")
			}
			return nil
		},
	}
}
