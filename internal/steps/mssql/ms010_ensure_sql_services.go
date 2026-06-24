package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS010EnsureSQLServices() *runner.Step {
	return &runner.Step{
		ID:   "MS-010",
		Name: "Ensure SQL Services",
		Tags: []string{"mssql", "mssql-instance"},
		Action: func(ctx *runner.StepContext) error {
			inst := commonmssql.LayoutInstanceName(ctx)
			svc := "MSSQLSERVER"
			if !strings.EqualFold(inst, commonmssql.DefaultInstance) {
				svc = "MSSQL$" + inst
			}
			script := fmt.Sprintf(`Start-Service -Name '%s' -ErrorAction Stop; Set-Service -Name '%s' -StartupType Automatic`, svc, svc)
			ctx.LogScriptPreview("powershell", "MS-010 services", script)
			_, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
			return err
		},
	}
}
