package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB005ConfigureSudoers Configure sudoers (optional/dangerous)
func StepB005ConfigureSudoers() *runner.Step {
	return &runner.Step{
		ID:          "B-005",
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
			user := ctx.GetParamString("os_user", "yashan")

			// 备份 sudoers
			ctx.Execute("cp /etc/sudoers /etc/sudoers.bak_$(date +%F)", true)

			// 检查是否已配置
			checkCmd := fmt.Sprintf("grep -q '^%s' /etc/sudoers", user)
			result, _ := ctx.Execute(checkCmd, true)
			if result.GetExitCode() == 0 {
				return nil
			}

			// 添加 sudo 权限
			cmds := []string{
				"chmod +w /etc/sudoers",
				fmt.Sprintf("echo '%s  ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers", user),
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
			cmd := fmt.Sprintf("su - %s -c 'sudo -n true' 2>/dev/null", user)
			result, _ := ctx.Execute(cmd, true)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("sudo verification failed for user %s", user)
			}
			return nil
		},
	}
}
