package win_os

import (
	"fmt"
	"strings"

	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func StepW012OSBaselineVerify() *runner.Step {
	return &runner.Step{
		ID:          "W-012",
		Name:        "OS Baseline Verify",
		Description: "Summarize OS baseline and verify topology extras",
		Tags:        []string{"win-os", "win-os-both", "verify"},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-012 verify")
			var lines []string
			if v, ok := ctx.Results["fqdn"].(string); ok && v != "" {
				lines = append(lines, "FQDN: "+v)
			}
			if v, ok := ctx.Results["domain_joined"].(bool); ok {
				lines = append(lines, fmt.Sprintf("Domain joined: %v", v))
			}
			if v, ok := ctx.Results["os_power_plan_guid"].(string); ok && v != "" {
				lines = append(lines, "Power plan: "+v)
			}
			if v, ok := ctx.Results["os_spn_ok"].(bool); ok {
				lines = append(lines, fmt.Sprintf("SPN OK: %v", v))
			}
			fw := commonwin.ListFirewallRulesSummary(ctx)
			if fw != "" {
				lines = append(lines, "Firewall rules: "+strings.ReplaceAll(fw, "\n", ", "))
			}
			ctx.Logger.Info("OS baseline summary:\n  %s", strings.Join(lines, "\n  "))
			if fn := ctx.GetParamString("win_os_verify_extra", ""); fn != "" {
				_ = fn
			}
			if extra, ok := ctx.Params["win_os_verify_extra_fn"]; ok {
				if f, ok := extra.(func(*runner.StepContext) error); ok && f != nil {
					if err := f(ctx); err != nil {
						return err
					}
				}
			}
			ctx.SetResult("win_os_baseline_ok", true)
			return nil
		},
	}
}
