package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS014SetSAPassword() *runner.Step {
	return &runner.Step{
		ID:       "MS-014",
		Name:     "Set SA Password",
		Tags:     []string{"mssql", "mssql-instance"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamString("mssql_sa_password", "") != "" {
				return runner.NewStepSkippedError("SAPWD in Configuration.ini")
			}
			if ini, ok := ctx.Results["mssql_configuration_ini"].(string); ok && strings.Contains(ini, "SAPWD=") {
				return runner.NewStepSkippedError("SAPWD in Configuration.ini")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			pwd := ctx.GetParamString("mssql_sa_password", "")
			if pwd == "" {
				return fmt.Errorf("mssql_sa_password required for MS-014")
			}
			if err := commonmssql.PrepareSqlcmdSession(ctx); err != nil {
				return err
			}
			q := strings.ReplaceAll(pwd, "'", "''")
			cmd := commonmssql.SqlcmdQueryCommand(ctx, fmt.Sprintf("ALTER LOGIN sa WITH PASSWORD=N'%s';", q))
			ctx.LogScriptPreview("sqlcmd", "MS-014 set sa password", cmd)
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			_, err := ctx.ExecuteWithCheck(cmd, false)
			return err
		},
	}
}
