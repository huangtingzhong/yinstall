package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC008InstallSoftware Install DB software
func StepC015InstallSoftware() *runner.Step {
	return &runner.Step{
		ID:          "C-015",
		Name:        "Install Software",
		Description: "Install YashanDB software on all nodes",
		Tags:        []string{"db", "install"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			user := ctx.GetParamString("os_user", "yashan")
			hostsPath := path.Join(stageDir, "hosts.toml")

			// Check hosts.toml exists (on first node)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("hosts.toml not found at %s", hostsPath)
			}

			// 清理环境：需要在所有节点执行（YAC 模式下确保每个节点都干净）
			homeDir := fmt.Sprintf("/home/%s", user)
			yasbootDir := path.Join(homeDir, ".yasboot")
			envFile := path.Join(yasbootDir, clusterName+".env")
			homeLink := path.Join(yasbootDir, clusterName+"_yasdb_home")
			killYasomCmd := fmt.Sprintf(
				"pgrep -f %s | xargs -r kill -9 2>/dev/null || true",
				commonos.ShellSingleQuote(commonos.PgrepBinaryClusterArgPattern("yasom", clusterName)),
			)
			killYasagentCmd := fmt.Sprintf(
				"pgrep -f %s | xargs -r kill -9 2>/dev/null || true",
				commonos.ShellSingleQuote(commonos.PgrepBinaryClusterArgPattern("yasagent", clusterName)),
			)

			// 获取需要清理的节点列表
			hostsToClean := ctx.TargetHosts
			if len(hostsToClean) == 0 {
				// 单机模式：只清理当前节点
				hostsToClean = []runner.TargetHost{{Host: ctx.Executor.Host(), Executor: ctx.Executor}}
			}

			isYACMode := len(ctx.TargetHosts) > 1

			if isYACMode {
				ctx.Logger.Info("YAC mode detected: cleaning up all %d nodes before installation", len(hostsToClean))
			}

			// 遍历所有节点进行清理
			for _, th := range hostsToClean {
				hctx := ctx.ForHost(th)
				// 单机模式：先检查是否存在旧安装，不存在则跳过清理
				if !isYACMode {
					resEnv, _ := hctx.Execute(fmt.Sprintf("test -f %s", envFile), false)
					resLink, _ := hctx.Execute(fmt.Sprintf("test -e %s", homeLink), false)
					if (resEnv == nil || resEnv.GetExitCode() != 0) && (resLink == nil || resLink.GetExitCode() != 0) {
						ctx.Logger.Info("No previous installation found on %s, skipping cleanup", th.Host)
						continue
					}
				}

				ctx.Logger.Info("Cleaning up previous installation on %s", th.Host)
				ctx.Logger.Info("  - Killing yasom/yasagent processes for cluster: %s", clusterName)
				hctx.Execute(killYasomCmd, true)
				hctx.Execute(killYasagentCmd, true)
				hctx.Execute("sleep 2", false)

				ctx.Logger.Info("  - Removing .yasboot artifacts: %s, %s", envFile, homeLink)
				hctx.Execute(fmt.Sprintf("rm -f %s", envFile), true)
				hctx.Execute(fmt.Sprintf("rm -rf %s", homeLink), true)

				ctx.Logger.Info("Cleanup completed on %s", th.Host)
			}

			if isYACMode {
				ctx.Logger.Info("All %d nodes cleaned up successfully", len(hostsToClean))
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			depsPackage := ctx.GetParamString("db_deps_package", "")
			user := ctx.GetParamString("os_user", "yashan")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			hostsPath := path.Join(stageDir, "hosts.toml")

			// C-008 仅在首节点执行（yasboot package install 会自动在所有节点安装软件）
			ctx.Logger.Info("Installing YashanDB software on first node: %s", ctx.Executor.Host())
			if len(ctx.TargetHosts) > 1 {
				ctx.Logger.Info("yasboot will automatically distribute and install on all %d nodes", len(ctx.TargetHosts))
			}

			// 判断是否需要 --force 参数
			// YAC 模式：PreCheck 已清理所有节点，使用 --force 确保安装成功
			// 单机模式：用户显式传入 --force C-008 时使用
			forceInstall := ctx.IsForceStep() || len(ctx.TargetHosts) > 1

			var installCmd string
			if depsPackage != "" {
				ctx.Logger.Info("Using SSL deps package: %s", depsPackage)
				installCmd = fmt.Sprintf("%s package install -t %s --deps %s", yasbootPath, hostsPath, depsPackage)
			} else {
				installCmd = fmt.Sprintf("%s package install -t %s", yasbootPath, hostsPath)
			}
			
			if forceInstall {
				installCmd += " --force"
				if len(ctx.TargetHosts) > 1 {
					ctx.Logger.Info("Using --force for yasboot package install (YAC mode, all nodes cleaned)")
				} else {
					ctx.Logger.Info("Using --force for yasboot package install (user specified)")
				}
			}

			cmd := fmt.Sprintf("cd %s && su - %s -c '%s'", stageDir, user, installCmd)
			ctx.Logger.Info("Executing: su - %s -c '%s'", user, installCmd)

			result, err := ctx.Execute(cmd, true)
			if err != nil {
				return fmt.Errorf("failed to install software: %w", err)
			}

			if result != nil && result.GetExitCode() != 0 {
				// Show detailed error information
				errMsg := result.GetStderr()
				if errMsg == "" {
					errMsg = result.GetStdout()
				}
				ctx.Logger.Error("Install command failed:")
				ctx.Logger.Error("  Exit code: %d", result.GetExitCode())
				if errMsg != "" {
					ctx.Logger.Error("  Output: %s", errMsg)
				}
				return fmt.Errorf("installation failed: %s", errMsg)
			}

			ctx.Logger.Info("Software installation completed successfully")
			if len(ctx.TargetHosts) > 1 {
				ctx.Logger.Info("Software has been distributed to all %d nodes", len(ctx.TargetHosts))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// Check yasom and yasagent processes
			result, _ := ctx.Execute("pgrep -x yasom", false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("yasom process not found")
			} else {
				ctx.Logger.Info("yasom process running: PID %s", strings.TrimSpace(result.GetStdout()))
			}

			result, _ = ctx.Execute("pgrep -x yasagent", false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("yasagent process not found")
			} else {
				ctx.Logger.Info("yasagent process running: PID %s", strings.TrimSpace(result.GetStdout()))
			}

			return nil
		},
	}
}
