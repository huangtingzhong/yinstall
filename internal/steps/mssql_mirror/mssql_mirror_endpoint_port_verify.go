package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepEndpointPortVerify() *runner.Step {
	return &runner.Step{
		Name:        "HA Endpoint Port Verify",
		Description: "Verify HA endpoint TCP port reachable between all peers after cert/endpoint setup",
		Tags:        []string{"mssql-ha", "firewall", "mirror", "ag"},
		PreCheck: func(ctx *runner.StepContext) error {
			if len(commonmssql.HAPeerHosts(ctx)) < 2 {
				return runner.NewStepSkippedError("M-011: single host; skip endpoint port verify")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mshLogPhase(ctx, "endpoint-port-verify-start", ctx.Executor.Host())
			if err := commonmssql.VerifyHAPeerEndpointReachability(ctx, ctx.CurrentStepID); err != nil {
				return err
			}
			mshLogPhase(ctx, "endpoint-port-verify-done", ctx.Executor.Host())
			return nil
		},
	}
}
