// e006_add_standby_instance.go - 添加备库实例
// 本步骤在主库执行 yasboot node add 创建备库实例
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE006AddStandbyInstance 添加备库实例步骤
func StepE010AddStandbyInstance() *runner.Step {
	return &runner.Step{
		ID:          "E-010",
		Name:        "Add Standby Instance",
		Description: "Create standby database instances using yasboot node add",
		Tags:        []string{"standby", "instance"},

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// Check cluster_add.toml exists
			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", clusterAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("%s_add.toml not found, run E-004 first", clusterName)
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)

			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)

			ctx.Logger.Info("Adding standby database instances")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Config file: %s", clusterAddFile)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)

			// Build yasboot node add command
			nodeAddCmd := fmt.Sprintf("cd %s && yasboot node add -c %s -t %s",
				stageDir, clusterName, clusterAddFile)

			// Execute as primary user with environment sourced
			ctx.Logger.Info("Running: yasboot node add ...")
			ctx.Logger.Info("NOTE: This command triggers background data synchronization")
			ctx.Logger.Info("      Command completion does not mean sync is finished")

			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, nodeAddCmd, true)
			// Check if command failed (either err != nil or exit code != 0)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				// Check if error is due to failed nodes that need cleanup
				if result != nil {
					stdout := result.GetStdout()
					stderr := result.GetStderr()
					output := stdout + stderr
					if strings.Contains(strings.ToLower(output), "scale failed node") ||
						strings.Contains(strings.ToLower(output), "node remove --clean") {
						ctx.Logger.Warn("Failed nodes detected, cleaning up before retrying...")
						// Execute cleanup command
						cleanupCmd := fmt.Sprintf("yasboot node remove --clean -c %s", clusterName)
						cleanupResult, cleanupErr := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, cleanupCmd, true)
						if cleanupErr == nil && cleanupResult != nil {
							cleanupOutput := cleanupResult.GetStdout() + cleanupResult.GetStderr()
							// Check if cleanup succeeded or if environment is already clean
							if strings.Contains(strings.ToLower(cleanupOutput), "clean") ||
								strings.Contains(strings.ToLower(cleanupOutput), "no scalefailed node") ||
								strings.Contains(strings.ToLower(cleanupOutput), "environment is clean") ||
								cleanupResult.GetExitCode() == 0 {
								ctx.Logger.Info("Cleanup completed, retrying node add...")
								// Retry node add
								result, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, nodeAddCmd, true)
								if err == nil && result != nil && result.GetExitCode() == 0 {
									// Success after cleanup
									if result.GetStdout() != "" {
										ctx.Logger.Info("Command output:")
										for _, line := range strings.Split(result.GetStdout(), "\n") {
											if line != "" {
												ctx.Logger.Info("  %s", line)
											}
										}
									}
									ctx.Logger.Info("Standby instance creation command completed")
									ctx.Logger.Info("Data synchronization may still be in progress")
									return nil
								}
							}
						}
					}
				}
				// If we get here, either cleanup didn't work or error is not about failed nodes
				if err != nil {
					return fmt.Errorf("failed to add standby instance: %w", err)
				}
				if result != nil && result.GetExitCode() != 0 {
					errMsg := result.GetStderr()
					if errMsg == "" {
						errMsg = result.GetStdout()
					}
					return fmt.Errorf("failed to add standby instance: exit code %d: %s", result.GetExitCode(), strings.TrimSpace(errMsg))
				}
			}

			if result.GetStdout() != "" {
				ctx.Logger.Info("Command output:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			ctx.Logger.Info("Standby instance creation command completed")
			ctx.Logger.Info("Data synchronization may still be in progress")
			return nil
		},
	}
}
