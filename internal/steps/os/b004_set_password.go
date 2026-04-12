package os

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB004SetUserPassword Set user password (optional)
func StepB004SetUserPassword() *runner.Step {
	return &runner.Step{
		ID:          "B-004",
		Name:        "Set User Password",
		Description: "Set product user password",
		Tags:        []string{"os", "user"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			password := ctx.GetParamString("os_user_password", "")
			if password == "" {
				return fmt.Errorf("password not provided")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			password := ctx.GetParamString("os_user_password", "")

			quoted := commonos.ShellSingleQuote(password)
			cmd := fmt.Sprintf("echo %s | passwd %s --stdin 2>/dev/null || { echo %s:%s | chpasswd; }", quoted, user, user, quoted)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to set password for %s: %w", user, err)
			}
			return nil
		},
	}
}
