package mssql_ag

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepListener() *runner.Step {
	return &runner.Step{
		Name:        "AG Listener",
		Description: "Create AG listener on primary",
		Tags:        []string{"mssql-ha", "listener"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("A-013 runs on primary only")
			}
			if !commonmssql.AGListenerEnabled(ctx) {
				return runner.NewStepSkippedError("A-013: --mssql-ag-listener-ip not set")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			ag := commonmssql.AGName(ctx)
			listener := commonmssql.AGListenerName(ctx)
			port := ctx.GetParamInt("mssql_ag_listener_port", commonmssql.ResolvedListenPort(ctx))
			ip, err := commonmssql.ResolveListenerIP(ctx)
			if err != nil {
				return err
			}
			sql := commonmssql.CreateListenerSQL(ag, listener, ip, port)
			return commonmssql.RunSqlcmdQueries(ctx, "A-013 listener", []string{sql})
		},
	}
}
