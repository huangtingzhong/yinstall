package db

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepC003SetDirOwnership Set directory ownership
func StepC006SetDirOwnership() *runner.Step {
	return &runner.Step{
		ID:          "C-006",
		Name:        "Set Directory Ownership",
		Description: "Set ownership for DB directories",
		Tags:        []string{"db", "directory", "permission"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")

			// Check user exists
			result, _ := ctx.Execute(fmt.Sprintf("id %s", user), false)
			if result == nil || result.GetExitCode() != 0 {
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
