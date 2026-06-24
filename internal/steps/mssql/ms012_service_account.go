package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS012ServiceAccount() *runner.Step {
	return &runner.Step{
		ID:       "MS-012",
		Name:     "Service Account",
		Tags:     []string{"mssql", "mssql-instance"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamString("mssql_sqlsvc_account", "") == "" {
				return runner.NewStepSkippedError("mssql_sqlsvc_account unset")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			acct := ctx.GetParamString("mssql_sqlsvc_account", "")
			pwd := ctx.GetParamString("mssql_sqlsvc_password", "")
			inst := commonmssql.LayoutInstanceName(ctx)
			svc := "MSSQLSERVER"
			if !strings.EqualFold(inst, commonmssql.DefaultInstance) {
				svc = "MSSQL$" + inst
			}
			script := fmt.Sprintf(`sc.exe config "%s" obj= "%s" password= "%s"`, svc, acct, pwd)
			ctx.LogScriptPreview("powershell", "MS-012 service account", "sc.exe config ... obj= (password redacted)")
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			_, err := ctx.ExecuteWithCheck(script, false)
			return err
		},
	}
}
