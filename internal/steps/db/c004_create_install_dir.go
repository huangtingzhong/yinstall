package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC004CreateInstallDir 创建数据库安装/暂存目录
func StepC004CreateInstallDir() *runner.Step {
	return &runner.Step{
		ID:          "C-004",
		Name:        "Create Install Directory",
		Description: "Create DB installation/stage directory",
		Tags:        []string{"db", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := strings.TrimSpace(ctx.GetParamString("db_stage_dir", "/home/yashan/install"))
			if stageDir == "" {
				return fmt.Errorf("db_stage_dir is required")
			}
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				isForce := hctx.IsForceStep()

				// 1) 存在性与类型检查（只读）
				stageQ := commonos.ShellSingleQuote(stageDir)
				existRes, err := hctx.Execute(fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi",
					stageQ, stageQ, stageQ), false)
				if err != nil {
					return fmt.Errorf("failed to check stage directory on %s: %w", th.Host, err)
				}
				kind := ""
				if existRes != nil {
					kind = strings.TrimSpace(existRes.GetStdout())
				}
				if strings.Contains(kind, "NOT_DIR") {
					return fmt.Errorf("stage directory path exists but is not a directory on %s: %s", th.Host, stageDir)
				}

				// 2) 目录缺失则在 apply 阶段创建（全新安装时不存在是常态，不作为 warn 上报）
				if strings.Contains(kind, "MISSING") {
					hctx.Logger.Info("C-004 precheck: stage path %s absent (normal for fresh install); apply will mkdir and chown to %s:%s", stageDir, user, group)
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "C-004",
						StepName:    "Create Install Directory",
						Host:        th.Host,
						Severity:    runner.PrecheckSeverityInfo,
						Code:        "PC.DB.STAGE_DIR.ABSENT",
						Message:     fmt.Sprintf("stage directory does not exist: %s; apply will mkdir and chown to %s:%s (expected for new install)", stageDir, user, group),
						Remediation: "no action required for a new installation; pre-create only if you need a fixed layout or permissions before apply",
					})
					continue
				}

				// 3) 目录已存在（IS_DIR）：属主探测；force 时仅提示将删建；非 force 时非空目录直接失败（与 C-007 空 stage 策略一致，更早 fail-fast）
				if !strings.Contains(kind, "IS_DIR") {
					continue
				}

				ownerRes, _ := hctx.Execute(fmt.Sprintf("stat -c '%%U:%%G' %s 2>/dev/null", stageQ), false)
				owner := ""
				if ownerRes != nil {
					owner = strings.TrimSpace(ownerRes.GetStdout())
				}

				if isForce {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "C-004",
						StepName:    "Create Install Directory",
						Host:        th.Host,
						Severity:    runner.PrecheckSeverityInfo,
						Code:        "PC.DB.STAGE_DIR.FORCE_RECREATE",
						Message:     fmt.Sprintf("--force-steps C-004 detected; apply will rm -rf and recreate directory %s (current owner %s)", stageDir, owner),
						Remediation: "ensure the directory does not contain important files; back up first if needed",
					})
					continue
				}

				if stageDirHasMinDepthEntries(hctx, stageDir) {
					return fmt.Errorf("stage directory %s on %s is not empty; remove existing files manually or re-run with --force-steps C-004 (or global -F) to delete and recreate the directory", stageDir, th.Host)
				}

				// 非 force 且空目录：属主不一致时在 Action 中 chown -R
				expected := fmt.Sprintf("%s:%s", user, group)
				if owner != "" && owner != expected {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "C-004",
						StepName:    "Create Install Directory",
						Host:        th.Host,
						Severity:    runner.PrecheckSeverityWarn,
						Code:        "PC.DB.STAGE_DIR.OWNERSHIP_MISMATCH",
						Message:     fmt.Sprintf("stage directory exists but ownership mismatches: %s current=%s expected=%s; apply will chown -R to fix", stageDir, owner, expected),
						Remediation: "if you do not want recursive chown, fix ownership manually or use a dedicated stage directory",
					})
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-004: Create Install Directory")
			for _, th := range ctx.HostsToRun() {
				dbLogPhase(ctx, "host-start", fmt.Sprintf("host=%s", th.Host))
				hctx := ctx.ForHost(th)
				stageDir := hctx.GetParamString("db_stage_dir", "/home/yashan/install")
				user := hctx.GetParamString("os_user", "yashan")
				group := hctx.GetParamString("os_group", "yashan")
				isForce := hctx.IsForceStep()

				stageQ := commonos.ShellSingleQuote(stageDir)
				result, _ := hctx.Execute(fmt.Sprintf("test -d %s", stageQ), false)
				if result != nil && result.GetExitCode() == 0 {
					// 目录已存在
					if isForce {
						// Force 模式：删除重建
						hctx.Logger.Warn("Force mode: deleting existing directory %s", stageDir)
						if err := commonos.ValidateDeletePath(stageDir); err != nil {
							return fmt.Errorf("refusing to delete stage directory %s on %s: %w", stageDir, th.Host, err)
						}
						if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", stageQ), true); err != nil {
							return fmt.Errorf("failed to delete directory %s on %s: %w", stageDir, th.Host, err)
						}
					} else {
						if stageDirHasMinDepthEntries(hctx, stageDir) {
							return fmt.Errorf("stage directory %s on %s is not empty; remove existing files manually or re-run with --force-steps C-004 (or global -F) to delete and recreate the directory", stageDir, th.Host)
						}
						// 非 Force 模式：检查属主
						result, _ = hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", stageQ), false)
						owner := ""
						if result != nil && result.GetStdout() != "" {
							owner = strings.TrimSpace(result.GetStdout())
						}

						if owner == user {
							// 属主正确，跳过创建
							hctx.Logger.Info("Directory %s already exists with correct ownership (%s), skipping creation", stageDir, user)
							dbLogPhase(hctx, "host-done", fmt.Sprintf("host=%s dir=%s skip=exists", th.Host, stageDir))
							continue
						} else if owner != "" {
							// 属主不正确，修复属主
							hctx.Logger.Info("Directory exists but owner is %s, fixing ownership to %s:%s", owner, user, group)
							cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, stageQ)
							if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
								return fmt.Errorf("failed to fix ownership on %s: %w", th.Host, err)
							}
							hctx.Logger.Info("Fixed ownership: %s (owner: %s:%s)", stageDir, user, group)
							dbLogPhase(hctx, "host-done", fmt.Sprintf("host=%s dir=%s op=chown", th.Host, stageDir))
							continue
						} else {
							// 无法获取属主信息，报错提示使用 force
							return fmt.Errorf("directory %s already exists on %s, use -f %s to delete and recreate", stageDir, th.Host, ctx.CurrentStepID)
						}
					}
				}

				// 目录不存在或已被删除，创建目录
				hctx.Logger.Info("Creating install directory: %s", stageDir)
				cmd := fmt.Sprintf("mkdir -p %s", stageQ)
				if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to create directory %s on %s: %w", stageDir, th.Host, err)
				}
				cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, stageQ)
				if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to set ownership on %s: %w", th.Host, err)
				}
				hctx.Logger.Info("Created directory: %s (owner: %s:%s)", stageDir, user, group)
				dbLogPhase(hctx, "host-done", fmt.Sprintf("host=%s dir=%s", th.Host, stageDir))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				stageDir := hctx.GetParamString("db_stage_dir", "/home/yashan/install")
				user := hctx.GetParamString("os_user", "yashan")
				stageQ := commonos.ShellSingleQuote(stageDir)
				result, _ := hctx.Execute(fmt.Sprintf("test -d %s", stageQ), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("directory %s not found on %s", stageDir, th.Host)
				}
				result, _ = hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", stageQ), false)
				if result != nil && result.GetStdout() != "" {
					owner := result.GetStdout()
					if len(owner) > 0 && owner[len(owner)-1] == '\n' {
						owner = owner[:len(owner)-1]
					}
					if owner != user {
						return fmt.Errorf("directory owner is %s on %s, expected %s", owner, th.Host, user)
					}
				}
			}
			return nil
		},
	}
}

// stageDirHasMinDepthEntries 为 true 表示 stageDir 下存在任意子项（与 C-007 空目录判定一致）。
// 使用 sudo 执行 find，避免目录属主非 SSH 用户时无法列举导致误判为空。
func stageDirHasMinDepthEntries(ctx *runner.StepContext, stageDir string) bool {
	stageQ := commonos.ShellSingleQuote(stageDir)
	cmd := fmt.Sprintf(`test -z "$(find %s -mindepth 1 2>/dev/null | head -1)" && echo EMPTY || echo NOT_EMPTY`, stageQ)
	res, _ := ctx.Execute(cmd, true)
	if res == nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(res.GetStdout()), "NOT_EMPTY")
}
