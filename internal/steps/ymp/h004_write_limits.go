// h003_write_limits.go - 配置 YMP 用户资源限制
// H-004: 写入 nproc limits 到 /etc/security/limits.conf

package ymp

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH004WriteLimits 写入 YMP 用户的 limits 配置
func StepH004WriteLimits() *runner.Step {
	return &runner.Step{
		ID:          "H-004",
		Name:        "Write YMP User Limits",
		Description: "Configure nproc limits for YMP user",
		Tags:        []string{"ymp", "limits"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("ymp_user", "ymp")
			limitsFile := ctx.GetParamString("ymp_limits_file", "/etc/security/limits.conf")
			nproc := ctx.GetParamString("ymp_nproc", "65536")

			// 检查是否已配置，如果已配置则直接跳过（不报错）
			result, _ := ctx.Execute(fmt.Sprintf("grep -q '%s.*nproc' %s 2>/dev/null", user, limitsFile), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Limits already configured for %s, skipping this step", user)
				return nil
			}

			ctx.Logger.Info("Writing limits for user %s: nproc=%s", user, nproc)

			limitsContent := fmt.Sprintf("%s soft nproc %s\n%s hard nproc %s", user, nproc, user, nproc)
			cmd := fmt.Sprintf("echo '%s' >> %s", limitsContent, limitsFile)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to write limits: %w", err)
			}

			ctx.Logger.Info("Limits configured for %s", user)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("ymp_user", "ymp")
			limitsFile := ctx.GetParamString("ymp_limits_file", "/etc/security/limits.conf")

			result, _ := ctx.Execute(fmt.Sprintf("grep '%s.*nproc' %s", user, limitsFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("limits not found for %s in %s", user, limitsFile)
			}
			ctx.Logger.Info("✓ Limits verified: %s", strings.TrimSpace(result.GetStdout()))
			return nil
		},
	}
}
