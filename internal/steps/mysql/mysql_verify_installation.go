package mysql

import (
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepVerifyInstallation runs basic verification queries.
func stepVerifyInstallation() *runner.Step {
	return &runner.Step{
		Name:        "Verify Installation",
		Description: "SELECT VERSION() and summarize",
		Tags:        []string{"mysql", "verify", "mysql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout, _ := layoutFromCtx(ctx)
			password := ctx.GetParamString("mysql_root_password", "")
			out, err := commonsql.QueryMysqlSQL(ctx, layout, password, "SELECT VERSION();")
			if err != nil {
				return err
			}
			ctx.Logger.Info("MySQL verify: %s", out)
			ctx.SetResult("mysql_version_running", out)
			if !hasCustomSQLScript(ctx) {
				return printMysqlInstallSummary(ctx, ctx.CurrentStepID)
			}
			return nil
		},
	}
}
