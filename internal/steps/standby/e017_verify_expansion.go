// e017_verify_expansion.go - 扩容完成验证
// 本步骤验证备库扩容是否成功，检查集群状态和备库连接

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
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
			// Read-only capability checks: commands used in Action should exist.
			r, _ := ctx.Execute("command -v ps >/dev/null 2>&1", false)
			if r == nil || r.GetExitCode() != 0 {
				return fmt.Errorf("ps command not found")
			}
			// pgrep is used
			r, _ = ctx.Execute("command -v pgrep >/dev/null 2>&1", false)
			if r == nil || r.GetExitCode() != 0 {
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepID:      "E-017",
					StepName:    "Verify Expansion",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityWarn,
					Code:        "PC.STANDBY.PGREP.MISSING",
					Message:     "pgrep is not available; the apply-time verification step cannot perform full process checks.",
					Remediation: "Install the procps package (or provide an equivalent tool).",
				})
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepID:      "E-017",
				StepName:    "Verify Expansion",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.STANDBY.VERIFY.APPLY_ONLY",
				Message:     "This step is for post-apply verification (connectivity/process/cluster status). In --precheck, it only checks command availability.",
				Remediation: "Run it after expansion is complete (or run without --precheck) to perform real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			homeDir, err := commonos.GetUserHomeDir(ctx, user)
			if err != nil {
				homeDir = fmt.Sprintf("/home/%s", user)
			}
			envFile := commonos.DetermineEnvFile(homeDir, beginPort)

			ctx.Logger.Info("Verifying standby expansion")

			// Test yasql connectivity（与 commonsql.ExecuteSQLAsSysdbaCtx 一致）
			ctx.Logger.Info("Testing database connectivity...")
			if _, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, user, envFile, clusterName, "SELECT 1 FROM DUAL", true); err != nil {
				return fmt.Errorf("database connectivity test failed: %w", err)
			}
			ctx.Logger.Info("Database connectivity: OK")

			// Check yasboot availability
			result, _ := commonos.ExecuteAsUserWithEnv(ctx, user, envFile, "command -v yasboot", true)
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
			result, _ = commonos.ExecuteAsUserWithEnv(ctx, user, envFile, fmt.Sprintf("yasboot cluster status -c %s -d 2>/dev/null || echo 'status check failed'", clusterName), true)
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
