package win_os

import (
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func stepWindowsFirewall() *runner.Step {
	return &runner.Step{
		Name:        "Windows Firewall",
		Description: "Open SQL/HA/management ports or disable firewall (lab)",
		Tags:        []string{"win-os", "win-os-both", "firewall"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonwin.ShouldConfigureFirewall(ctx) {
				return runner.NewStepSkippedError("firewall mode is keep")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-004 firewall")
			return commonwin.OpenFirewallPorts(ctx)
		},
	}
}
