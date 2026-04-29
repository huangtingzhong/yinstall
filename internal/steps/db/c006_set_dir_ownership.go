package db

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepC006SetDirOwnership 设置数据库相关目录属主
func StepC006SetDirOwnership() *runner.Step {
	return &runner.Step{
		ID:          "C-006",
		Name:        "Set Directory Ownership",
		Description: "Set ownership for DB directories",
		Tags:        []string{"db", "directory", "permission"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")

			// 确认 OS 用户存在。全量安装时 B-003 早于 C-006；--precheck/--dry-run 不执行 Action，用户可能尚未创建（与 B-010 一致）。
			// 若 --skip-os，则不会跑 B-003，此时用户必须已存在。
			result, _ := ctx.Execute(fmt.Sprintf("id %s", user), false)
			if result == nil || result.GetExitCode() != 0 {
				if ctx.Precheck || ctx.DryRun {
					if ctx.GetParamBool("db_skip_os", false) {
						return fmt.Errorf("user %s does not exist (--skip-os: create the user first or run OS preparation without --skip-os)", user)
					}
					ctx.Logger.Info("C-006: user %s not found yet; skipping strict precheck (B-003 will create user before apply runs C-006)", user)
					return nil
				}
				return fmt.Errorf("user %s does not exist", user)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				user := hctx.GetParamString("os_user", "yashan")
				group := hctx.GetParamString("os_group", "yashan")
				installPath := hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
				dataPath := hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				logPath := hctx.GetParamString("db_log_path", "/data/yashan/log")
				dirs := []string{installPath, dataPath, logPath}
				for _, dir := range dirs {
					hctx.Logger.Info("Setting ownership for: %s -> %s:%s", dir, user, group)
					cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, dir)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to set ownership for %s on %s: %w", dir, th.Host, err)
					}
				}
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				user := hctx.GetParamString("os_user", "yashan")
				installPath := hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
				result, _ := hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", installPath), false)
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
