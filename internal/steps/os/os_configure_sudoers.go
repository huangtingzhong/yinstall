package os

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepConfigureSudoers 配置 sudoers（可选/危险）
func stepConfigureSudoers() *runner.Step {
	return &runner.Step{
		Name:        "Configure Sudoers",
		Description: "Configure user passwordless sudo privileges",
		Tags:        []string{"os", "sudo"},
		Optional:    true,
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			enabled := ctx.GetParamBool("os_sudoers_enable", false)
			if !enabled {
				return fmt.Errorf("sudoers configuration not enabled")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-005: Configure Sudoers")
			user := ctx.GetParamString("os_user", "yashan")
			wantLine := fmt.Sprintf("%s  ALL=(ALL) NOPASSWD:ALL", user)

			// 已配置则整步 skip（含不备份）
			if !ctx.IsForceStep() {
				checkCmd := fmt.Sprintf("grep -qF '%s' /etc/sudoers 2>/dev/null || grep -q '^%s[[:space:]]' /etc/sudoers 2>/dev/null", wantLine, user)
				result, _ := ctx.Execute(checkCmd, true)
				if result != nil && result.GetExitCode() == 0 {
					ctx.Logger.Info("Sudoers already configured for %s, skipping (use -f %s to force)", user, ctx.CurrentStepID)
					osLogPhase(ctx, "skip", "already_configured=sudoers")
					return nil
				}
			}

			// 备份 sudoers
			ctx.Execute("cp /etc/sudoers /etc/sudoers.bak_$(date +%F)", true)

			// 添加 sudo 权限
			cmds := []string{
				"chmod +w /etc/sudoers",
				fmt.Sprintf("echo '%s' >> /etc/sudoers", wantLine),
				"chmod -w /etc/sudoers",
				"chmod 400 /etc/sudoers",
			}
			for _, cmd := range cmds {
				if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
					return err
				}
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			result, _ := commonos.ExecuteAsUser(ctx, user, "sudo -n true 2>/dev/null", true)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("sudo verification failed for user %s", user)
			}
			return nil
		},
	}
}
