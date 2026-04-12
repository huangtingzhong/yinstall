// e001_check_primary_connectivity.go - 主库连通性检查
// 本步骤验证主库 IP 有效性和 SSH 连通性

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepE001CheckPrimaryConnectivity 主库连通性检查步骤
func StepE001CheckPrimaryConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "E-001",
		Name:        "Check Primary Connectivity",
		Description: "Verify primary database IP validity and SSH connectivity",
		Tags:        []string{"standby", "primary", "connectivity"},

		PreCheck: func(ctx *runner.StepContext) error {
			primaryIP := ctx.GetParamString("primary_ip", "")
			if primaryIP == "" {
				return fmt.Errorf("primary_ip parameter is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			primaryIP := ctx.GetParamString("primary_ip", "")

			ctx.Logger.Info("Checking connectivity to primary: %s", primaryIP)

			// Test basic connectivity
			result, err := ctx.Execute("hostname", false)
			if err != nil {
				return fmt.Errorf("failed to execute command on primary: %w", err)
			}

			hostname := strings.TrimSpace(result.GetStdout())
			ctx.Logger.Info("Primary hostname: %s", hostname)

			// Collect OS info
			result, _ = ctx.Execute("cat /etc/os-release 2>/dev/null | grep -E '^(NAME|VERSION|ID)=' | head -5", false)
			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Primary OS info:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			// Check uptime
			result, _ = ctx.Execute("uptime", false)
			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Primary uptime: %s", strings.TrimSpace(result.GetStdout()))
			}

			ctx.SetResult("primary_hostname", hostname)
			ctx.Logger.Info("Primary connectivity check passed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// Verify hostname was collected
			hostname := ctx.Results["primary_hostname"]
			if hostname == nil || hostname == "" {
				return fmt.Errorf("failed to collect primary hostname")
			}
			return nil
		},
	}
}
