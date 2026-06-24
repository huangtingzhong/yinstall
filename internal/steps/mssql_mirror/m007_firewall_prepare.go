package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepM007HAFirewallPrepare() *runner.Step {
	return &runner.Step{
		ID:          "M-007",
		Name:        "HA Firewall Prepare",
		Description: "Open inbound firewall for SQL/HA/SMB ports before mirror or Always On endpoint creation",
		Tags:        []string{"mssql-ha", "firewall", "mirror", "ag"},
		PreCheck: func(ctx *runner.StepContext) error {
			if len(commonmssql.HAPeerHosts(ctx)) < 2 {
				return runner.NewStepSkippedError("M-007: single host; skip HA firewall prepare")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mshLogPhase(ctx, "plan", "M-007 HA firewall prepare")
			ports := commonmssql.HAFirewallTCPPorts(ctx)
			ctx.Logger.Info("M-007: ensuring inbound firewall for TCP ports %v on %s", ports, ctx.Executor.Host())
			if err := commonmssql.EnsureHAFirewallInbound(ctx); err != nil {
				return err
			}
			mshLogPhase(ctx, "firewall-done", ctx.Executor.Host())
			return nil
		},
		PostCheck: func(ctx *runner.StepContext) error {
			return commonmssql.VerifyHAPreEndpointConnectivity(ctx, "M-007")
		},
	}
}
