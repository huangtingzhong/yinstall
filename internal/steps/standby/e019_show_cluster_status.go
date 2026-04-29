// e019_show_cluster_status.go - 显示集群状态
// 本步骤在主库上执行 yasboot cluster status 命令，显示完整的集群状态信息
// 参考 C-028 步骤的实现

package standby

import (
	"fmt"
	"os"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE019ShowClusterStatus 显示集群状态步骤
func StepE019ShowClusterStatus() *runner.Step {
	return &runner.Step{
		ID:          "E-019",
		Name:        "Show Cluster Status",
		Description: "Display cluster status information on primary database",
		Tags:        []string{"standby", "status", "display"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// Read-only: ensure envFile & yasboot status command can be executed (do not print full status here).
			primaryUser := GetPrimaryOSUser(ctx)
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			_, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
			if err != nil {
				return fmt.Errorf("yasboot cluster status check failed (precheck): %w", err)
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepID:      "E-019",
				StepName:    "Show Cluster Status",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.STANDBY.DISPLAY.APPLY_ONLY",
				Message:     "This step displays cluster status during apply; in --precheck it only validates envFile/yasboot availability.",
				Remediation: "Run during apply or after installation (or run without --precheck) to view real status output.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)

			ctx.Logger.Info("Displaying cluster status on primary")
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ctx.Logger.Info("  Cluster: %s", clusterName)

			// Execute yasboot cluster status command
			cmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, cmd, true)

			if err != nil {
				errMsg := "Failed to get cluster status"
				if result != nil {
					if result.GetStderr() != "" {
						errMsg = result.GetStderr()
					} else if result.GetStdout() != "" {
						errMsg = result.GetStdout()
					}
				}
				ctx.Logger.Warn("Failed to get cluster status: %s", errMsg)
				return fmt.Errorf("failed to get cluster status: %s", errMsg)
			}

			if result != nil && result.GetExitCode() == 0 {
				output := result.GetStdout()
				if output != "" {
					// 输出到日志
					ctx.Logger.Info("========== Cluster Status ==========")
					for _, line := range strings.Split(output, "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							ctx.Logger.Info("%s", line)
						}
					}
					ctx.Logger.Info("=====================================")

					// 同时输出到终端标准输出
					fmt.Fprintf(os.Stdout, "\n========== Cluster Status ==========\n")
					for _, line := range strings.Split(output, "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							fmt.Fprintf(os.Stdout, "%s\n", line)
						}
					}
					fmt.Fprintf(os.Stdout, "=====================================\n\n")
				} else {
					ctx.Logger.Warn("Cluster status command returned empty output")
				}
			} else {
				errMsg := "Failed to get cluster status"
				if result != nil {
					if result.GetStderr() != "" {
						errMsg = result.GetStderr()
					} else if result.GetStdout() != "" {
						errMsg = result.GetStdout()
					}
				}
				ctx.Logger.Warn("Failed to get cluster status: %s", errMsg)
				return fmt.Errorf("failed to get cluster status: %s", errMsg)
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
