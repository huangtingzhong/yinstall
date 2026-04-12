// h002_create_user.go - 创建 YMP 用户
// H-003: 创建 YMP 安装用户（默认 ymp）

package ymp

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepH003CreateUser 创建 YMP 用户
func StepH003CreateUser() *runner.Step {
	return &runner.Step{
		ID:          "H-003",
		Name:        "Create YMP User",
		Description: "Create YMP installation user and set password",
		Tags:        []string{"ymp", "user"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("ymp_user", "ymp")

			// 检查用户是否已存在
			result, _ := ctx.Execute(fmt.Sprintf("id %s 2>/dev/null", user), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("User %s already exists, skipping creation", user)
				return fmt.Errorf("user %s already exists", user)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("ymp_user", "ymp")
			password := ctx.GetParamString("ymp_user_password", "aaBB11@@33$$")

			ctx.Logger.Info("Creating user: %s", user)

			// 创建用户
			cmd := fmt.Sprintf("useradd %s", user)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to create user %s: %w", user, err)
			}

			quoted := commonos.ShellSingleQuote(password)
			cmd = fmt.Sprintf("echo %s | passwd %s --stdin 2>/dev/null || echo %s:%s | chpasswd",
				quoted, user, user, quoted)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to set password for %s: %w", user, err)
			}

			ctx.Logger.Info("User %s created successfully", user)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("ymp_user", "ymp")
			result, _ := ctx.Execute(fmt.Sprintf("id %s", user), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("user %s does not exist after creation", user)
			}
			ctx.Logger.Info("✓ User %s verified", user)
			return nil
		},
	}
}
