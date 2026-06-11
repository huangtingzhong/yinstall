package os

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB004SetUserPassword 设置产品用户密码（可选）
func StepB004SetUserPassword() *runner.Step {
	return &runner.Step{
		ID:          "B-004",
		Name:        "Set User Password",
		Description: "Set product user password",
		Tags:        []string{"os", "user"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamBool("os_user_nologin", false) {
				return fmt.Errorf("nologin system user: skip password step")
			}
			password := ctx.GetParamString("os_user_password", "")
			if password == "" {
				return fmt.Errorf("password not provided")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-004: Set User Password")
			user := ctx.GetParamString("os_user", "yashan")
			password := ctx.GetParamString("os_user_password", "")
			if err := commonos.SetProductUserPassword(ctx, user, password); err != nil {
				return err
			}
			return nil
		},
	}
}
