// b006_configure_umask.go - 配置用户 umask
// B-006: 在用户的 .bashrc 中设置 umask 022
// installer.md 9.1: echo "umask 022" >> /home/yashan/.bashrc

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB006ConfigureUmask Configure umask for product user
func StepB006ConfigureUmask() *runner.Step {
	return &runner.Step{
		ID:          "B-006",
		Name:        "Configure Umask",
		Description: "Set umask 022 in user's .bashrc",
		Tags:        []string{"os", "user"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			osUser := ctx.GetParamString("os_user", "yashan")

			// 检查用户是否存在
			result, _ := ctx.Execute(fmt.Sprintf("id %s 2>/dev/null", osUser), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("user %s does not exist, create user first", osUser)
			}

			// 检查 .bashrc 中是否已配置 umask
			homeDir := fmt.Sprintf("/home/%s", osUser)
			result, _ = ctx.Execute(fmt.Sprintf("grep -q 'umask 022' %s/.bashrc 2>/dev/null", homeDir), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("umask 022 already configured in %s/.bashrc, skipping", homeDir)
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osUser := ctx.GetParamString("os_user", "yashan")
			umaskValue := ctx.GetParamString("os_umask", "022")
			homeDir := fmt.Sprintf("/home/%s", osUser)
			bashrc := fmt.Sprintf("%s/.bashrc", homeDir)

			ctx.Logger.Info("Configuring umask %s for user %s", umaskValue, osUser)

			// 确保 .bashrc 存在
			ctx.Execute(fmt.Sprintf("touch %s", bashrc), true)

			// 追加 umask 配置
			cmd := fmt.Sprintf("echo 'umask %s' >> %s", umaskValue, bashrc)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to configure umask: %w", err)
			}

			// 设置文件归属
			ctx.Execute(fmt.Sprintf("chown %s:%s %s", osUser, osUser, bashrc), true)

			ctx.Logger.Info("umask %s configured in %s", umaskValue, bashrc)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			osUser := ctx.GetParamString("os_user", "yashan")
			umaskValue := ctx.GetParamString("os_umask", "022")
			homeDir := fmt.Sprintf("/home/%s", osUser)
			bashrc := fmt.Sprintf("%s/.bashrc", homeDir)

			result, _ := ctx.Execute(fmt.Sprintf("grep 'umask' %s", bashrc), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("umask not found in %s", bashrc)
			}

			output := strings.TrimSpace(result.GetStdout())
			if !strings.Contains(output, fmt.Sprintf("umask %s", umaskValue)) {
				return fmt.Errorf("expected umask %s, found: %s", umaskValue, output)
			}

			ctx.Logger.Info("✓ umask %s verified in %s", umaskValue, bashrc)
			return nil
		},
	}
}
