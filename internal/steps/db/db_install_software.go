package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepInstallSoftware 安装 YashanDB 软件（yasboot package install）
func stepInstallSoftware() *runner.Step {
	return &runner.Step{
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
			homeDir, err := commonos.GetUserHomeDir(ctx, user)
			if err != nil {
				return err
			}
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
			// 若残留存在且版本不匹配，yasboot 通常会失败 → PreCheck 尽早失败。
			// 若同版本已装齐 → Info，apply 将 skip install（幂等续跑）。
			failOnLegacy := !isYACMode && !ctx.IsForceStep()
			allMatch := true
			anyArtifact := false
			for _, th := range hostsToClean {
				hctx := ctx.ForHost(th)
				resEnv, _ := hctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(envFile)), false)
				resLink, _ := hctx.Execute(fmt.Sprintf("test -e %s", commonos.ShellSingleQuote(homeLink)), false)
				hasEnv := resEnv != nil && resEnv.GetExitCode() == 0
				hasLink := resLink != nil && resLink.GetExitCode() == 0

				if !hasEnv && !hasLink {
					ctx.Logger.Info("No legacy .yasboot artifacts detected on %s", th.Host)
					allMatch = false
					continue
				}
				anyArtifact = true
				match, installedVer, expectedVer := softwareHomeMatchesStage(hctx, homeLink, stageDir)
				if match && !ctx.IsForceStep() {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepName: "Install Software",
						Host:     th.Host,
						Severity: runner.PrecheckSeverityInfo,
						Code:     "PC.DB.SOFTWARE_ALREADY_INSTALLED",
						Message:  fmt.Sprintf("yasdb home already installed at matching version %s (expected %s); apply will skip package install unless forced", installedVer, expectedVer),
					})
					ctx.Logger.Info("Software already installed on %s (version=%s); apply will skip install", th.Host, installedVer)
					continue
				}
				allMatch = false
				severity := runner.PrecheckSeverityWarn
				code := "PC.DB.LEGACY_YASBOOT_ARTIFACTS"
				remediation := fmt.Sprintf("run yinstall clean first, or remove legacy files manually; if you intentionally want to override, add %s (will cleanup + use --force during install)", ctx.ForceStepsHint())
				if failOnLegacy {
					severity = runner.PrecheckSeverityError
					code = "PC.DB.LEGACY_YASBOOT_ARTIFACTS_BLOCKING"
				}
				msg := fmt.Sprintf("legacy .yasboot artifacts detected (%s=%v, %s=%v); cleanup may be required before installation", envFile, hasEnv, homeLink, hasLink)
				if installedVer != "" || expectedVer != "" {
					msg = fmt.Sprintf("%s (installed=%q expected=%q)", msg, installedVer, expectedVer)
				}
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepName:    "Install Software",
					Host:        th.Host,
					Severity:    severity,
					Code:        code,
					Message:     msg,
					Remediation: remediation,
				})
				if !isYACMode {
					if failOnLegacy {
						ctx.Logger.Error("Legacy artifacts found on %s; precheck must fail because apply will fail without %s", th.Host, ctx.ForceStepsHint())
					} else {
						ctx.Logger.Warn("Legacy artifacts found on %s; consider %s or run clean first", th.Host, ctx.ForceStepsHint())
					}
				}
			}

			if anyArtifact && allMatch && !ctx.IsForceStep() {
				if ctx.Results == nil {
					ctx.Results = make(map[string]interface{})
				}
				ctx.Results["db_software_install_skip"] = true
			}

			if failOnLegacy {
				for _, issue := range ctx.GetPrecheckIssues() {
					if issue.StepID == ctx.CurrentStepID && issue.Severity == runner.PrecheckSeverityError {
						return fmt.Errorf("legacy .yasboot artifacts detected; apply will fail without %s or cleanup", ctx.ForceStepsHint())
					}
				}
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-020: Install Software")
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
			homeDir, err := commonos.GetUserHomeDir(ctx, user)
			if err != nil {
				return err
			}
			yasbootDir := path.Join(homeDir, ".yasboot")
			envFile := path.Join(yasbootDir, clusterName+".env")
			homeLink := path.Join(yasbootDir, clusterName+"_yasdb_home")

			// 同版本已装且未 force：跳过清理与 package install
			skipInstall := false
			if v, ok := ctx.Results["db_software_install_skip"].(bool); ok && v && !ctx.IsForceStep() {
				skipInstall = true
			}
			if !skipInstall && !ctx.IsForceStep() {
				if match, ver, _ := softwareHomeMatchesStage(ctx, homeLink, stageDir); match {
					skipInstall = true
					ctx.Logger.Info("Software already installed (version=%s); skip package install", ver)
					dbLogPhase(ctx, "install-skip", "already_installed:"+ver)
				}
			}
			if skipInstall {
				ctx.Logger.Info("Skipping yasboot package install (home already matches stage version)")
				return nil
			}

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
				dbLogPhase(ctx, "cleanup-start", fmt.Sprintf("hosts=%d yac=%v force=%v", len(hostsToClean), isYACMode, ctx.IsForceStep()))
				ctx.Logger.Info("Cleaning up legacy artifacts before installation (force=%v, yac=%v)", ctx.IsForceStep(), isYACMode)
				if err := commonos.ValidateDeletePath(envFile); err != nil {
					return fmt.Errorf("refusing to remove legacy env file %q: %w", envFile, err)
				}
				if err := commonos.ValidateDeletePath(homeLink); err != nil {
					return fmt.Errorf("refusing to remove legacy home link %q: %w", homeLink, err)
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
				dbLogPhase(ctx, "cleanup-done", fmt.Sprintf("hosts=%d", len(hostsToClean)))
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

			dbLogPhase(ctx, "install-start", runner.TruncateForLog(installCmd, 120))
			if _, err := commonos.ExecuteAsUserWithCheck(ctx, user, cmd, true); err != nil {
				dbLogPhase(ctx, "install-fail", runner.TruncateForLog(err.Error(), 120))
				return fmt.Errorf("failed to install software: %w", err)
			}
			dbLogPhase(ctx, "install-done", "yasboot package install completed")

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

// softwareHomeMatchesStage 比较 yasdb_home 链接目标/yasql -V 与 stage 包版本是否一致。
func softwareHomeMatchesStage(ctx *runner.StepContext, homeLink, stageDir string) (match bool, installedVer, expectedVer string) {
	expectedVer = expectedDBVersionFromStage(ctx, stageDir)
	if expectedVer == "" {
		pkg := ctx.GetParamString("db_package", "")
		expectedVer = extractYashanPackageVersion(pkg)
	}
	installedVer = probeInstalledYasdbVersion(ctx, homeLink)
	if installedVer == "" || expectedVer == "" {
		return false, installedVer, expectedVer
	}
	return installedVer == expectedVer, installedVer, expectedVer
}

func expectedDBVersionFromStage(ctx *runner.StepContext, stageDir string) string {
	stageQ := commonos.ShellSingleQuote(stageDir)
	cmd := fmt.Sprintf(`basename "$(find %s -maxdepth 2 -type f -name 'database-*.tar.gz' 2>/dev/null | head -1)"`, stageQ)
	res, _ := ctx.Execute(cmd, true)
	if res == nil {
		return ""
	}
	base := strings.TrimSpace(res.GetStdout())
	// database-23.5.2.101-linux-aarch64.tar.gz
	if !strings.HasPrefix(base, "database-") {
		return ""
	}
	rest := strings.TrimPrefix(base, "database-")
	for i, r := range rest {
		if (r < '0' || r > '9') && r != '.' {
			if i == 0 {
				return ""
			}
			return rest[:i]
		}
	}
	return rest
}

func probeInstalledYasdbVersion(ctx *runner.StepContext, homeLink string) string {
	linkQ := commonos.ShellSingleQuote(homeLink)
	// 优先链接目标 basename（常见 /data/yashan/yasdb_home/23.5.2.101）
	res, _ := ctx.Execute(fmt.Sprintf(`if [ -e %s ]; then basename "$(readlink -f %s 2>/dev/null || echo '')"; fi`, linkQ, linkQ), false)
	if res != nil {
		base := strings.TrimSpace(res.GetStdout())
		if looksLikeYashanVersion(base) {
			return base
		}
	}
	// 回退 yasql -V
	verCmd := fmt.Sprintf(`if [ -x %s/bin/yasql ]; then %s/bin/yasql -V 2>/dev/null | head -1; fi`, linkQ, linkQ)
	vres, _ := ctx.Execute(verCmd, false)
	if vres == nil {
		return ""
	}
	return extractVersionFromYasqlV(vres.GetStdout())
}

func looksLikeYashanVersion(s string) bool {
	if s == "" {
		return false
	}
	dots := 0
	for _, r := range s {
		if r == '.' {
			dots++
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return dots >= 2
}

func extractVersionFromYasqlV(out string) string {
	// YashanDB SQL Enterprise Edition Release 23.5.2.101 aarch64
	fields := strings.Fields(out)
	for i, f := range fields {
		if strings.EqualFold(f, "Release") && i+1 < len(fields) {
			v := fields[i+1]
			if looksLikeYashanVersion(v) {
				return v
			}
		}
		if looksLikeYashanVersion(f) {
			return f
		}
	}
	return ""
}
