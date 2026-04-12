// e009_check_network_connectivity.go - 主备网络互通检查
// 本步骤验证主库和备库之间的网络连通性

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepE009CheckNetworkConnectivity 主备网络互通检查步骤
func StepE009CheckNetworkConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "E-009",
		Name:        "Check Network Connectivity",
		Description: "Verify network connectivity between primary and standby nodes",
		Tags:        []string{"standby", "network"},
		Optional:    true, // 网络检查为可选步骤，失败不阻止后续流程

		PreCheck: func(ctx *runner.StepContext) error {
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("no standby targets specified")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			targets := ctx.GetParamStringSlice("standby_targets")
			sshPort := ctx.GetParamInt("ssh_port", 22)

			ctx.Logger.Info("Checking network connectivity from primary to standby nodes")

			for _, target := range targets {
				ctx.Logger.Info("Pinging standby node: %s", target)

				// Ping test
				cmd := fmt.Sprintf("ping -c 3 -W 5 %s", target)
				result, _ := ctx.Execute(cmd, false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("cannot ping standby node %s from primary", target)
				}
				ctx.Logger.Info("  Ping successful to %s", target)

				// SSH port check（与 yinstall 连接备库所用端口一致）
				cmd = fmt.Sprintf("timeout 5 bash -c '</dev/tcp/%s/%d' 2>/dev/null && echo 'SSH_OK' || echo 'SSH_FAIL'", target, sshPort)
				result, _ = ctx.Execute(cmd, false)
				if result == nil || !strings.Contains(result.GetStdout(), "SSH_OK") {
					ctx.Logger.Warn("  SSH port %d may not be accessible on %s", sshPort, target)
				} else {
					ctx.Logger.Info("  SSH port %d accessible on %s", sshPort, target)
				}
			}

			ctx.Logger.Info("Network connectivity check passed")
			return nil
		},
	}
}
