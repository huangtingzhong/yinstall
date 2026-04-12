package db

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepC001CreateInstallDir Create DB installation directory
func StepC004CreateInstallDir() *runner.Step {
	return &runner.Step{
		ID:          "C-004",
		Name:        "Create Install Directory",
		Description: "Create DB installation/stage directory",
		Tags:        []string{"db", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			if stageDir == "" {
				return fmt.Errorf("db_stage_dir is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				stageDir := hctx.GetParamString("db_stage_dir", "/home/yashan/install")
				user := hctx.GetParamString("os_user", "yashan")
				group := hctx.GetParamString("os_group", "yashan")
				isForce := hctx.IsForceStep()

				result, _ := hctx.Execute(fmt.Sprintf("test -d %s", stageDir), false)
				if result != nil && result.GetExitCode() == 0 {
					// 目录已存在
					if isForce {
						// Force 模式：删除重建
						hctx.Logger.Warn("Force mode: deleting existing directory %s", stageDir)
						if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", stageDir), true); err != nil {
							return fmt.Errorf("failed to delete directory %s on %s: %w", stageDir, th.Host, err)
						}
					} else {
						// 非 Force 模式：检查属主
						result, _ = hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", stageDir), false)
						owner := ""
						if result != nil && result.GetStdout() != "" {
							owner = strings.TrimSpace(result.GetStdout())
						}

						if owner == user {
							// 属主正确，跳过创建
							hctx.Logger.Info("Directory %s already exists with correct ownership (%s), skipping creation", stageDir, user)
							continue
						} else if owner != "" {
							// 属主不正确，修复属主
							hctx.Logger.Info("Directory exists but owner is %s, fixing ownership to %s:%s", owner, user, group)
							cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, stageDir)
							if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
								return fmt.Errorf("failed to fix ownership on %s: %w", th.Host, err)
							}
							hctx.Logger.Info("Fixed ownership: %s (owner: %s:%s)", stageDir, user, group)
							continue
						} else {
							// 无法获取属主信息，报错提示使用 force
							return fmt.Errorf("directory %s already exists on %s, use --force %s to delete and recreate", stageDir, th.Host, ctx.CurrentStepID)
						}
					}
				}

				// 目录不存在或已被删除，创建目录
				hctx.Logger.Info("Creating install directory: %s", stageDir)
				cmd := fmt.Sprintf("mkdir -p %s", stageDir)
				if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to create directory %s on %s: %w", stageDir, th.Host, err)
				}
				cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, stageDir)
				if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to set ownership on %s: %w", th.Host, err)
				}
				hctx.Logger.Info("Created directory: %s (owner: %s:%s)", stageDir, user, group)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				stageDir := hctx.GetParamString("db_stage_dir", "/home/yashan/install")
				user := hctx.GetParamString("os_user", "yashan")
				result, _ := hctx.Execute(fmt.Sprintf("test -d %s", stageDir), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("directory %s not found on %s", stageDir, th.Host)
				}
				result, _ = hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", stageDir), false)
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
