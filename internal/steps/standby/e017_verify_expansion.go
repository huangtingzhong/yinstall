// e017_verify_expansion.go - 扩容完成验证
// 本步骤验证备库扩容是否成功，检查集群状态和备库连接

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepE017VerifyExpansion 扩容完成验证步骤
func StepE017VerifyExpansion() *runner.Step {
	return &runner.Step{
		ID:          "E-017",
		Name:        "Verify Expansion",
		Description: "Verify standby expansion completed successfully",
		Tags:        []string{"standby", "verify"},

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			ctx.Logger.Info("Verifying standby expansion")

			// Test yasql connectivity
			ctx.Logger.Info("Testing database connectivity...")
			cmd := fmt.Sprintf("su - %s -c 'echo \"SELECT 1 FROM DUAL;\" | yasql / as sysdba'", user)
			result, err := ctx.Execute(cmd, true)
			if err != nil {
				ctx.Logger.Warn("Database connectivity test failed: %v", err)
			} else if result.GetExitCode() == 0 {
				ctx.Logger.Info("Database connectivity: OK")
			} else {
				ctx.Logger.Warn("Database connectivity test returned non-zero exit code")
			}

			// Check yasboot availability
			cmd = fmt.Sprintf("su - %s -c 'which yasboot'", user)
			result, _ = ctx.Execute(cmd, true)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("yasboot found: %s", strings.TrimSpace(result.GetStdout()))
			} else {
				ctx.Logger.Warn("yasboot not found in PATH")
			}

			// Check key processes
			ctx.Logger.Info("Checking key processes...")

			// Check yasdb
			result, _ = ctx.Execute("pgrep -x yasdb", false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("  yasdb process: running")
			} else {
				ctx.Logger.Warn("  yasdb process: not found")
			}

			// Check yasom
			result, _ = ctx.Execute("pgrep -x yasom", false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("  yasom process: running")
			} else {
				ctx.Logger.Warn("  yasom process: not found")
			}

			// Check yasagent
			result, _ = ctx.Execute("pgrep -x yasagent", false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("  yasagent process: running")
			} else {
				ctx.Logger.Warn("  yasagent process: not found")
			}

			// Get cluster status
			ctx.Logger.Info("Final cluster status:")
			cmd = fmt.Sprintf("su - %s -c 'yasboot cluster status -c %s -d' 2>/dev/null || echo 'status check failed'", user, clusterName)
			result, _ = ctx.Execute(cmd, true)
			if result != nil && result.GetStdout() != "" {
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			ctx.Logger.Info("Standby expansion verification completed")
			return nil
		},
	}
}
