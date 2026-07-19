// standby_check_primary_status.go - 主库状态检查
// 本步骤验证主库运行状态、yasboot 可用性、stage 目录存在性
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepCheckPrimaryStatus 主库状态检查步骤
func stepCheckPrimaryStatus() *runner.Step {
	return &runner.Step{
		Name:        "Check Primary Status",
		Description: "Verify primary database is running and yasboot is available",
		Tags:        []string{"standby", "primary", "status"},

		PreCheck: func(ctx *runner.StepContext) error {
			return checkPrimaryStatus(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Primary Status")
			return checkPrimaryStatus(ctx)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// checkPrimaryStatus 只读：env / stage / yasboot / cluster status / CE 路径解析。
func checkPrimaryStatus(ctx *runner.StepContext) error {
	if strings.TrimSpace(ctx.GetParamString("primary_env_file", "")) == "" &&
		strings.TrimSpace(ctx.GetParamString("db_cluster_name", "")) == "" {
		return fmt.Errorf("db_cluster_name is required unless primary_env_file is set")
	}

	standbyLogPhase(ctx, "check-start", "yasboot+cluster status")
	primaryUser := GetPrimaryOSUser(ctx)

	ctx.Logger.Info("Checking primary database status")
	ctx.Logger.Info("  Primary user: %s", primaryUser)

	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("failed to get primary environment file: %w", err)
	}
	ctx.Logger.Info("Found primary environment file: %s", envFile)
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		return err
	}
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	ctx.Logger.Info("  Cluster: %s", clusterName)

	if err := EnsurePrimaryStageDirParam(ctx); err != nil {
		return err
	}
	EnsureExpansionPathParams(ctx)
	beginPort := ctx.GetParamInt("db_begin_port", 1688)
	stageDir := strings.TrimSpace(ctx.GetParamString("db_stage_dir", ""))
	if stageDir == "" {
		return fmt.Errorf("db_stage_dir is empty after resolution")
	}
	ctx.Logger.Info("  Primary listen port (db_begin_port): %d", beginPort)
	ctx.Logger.Info("  Stage dir (must exist on OM host): %s", stageDir)

	result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, "which yasboot", true)
	if err != nil || result.GetExitCode() != 0 {
		return fmt.Errorf("yasboot not found for user %s after sourcing environment", primaryUser)
	}
	yasbootPath := strings.TrimSpace(result.GetStdout())
	ctx.Logger.Info("Found yasboot at: %s", yasbootPath)

	result, _ = ctx.Execute(fmt.Sprintf("test -d %s", stageDir), false)
	if result == nil || result.GetExitCode() != 0 {
		return fmt.Errorf("stage directory does not exist on OM host: %s (create it first or set --db-stage-dir / -M/--om)", stageDir)
	}
	ctx.Logger.Info("Stage directory exists: %s", stageDir)

	result, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile,
		fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
	if err != nil {
		return fmt.Errorf("failed to check cluster status: %w", err)
	}

	statusOut := result.GetStdout()
	ctx.Logger.Info("Cluster status output:")
	for _, line := range strings.Split(statusOut, "\n") {
		if line != "" {
			ctx.Logger.Info("  %s", line)
		}
	}

	if !strings.Contains(statusOut, "primary") {
		return fmt.Errorf("primary database role not found in cluster status")
	}

	if !strings.Contains(statusOut, "normal") && !strings.Contains(statusOut, "open") {
		ctx.Logger.Warn("Database status may not be optimal, please verify manually")
	}

	if err := EnsureStandbyCEPath(ctx, statusOut); err != nil {
		return err
	}
	useCE := ctx.GetParamBool("standby_ce_path", false)

	ctx.SetResult("primary_env_file", envFile)

	standbyLogPhase(ctx, "check-done", fmt.Sprintf("cluster=%s port=%d ce_path=%v", clusterName, beginPort, useCE))
	ctx.Logger.Info("Primary database status check passed")
	return nil
}
