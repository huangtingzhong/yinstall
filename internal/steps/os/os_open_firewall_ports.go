package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepOpenFirewallPorts 放行指定防火墙端口（可选）
func stepOpenFirewallPorts() *runner.Step {
	return &runner.Step{
		Name:        "Open Firewall Ports",
		Description: "Open specified ports in firewall",
		Tags:        []string{"os", "firewall"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			mode := ctx.GetParamString("os_firewall_mode", "keep")
			if mode != "open-ports" {
				return fmt.Errorf("firewall mode is not open-ports")
			}
			result, _ := ctx.Execute("systemctl is-active firewalld 2>/dev/null", false)
			if strings.TrimSpace(result.GetStdout()) != "active" {
				return fmt.Errorf("firewalld is not active")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-018: Open Firewall Ports")
			portsStr := ctx.GetParamString("os_firewall_ports", "")
			if portsStr == "" {
				return nil
			}

			ports := strings.Split(portsStr, ",")
			var want []string
			for _, port := range ports {
				port = strings.TrimSpace(port)
				if port == "" {
					continue
				}
				want = append(want, port+"/tcp")
			}
			if len(want) == 0 {
				return nil
			}

			if !ctx.IsForceStep() && firewallPortsAlreadyOpen(ctx, want) {
				ctx.Logger.Info("Firewall ports already open, skipping add/reload (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=firewall_ports")
				return nil
			}

			for _, p := range want {
				cmd := fmt.Sprintf("firewall-cmd --zone=public --add-port=%s --permanent", p)
				ctx.Execute(cmd, true)
			}

			ctx.Execute("firewall-cmd --reload", true)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("firewall-cmd --zone=public --list-ports 2>/dev/null", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("failed to list firewall ports")
			}
			return nil
		},
	}
}

func firewallPortsAlreadyOpen(ctx *runner.StepContext, want []string) bool {
	result, _ := ctx.Execute("firewall-cmd --zone=public --list-ports 2>/dev/null", false)
	if result == nil || result.GetExitCode() != 0 {
		return false
	}
	listed := strings.Fields(strings.TrimSpace(result.GetStdout()))
	have := make(map[string]bool, len(listed))
	for _, p := range listed {
		have[p] = true
	}
	for _, p := range want {
		if !have[p] {
			return false
		}
	}
	return true
}
