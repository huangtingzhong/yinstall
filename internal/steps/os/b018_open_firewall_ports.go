package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB018OpenFirewallPorts Open firewall ports (optional)
func StepB018OpenFirewallPorts() *runner.Step {
	return &runner.Step{
		ID:          "B-018",
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
			portsStr := ctx.GetParamString("os_firewall_ports", "")
			if portsStr == "" {
				return nil
			}

			ports := strings.Split(portsStr, ",")
			for _, port := range ports {
				port = strings.TrimSpace(port)
				if port == "" {
					continue
				}
				cmd := fmt.Sprintf("firewall-cmd --zone=public --add-port=%s/tcp --permanent", port)
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
