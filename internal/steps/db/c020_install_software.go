package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC020InstallSoftware 安装 YashanDB 软件（yasboot package install）
func StepC020InstallSoftware() *runner.Step {
	return &runner.Step{
		ID:          "C-020",
		Name:        "Install Software",
		Description: "Install YashanDB software on all nodes",
		Tags:        []string{"db", "install"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			user := ctx.GetParamString("os_user", "yashan")
			hostsPath := path.Join(stageDir, "hosts.toml")

			// 确认 hosts.toml 存在（首节点）
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("hosts.toml not found at %s", hostsPath))
			}

			// 只读探测历史残留（此处不做清理）
			homeDir := fmt.Sprintf("/home/%s", user)
			yasbootDir := path.Join(homeDir, ".yasboot")
			envFile := path.Join(yasbootDir, clusterName+".env")
			homeLink := path.Join(yasbootDir, clusterName+"_yasdb_home")

			// 获取需要清理的节点列表
			hostsToClean := ctx.TargetHosts
			if len(hostsToClean) == 0 {
				// 单机模式：只清理当前节点
				hostsToClean = []runner.TargetHost{{Host: ctx.Executor.Host(), Executor: ctx.Executor}}
			}

			isYACMode := len(ctx.TargetHosts) > 1

			if isYACMode {
				ctx.Logger.Info("YAC mode detected: will validate legacy artifacts on all %d nodes before installation", len(hostsToClean))
			}

			// 遍历所有节点进行只读探测
			// 说明：
			// 单机模式下，除非用户显式 force 本步，否则 C-020 Action 不会清理历史 .yasboot 产物。
			// 若残留存在，yasboot 通常会失败（除非安装侧 --force）。因此 PreCheck 需尽早失败，
			// 避免 apply 必然失败却拖到 Action。
			failOnLegacy := !isYACMode && !ctx.IsForceStep()
			for _, th := range hostsToClean {
				hctx := ctx.ForHost(th)
				resEnv, _ := hctx.Execute(fmt.Sprintf("test -f %s", envFile), false)
				resLink, _ := hctx.Execute(fmt.Sprintf("test -e %s", homeLink), false)
				hasEnv := resEnv != nil && resEnv.GetExitCode() == 0
				hasLink := resLink != nil && resLink.GetExitCode() == 0

				if hasEnv || hasLink {
					severity := runner.PrecheckSeverityWarn
					code := "PC.DB.LEGACY_YASBOOT_ARTIFACTS"
					remediation := "run yinstall clean first, or remove legacy files manually; if you intentionally want to override, add --force-steps C-020 (will cleanup + use --force during install)"
					if failOnLegacy {
						severity = runner.PrecheckSeverityError
						code = "PC.DB.LEGACY_YASBOOT_ARTIFACTS_BLOCKING"
					}
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "C-020",
						StepName:    "Install Software",
						Host:        th.Host,
						Severity:    severity,
						Code:        code,
						Message:     fmt.Sprintf("legacy .yasboot artifacts detected (%s=%v, %s=%v); cleanup may be required before installation", envFile, hasEnv, homeLink, hasLink),
						Remediation: remediation,
					})
					if !isYACMode {
						if failOnLegacy {
							ctx.Logger.Error("Legacy artifacts found on %s; precheck must fail because apply will fail without --force-steps C-020", th.Host)
						} else {
							ctx.Logger.Warn("Legacy artifacts found on %s; consider --force-steps C-020 or run clean first", th.Host)
						}
					}
				} else {
					ctx.Logger.Info("No legacy .yasboot artifacts detected on %s", th.Host)
				}
			}

			if failOnLegacy {
				for _, issue := range ctx.GetPrecheckIssues() {
					if issue.StepID == "C-020" && issue.Severity == runner.PrecheckSeverityError {
						return fmt.Errorf("legacy .yasboot artifacts detected; apply will fail without --force-steps C-020 or cleanup")
					}
				}
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			depsPackage := ctx.GetParamString("db_deps_package", "")
			user := ctx.GetParamString("os_user", "yashan")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			hostsPath := path.Join(stageDir, "hosts.toml")

			// C-020 仅在首节点执行（yasboot package install 会自动在所有节点安装软件）
			ctx.Logger.Info("Installing YashanDB software on first node: %s", ctx.Executor.Host())
			if len(ctx.TargetHosts) > 1 {
				ctx.Logger.Info("yasboot will automatically distribute and install on all %d nodes", len(ctx.TargetHosts))
			}

			// 安装前清理历史产物（写操作放在 Action；PreCheck 只做只读探测）
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

			hostsToClean := ctx.TargetHosts
			if len(hostsToClean) == 0 {
				hostsToClean = []runner.TargetHost{{Host: ctx.Executor.Host(), Executor: ctx.Executor}}
			}
			isYACMode := len(ctx.TargetHosts) > 1
			forceCleanup := isYACMode || ctx.IsForceStep()
			if forceCleanup {
				ctx.Logger.Info("Cleaning up legacy artifacts before installation (force=%v, yac=%v)", ctx.IsForceStep(), isYACMode)
				if !commonos.IsSafeUnixRmRfPath(envFile) || !commonos.IsSafeUnixRmRfPath(homeLink) {
					return fmt.Errorf("refusing to remove legacy paths: env=%q link=%q (not under allowed installation roots)", envFile, homeLink)
				}
				envQ := commonos.ShellSingleQuote(envFile)
				linkQ := commonos.ShellSingleQuote(homeLink)
				for _, th := range hostsToClean {
					hctx := ctx.ForHost(th)
					ctx.Logger.Info("Cleaning up previous installation on %s", th.Host)
					hctx.Execute(killYasomCmd, true)
					hctx.Execute(killYasagentCmd, true)
					hctx.Execute("sleep 2", false)
					hctx.Execute(fmt.Sprintf("rm -f %s", envQ), true)
					hctx.Execute(fmt.Sprintf("rm -rf %s", linkQ), true)
				}
			}

			// 判断是否需要 --force 参数
			// YAC 模式：PreCheck 已清理所有节点，使用 --force 确保安装成功
			// 单机模式：用户显式传入 --force C-020 时使用
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

			cmd := fmt.Sprintf("cd %s && %s", stageDir, installCmd)
			ctx.Logger.Info("Executing as %s: %s", user, installCmd)

			result, err := commonos.ExecuteAsUser(ctx, user, cmd, true)
			if err != nil {
				return fmt.Errorf("failed to install software: %w", err)
			}

			if result != nil && result.GetExitCode() != 0 {
				// 输出详细错误便于定位
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
			// 检查 yasom / yasagent 进程是否存在（尽力而为）
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
