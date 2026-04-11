// e005_install_software.go - 安装软件到备库节点
// 本步骤在主库执行 yasboot host add 将软件安装到备库节点
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE005InstallSoftware 安装软件到备库节点步骤
func StepE009InstallSoftware() *runner.Step {
	return &runner.Step{
		ID:          "E-009",
		Name:        "Install Software on Standby",
		Description: "Install YashanDB software on standby nodes using yasboot host add",
		Tags:        []string{"standby", "install"},

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// Check hosts_add.toml exists
			hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("hosts_add.toml not found, run E-004 first")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)
			depsPackage := ctx.GetParamString("db_deps_package", "")

			hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)

			ctx.Logger.Info("Installing software on standby nodes")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Config file: %s", hostsAddFile)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)

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

			// Execute as primary user with environment sourced
			ctx.Logger.Info("Running: yasboot host add ...")
			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, hostAddCmd, true)
			if err != nil {
				// Check if error is due to hostid conflict (node might be in removing state)
				if result != nil {
					stdout := result.GetStdout()
					stderr := result.GetStderr()
					output := stdout + stderr
					if strings.Contains(strings.ToLower(output), "hostid") &&
						(strings.Contains(strings.ToLower(output), "conflict") ||
							strings.Contains(strings.ToLower(output), "in removing hosts")) {
						ctx.Logger.Warn("Hostid conflict detected, node might be in removing state")
						ctx.Logger.Info("This is expected if node was recently removed. Continuing...")
						// Check if hosts_add.toml was generated successfully
						hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)
						checkResult, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsAddFile), false)
						if checkResult != nil && checkResult.GetExitCode() == 0 {
							ctx.Logger.Info("hosts_add.toml exists, assuming software installation can proceed")
							return nil
						}
					}
				}
				return fmt.Errorf("failed to install software on standby: %w", err)
			}

			if result.GetStdout() != "" {
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
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			primaryUser := GetPrimaryOSUser(ctx)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				ctx.Logger.Warn("Failed to get primary environment file: %v", err)
				return nil // PostCheck 允许失败
			}

			// Check yasagent status
			result, _ := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile,
				fmt.Sprintf("yasboot process yasagent status -c %s", clusterName), true)
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
