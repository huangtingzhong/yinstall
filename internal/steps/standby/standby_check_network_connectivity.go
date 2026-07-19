// standby_check_network_connectivity.go - 主备网络互通检查
// 本步骤验证主库和备库之间的网络连通性

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepCheckNetworkConnectivity 主备网络互通检查步骤
// 注意：不再标 Optional。Optional+PreCheck 失败会被框架当成 skip，导致 --precheck 假绿；
// ping 失败行为与原 Action 一致（阻塞后续）。
func stepCheckNetworkConnectivity() *runner.Step {
	return &runner.Step{
		Name:        "Check Network Connectivity",
		Description: "Verify network connectivity between primary and standby nodes",
		Tags:        []string{"standby", "network"},

		PreCheck: func(ctx *runner.StepContext) error {
			return checkNetworkConnectivity(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Network Connectivity")
			return checkNetworkConnectivity(ctx)
		},
	}
}

// checkNetworkConnectivity 只读：从主库 ping / 探测备库 SSH 端口。
func checkNetworkConnectivity(ctx *runner.StepContext) error {
	targets := ctx.GetParamStringSlice("standby_targets")
	if len(targets) == 0 {
		return fmt.Errorf("no standby targets specified")
	}
	standbyLogPhase(ctx, "check-start", fmt.Sprintf("targets=%d", len(targets)))
	sshPort := ctx.GetParamInt("ssh_port", 22)

	ctx.Logger.Info("Checking network connectivity from primary to standby nodes")

	for _, target := range targets {
		ctx.Logger.Info("Pinging standby node: %s", target)

		cmd := fmt.Sprintf("ping -c 3 -W 5 %s", target)
		result, _ := ctx.Execute(cmd, false)
		if result == nil || result.GetExitCode() != 0 {
			return fmt.Errorf("cannot ping standby node %s from primary", target)
		}
		ctx.Logger.Info("  Ping successful to %s", target)

		cmd = fmt.Sprintf("timeout 5 bash -c '</dev/tcp/%s/%d' 2>/dev/null && echo 'SSH_OK' || echo 'SSH_FAIL'", target, sshPort)
		result, _ = ctx.Execute(cmd, false)
		if result == nil || !strings.Contains(result.GetStdout(), "SSH_OK") {
			ctx.Logger.Warn("  SSH port %d may not be accessible on %s", sshPort, target)
		} else {
			ctx.Logger.Info("  SSH port %d accessible on %s", sshPort, target)
		}
	}

	standbyLogPhase(ctx, "check-done", fmt.Sprintf("targets=%d", len(targets)))
	ctx.Logger.Info("Network connectivity check passed")
	return nil
}
