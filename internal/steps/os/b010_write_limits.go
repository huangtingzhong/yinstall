package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB010WriteLimitsConfig 写入用户资源 limits 配置
func StepB010WriteLimitsConfig() *runner.Step {
	return &runner.Step{
		ID:          "B-010",
		Name:        "Write Limits Config",
		Description: "Configure user resource limits",
		Tags:        []string{"os", "limits"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			limitsFile := ctx.GetParamString("os_limits_file", "/etc/security/limits.conf")

			// 用户是否存在？全量安装时 B-003 在 B-010 之前会创建用户；--precheck / --dry-run 不执行 Action，故用户可能尚不存在，此时不视为预检失败。
			res, _ := ctx.Execute(fmt.Sprintf("id %s >/dev/null 2>&1", user), false)
			if res == nil || res.GetExitCode() != 0 {
				if ctx.Precheck || ctx.DryRun {
					ctx.Logger.Info("B-010: user %s not found yet; skipping user-dependent precheck (user is created in B-003 before apply runs B-010)", user)
					return nil
				}
				return fmt.Errorf("user '%s' not found (required for limits config)", user)
			}
			// limits 文件是否可读？
			res, _ = ctx.Execute(fmt.Sprintf("test -r %s", limitsFile), true)
			if res == nil || res.GetExitCode() != 0 {
				return fmt.Errorf("limits file not readable: %s", limitsFile)
			}
			// 是否已配置过（避免重复追加）？
			checkCmd := fmt.Sprintf("grep -q '^%s soft nofile' %s 2>/dev/null", user, limitsFile)
			res, _ = ctx.Execute(checkCmd, true)
			if res != nil && res.GetExitCode() == 0 {
				ctx.SetResult("os_limits_already_configured", true)
			} else {
				ctx.SetResult("os_limits_already_configured", false)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			limitsFile := ctx.GetParamString("os_limits_file", "/etc/security/limits.conf")

			// 检查是否已配置
			if v, ok := ctx.Results["os_limits_already_configured"]; ok {
				if b, ok := v.(bool); ok && b {
					return nil
				}
			}
			checkCmd := fmt.Sprintf("grep -q '^%s soft nofile' %s 2>/dev/null", user, limitsFile)
			result, _ := ctx.Execute(checkCmd, true)
			if result != nil && result.GetExitCode() == 0 {
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
