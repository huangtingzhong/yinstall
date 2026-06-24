package mssql_ag

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepA009HAEndpointPortVerify() *runner.Step {
	return &runner.Step{
		ID:          "A-009",
		Name:        "HA Endpoint Port Verify",
		Description: "Verify HA endpoint TCP port reachable between all peers after cert/endpoint setup",
		Tags:        []string{"mssql-ha", "firewall", "mirror", "ag"},
		PreCheck: func(ctx *runner.StepContext) error {
			if len(commonmssql.HAPeerHosts(ctx)) < 2 {
				return runner.NewStepSkippedError("A-009: single host; skip endpoint port verify")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mshLogPhase(ctx, "endpoint-port-verify-start", ctx.Executor.Host())
			if err := commonmssql.VerifyHAPeerEndpointReachability(ctx, "A-009"); err != nil {
				return err
			}
			mshLogPhase(ctx, "endpoint-port-verify-done", ctx.Executor.Host())
			return nil
		},
	}
}
