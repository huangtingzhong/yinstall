// e012_install_software.go - 安装软件到备库节点
// 本步骤在主库执行 yasboot host add 将软件安装到备库节点
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepE012InstallSoftware 安装软件到备库节点步骤
func StepE012InstallSoftware() *runner.Step {
	return &runner.Step{
		ID:          "E-012",
		Name:        "Install Software on Standby",
		Description: "Install YashanDB software on standby nodes using yasboot host add",
		Tags:        []string{"standby", "install"},

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// Check hosts_add.toml exists
			hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("hosts_add.toml not found, run E-011 first"))
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)
			depsPackage := ctx.GetParamString("db_deps_package", "")

			hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)

			ctx.Logger.Info("Installing software on standby nodes")
			ctx.Logger.Info("  Config file: %s", hostsAddFile)
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

			// Build yasboot host add command
			// Add --force to skip check for existing cluster config files
			var hostAddCmd string
			if depsPackage != "" {
				ctx.Logger.Info("  Using deps package: %s", depsPackage)
				hostAddCmd = fmt.Sprintf("cd %s && yasboot host add -c %s -t %s --deps %s --force",
					stageDir, clusterName, hostsAddFile, depsPackage)
			} else {
				hostAddCmd = fmt.Sprintf("cd %s && yasboot host add -c %s -t %s --force",
					stageDir, clusterName, hostsAddFile)
			}

			// 与 E-011 相同：用 runYasbootOnPrimaryWithEnvFile，避免 ExecuteAsUserWithEnv* 破坏 -p 引号且忽略退出码导致误报成功
			ctx.Logger.Info("Running: yasboot host add ...")
			result, err := runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, hostAddCmd)
			if err != nil {
				var combined string
				if result != nil {
					combined = YasbootCombinedOutput(result.GetStdout(), result.GetStderr())
				}
				hint := ExplainYasbootHostAddFailure(combined)
				ctx.Logger.Error("E-012 yasboot host add failed: %s", hint)
				if combined != "" {
					ctx.Logger.Error("--- full yasboot host add output ---\n%s", combined)
				}
				return fmt.Errorf("failed to install software on standby: %w\n%s", err, hint)
			}

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Command output:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			ctx.Logger.Info("Software installation on standby nodes completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				ctx.Logger.Warn("Failed to get primary environment file: %v", err)
				return nil // PostCheck 允许失败
			}
			_ = SyncPrimaryClusterNameFromEnvFile(ctx, envFile)
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// Check yasagent status（不强制成功，避免次要失败阻断）
			result, _ := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile,
				fmt.Sprintf("yasboot process yasagent status -c %s", clusterName))
			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Yasagent status:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			return nil
		},
	}
}
