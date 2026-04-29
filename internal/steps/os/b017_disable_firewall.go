package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB017DisableFirewall 关闭或调整防火墙（可选/危险）
func StepB017DisableFirewall() *runner.Step {
	return &runner.Step{
		ID:          "B-017",
		Name:        "Disable Firewall",
		Description: "Stop and disable firewalld service",
		Tags:        []string{"os", "firewall"},
		Optional:    true,
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			mode := ctx.GetParamString("os_firewall_mode", "keep")
			if mode != "disable" {
				return fmt.Errorf("firewall mode is not disable")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Execute("systemctl stop firewalld 2>/dev/null", true)
			ctx.Execute("systemctl disable firewalld 2>/dev/null", true)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("systemctl is-active firewalld 2>/dev/null || echo inactive", false)
			if strings.TrimSpace(result.GetStdout()) == "active" {
				return fmt.Errorf("firewalld is still active")
			}
			return nil
		},
	}
}
