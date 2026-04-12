package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB010WriteLimitsConfig Write limits config
func StepB010WriteLimitsConfig() *runner.Step {
	return &runner.Step{
		ID:          "B-010",
		Name:        "Write Limits Config",
		Description: "Configure user resource limits",
		Tags:        []string{"os", "limits"},
		Optional:    false,

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			limitsFile := ctx.GetParamString("os_limits_file", "/etc/security/limits.conf")

			// 检查是否已配置
			checkCmd := fmt.Sprintf("grep -q '^%s soft nofile' %s 2>/dev/null", user, limitsFile)
			result, _ := ctx.Execute(checkCmd, true)
			if result.GetExitCode() == 0 {
				return nil
			}

			config := fmt.Sprintf(`
%s soft nofile  1048576
%s hard nofile  1048576
%s soft nproc   1048576
%s hard nproc   1048576
%s soft rss     unlimited
%s hard rss     unlimited
%s soft stack   8192
%s hard stack   8192
%s soft core    unlimited
%s hard core    unlimited
%s soft memlock -1
%s hard memlock -1
`, user, user, user, user, user, user, user, user, user, user, user, user)

			cmd := fmt.Sprintf("cat >> %s << 'EOF'%sEOF", limitsFile, config)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			limitsFile := ctx.GetParamString("os_limits_file", "/etc/security/limits.conf")
			result, _ := ctx.Execute(fmt.Sprintf("grep '%s soft nofile' %s", user, limitsFile), false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("limits configuration not found")
			}
			return nil
		},
	}
}
