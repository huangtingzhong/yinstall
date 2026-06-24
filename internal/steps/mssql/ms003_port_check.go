package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS003PortCheck() *runner.Step {
	return &runner.Step{
		ID:   "MS-003",
		Name: "Port Check",
		Tags: []string{"mssql", "mssql-instance", "port"},
		PreCheck: func(ctx *runner.StepContext) error {
			port := commonmssql.ResolvedListenPort(ctx)
			haPort := ctx.GetParamInt("mssql_ha_endpoint_port", 5022)
			for _, p := range []int{port, haPort} {
				cmd := fmt.Sprintf(`powershell -NoProfile -Command "(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Measure-Object).Count"`, p)
				res, _ := ctx.Execute(cmd, false)
				if res != nil && strings.TrimSpace(res.GetStdout()) != "0" && strings.TrimSpace(res.GetStdout()) != "" {
					return fmt.Errorf("port %d already in use", p)
				}
			}
			return nil
		},
	}
}
