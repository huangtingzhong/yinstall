// os_disable_firewall.go - 按 --os-firewall-mode=disable 停用 firewalld
// mode 非 disable 时 Optional PreCheck skip

package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepDisableFirewall 关闭防火墙（--os-firewall-mode=disable）
func stepDisableFirewall() *runner.Step {
	return &runner.Step{
		Name:        "Disable Firewall",
		Description: "Stop and disable firewalld service",
		Tags:        []string{"os", "firewall"},
		Optional:    true,
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			mode, err := commonos.NormalizeFirewallMode(ctx.GetParamString("os_firewall_mode", commonos.FirewallModeEnable))
			if err != nil {
				return err
			}
			if mode != commonos.FirewallModeDisable {
				return fmt.Errorf("firewall mode is not disable")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-017: Disable Firewall")
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
