// e007_check_sync_status.go - 备库同步状态检查
// 本步骤检查备库实例同步状态
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE007CheckSyncStatus 备库同步状态检查步骤
func StepE011CheckSyncStatus() *runner.Step {
	return &runner.Step{
		ID:          "E-011",
		Name:        "Check Sync Status",
		Description: "Check standby instance synchronization status",
		Tags:        []string{"standby", "sync", "status"},

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			primaryUser := GetPrimaryOSUser(ctx)

			ctx.Logger.Info("Checking standby synchronization status")
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)

			// Check cluster status (with environment sourced)
			result, err := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile,
				fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
			if err != nil {
				return fmt.Errorf("failed to check cluster status: %w", err)
			}

			ctx.Logger.Info("Cluster status:")
			for _, line := range strings.Split(result.GetStdout(), "\n") {
				if line != "" {
					ctx.Logger.Info("  %s", line)
				}
			}

			// Check for standby role
			hasStandby := strings.Contains(result.GetStdout(), "standby")
			hasPrimary := strings.Contains(result.GetStdout(), "primary")

			if !hasPrimary {
				return fmt.Errorf("primary database not found in cluster status")
			}

			if !hasStandby {
				ctx.Logger.Warn("No standby database found yet - synchronization may be in progress")
			} else {
				ctx.Logger.Info("Standby database found in cluster")
			}

			// Check instance status
			if strings.Contains(result.GetStdout(), "open") {
				ctx.Logger.Info("Instance status: OPEN")
			} else if strings.Contains(result.GetStdout(), "mounted") {
				ctx.Logger.Info("Instance status: MOUNTED (synchronization in progress)")
			}

			// Check database status
			if strings.Contains(result.GetStdout(), "normal") {
				ctx.Logger.Info("Database status: NORMAL")
			} else {
				ctx.Logger.Warn("Database status may not be normal yet")
			}

			ctx.Logger.Info("Sync status check completed")
			return nil
		},
	}
}
