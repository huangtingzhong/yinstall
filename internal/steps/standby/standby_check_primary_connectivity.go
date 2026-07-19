// standby_check_primary_connectivity.go - 主库连通性检查
// 本步骤验证主库 IP 有效性和 SSH 连通性

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepCheckPrimaryConnectivity 主库连通性检查步骤
func stepCheckPrimaryConnectivity() *runner.Step {
	return &runner.Step{
		Name:        "Check Primary Connectivity",
		Description: "Verify primary database IP validity and SSH connectivity",
		Tags:        []string{"standby", "primary", "connectivity"},

		PreCheck: func(ctx *runner.StepContext) error {
			return checkPrimaryConnectivity(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Primary Connectivity")
			return checkPrimaryConnectivity(ctx)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			hostname := ctx.Results["primary_hostname"]
			if hostname == nil || hostname == "" {
				return fmt.Errorf("failed to collect primary hostname")
			}
			return nil
		},
	}
}

// checkPrimaryConnectivity 只读：SSH hostname / OS / uptime，写入 primary_hostname。
func checkPrimaryConnectivity(ctx *runner.StepContext) error {
	primaryIP := ctx.GetParamString("primary_ip", "")
	if primaryIP == "" {
		return fmt.Errorf("primary_ip parameter is required")
	}
	standbyLogPhase(ctx, "check-start", fmt.Sprintf("primary=%s", primaryIP))
	ctx.Logger.Info("Checking connectivity to primary: %s", primaryIP)

	result, err := ctx.Execute("hostname", false)
	if err != nil {
		return fmt.Errorf("failed to execute command on primary: %w", err)
	}

	hostname := strings.TrimSpace(result.GetStdout())
	ctx.Logger.Info("Primary hostname: %s", hostname)

	result, _ = ctx.Execute("cat /etc/os-release 2>/dev/null | grep -E '^(NAME|VERSION|ID)=' | head -5", false)
	if result != nil && result.GetStdout() != "" {
		ctx.Logger.Info("Primary OS info:")
		for _, line := range strings.Split(result.GetStdout(), "\n") {
			if line != "" {
				ctx.Logger.Info("  %s", line)
			}
		}
	}

	result, _ = ctx.Execute("uptime", false)
	if result != nil && result.GetStdout() != "" {
		ctx.Logger.Info("Primary uptime: %s", strings.TrimSpace(result.GetStdout()))
	}

	ctx.SetResult("primary_hostname", hostname)
	standbyLogPhase(ctx, "check-done", fmt.Sprintf("hostname=%s", hostname))
	ctx.Logger.Info("Primary connectivity check passed")
	return nil
}
