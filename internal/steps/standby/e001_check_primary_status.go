// e001_check_primary_status.go - 主库状态检查
// 本步骤验证主库运行状态、yasboot 可用性、stage 目录存在性
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE001CheckPrimaryStatus 主库状态检查步骤
func StepE001CheckPrimaryStatus() *runner.Step {
	return &runner.Step{
		ID:          "E-001",
		Name:        "Check Primary Status",
		Description: "Verify primary database is running and yasboot is available",
		Tags:        []string{"standby", "primary", "status"},

		PreCheck: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "")
			if clusterName == "" {
				return fmt.Errorf("db_cluster_name parameter is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)

			ctx.Logger.Info("Checking primary database status")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Stage dir: %s", stageDir)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Found primary environment file: %s", envFile)

			// Check yasboot availability (with environment sourced)
			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, "which yasboot", true)
			if err != nil || result.GetExitCode() != 0 {
				return fmt.Errorf("yasboot not found for user %s after sourcing environment", primaryUser)
			}
			yasbootPath := strings.TrimSpace(result.GetStdout())
			ctx.Logger.Info("Found yasboot at: %s", yasbootPath)

			// Check stage directory exists, create if not
			result, _ = ctx.Execute(fmt.Sprintf("test -d %s", stageDir), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Info("Stage directory %s does not exist, creating...", stageDir)
				cmd := fmt.Sprintf("mkdir -p %s && chown %s:%s %s", stageDir, primaryUser, primaryUser, stageDir)
				if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to create stage directory: %w", err)
				}
			}
			ctx.Logger.Info("Stage directory ready: %s", stageDir)

			// Check cluster status (with environment sourced)
			result, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile,
				fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
			if err != nil {
				return fmt.Errorf("failed to check cluster status: %w", err)
			}

			ctx.Logger.Info("Cluster status output:")
			for _, line := range strings.Split(result.GetStdout(), "\n") {
				if line != "" {
					ctx.Logger.Info("  %s", line)
				}
			}

			// Verify primary role
			if !strings.Contains(result.GetStdout(), "primary") {
				return fmt.Errorf("primary database role not found in cluster status")
			}

			// Verify database status is normal
			if !strings.Contains(result.GetStdout(), "normal") && !strings.Contains(result.GetStdout(), "open") {
				ctx.Logger.Warn("Database status may not be optimal, please verify manually")
			}

			// Store environment file path for subsequent steps
			ctx.SetResult("primary_env_file", envFile)

			ctx.Logger.Info("Primary database status check passed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
