package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepSetPassword 设置产品用户密码（可选）
func stepSetPassword() *runner.Step {
	return &runner.Step{
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
			user := ctx.GetParamString("os_user", "yashan")
			if !ctx.IsForceStep() && shouldSkipPasswordForExistingUser(ctx, user) {
				return fmt.Errorf("user %q already exists (use -f B-004 to reset password)", user)
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

// shouldSkipPasswordForExistingUser 在重复执行 OS 基线时保留已有产品用户密码。
// B-003 在本轮已创建用户（user_existed=false）时仍应设密；仅当用户已存在时跳过。
func shouldSkipPasswordForExistingUser(ctx *runner.StepContext, user string) bool {
	if ctx != nil && ctx.Results != nil {
		if existed, ok := ctx.Results["user_existed"].(bool); ok {
			return existed
		}
	}
	result, _ := ctx.Execute(fmt.Sprintf("id -u %s 2>/dev/null", user), false)
	return result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != ""
}
