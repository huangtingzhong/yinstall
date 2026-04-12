// e013_add_standby_instance.go - 添加备库实例
// 本步骤在主库执行 yasboot node add 创建备库实例
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepE013AddStandbyInstance 添加备库实例步骤
func StepE013AddStandbyInstance() *runner.Step {
	return &runner.Step{
		ID:          "E-013",
		Name:        "Add Standby Instance",
		Description: "Create standby database instances using yasboot node add",
		Tags:        []string{"standby", "instance"},

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return err
			}
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// Check cluster_add.toml exists
			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", clusterAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("%s_add.toml not found, run E-011 first", clusterName)
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)

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
			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)

			ctx.Logger.Info("Adding standby database instances")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Config file: %s", clusterAddFile)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Build yasboot node add command
			nodeAddCmd := fmt.Sprintf("cd %s && yasboot node add -c %s -t %s",
				stageDir, clusterName, clusterAddFile)

			// Execute as primary user with environment sourced
			ctx.Logger.Info("Running: yasboot node add ...")
			ctx.Logger.Info("NOTE: This command triggers background data synchronization")
			ctx.Logger.Info("      Command completion does not mean sync is finished")

			result, err := runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, nodeAddCmd)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				var combined string
				if result != nil {
					combined = YasbootCombinedOutput(result.GetStdout(), result.GetStderr())
				}
				// scale failed 时尝试 node remove --clean 后重试一次
				if result != nil && (strings.Contains(strings.ToLower(combined), "scale failed node") ||
					strings.Contains(strings.ToLower(combined), "node remove --clean")) {
					ctx.Logger.Warn("Failed nodes detected, cleaning up before retrying...")
					cleanupCmd := fmt.Sprintf("yasboot node remove --clean -c %s", clusterName)
					cleanupResult, cleanupErr := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, cleanupCmd)
					cleanupOut := ""
					if cleanupResult != nil {
						cleanupOut = YasbootCombinedOutput(cleanupResult.GetStdout(), cleanupResult.GetStderr())
					}
					if cleanupErr == nil && cleanupResult != nil &&
						(strings.Contains(strings.ToLower(cleanupOut), "clean") ||
							strings.Contains(strings.ToLower(cleanupOut), "no scalefailed node") ||
							strings.Contains(strings.ToLower(cleanupOut), "environment is clean") ||
							cleanupResult.GetExitCode() == 0) {
						ctx.Logger.Info("Cleanup completed, retrying node add...")
						result, err = runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, nodeAddCmd)
						if err == nil && result != nil && result.GetExitCode() == 0 {
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
						if result != nil {
							combined = YasbootCombinedOutput(result.GetStdout(), result.GetStderr())
						}
					}
				}
				hint := ExplainYasbootNodeAddFailure(combined)
				ctx.Logger.Error("E-013 yasboot node add failed: %s", hint)
				if combined != "" {
					ctx.Logger.Error("--- full yasboot node add output ---\n%s", combined)
				}
				if err != nil {
					return fmt.Errorf("failed to add standby instance: %w\n%s", err, hint)
				}
				if result != nil && result.GetExitCode() != 0 {
					return fmt.Errorf("failed to add standby instance: exit code %d: %s\n%s",
						result.GetExitCode(), strings.TrimSpace(YasbootCombinedOutput(result.GetStdout(), result.GetStderr())), hint)
				}
			}

			if result != nil && result.GetStdout() != "" {
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
