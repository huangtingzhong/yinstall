package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS015RemoteSAAccess() *runner.Step {
	return &runner.Step{
		ID:       "MS-015",
		Name:     "Remote SA Access",
		Tags:     []string{"mssql", "mssql-instance"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("mssql_remote_sa", false) {
				return runner.NewStepSkippedError("mssql_remote_sa=false")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				cmd := commonmssql.SqlcmdQueryCommand(ctx, "ALTER LOGIN sa ENABLE;")
				ctx.LogScriptPreview("sqlcmd", "MS-015 enable sa", cmd)
				return nil
			}
			if err := commonmssql.PrepareSqlcmdSession(ctx); err != nil {
				return err
			}
			cmd := commonmssql.SqlcmdQueryCommand(ctx, "ALTER LOGIN sa ENABLE;")
			_, err := ctx.ExecuteWithCheck(cmd, false)
			return err
		},
	}
}
