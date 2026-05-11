// h004_write_limits.go - 配置 YMP 用户资源限制
// H-004: 写入 nproc limits 到 /etc/security/limits.conf

package ymp

import (
	"fmt"

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
			// 默认值与 YashanDB 官方准备建议一致：
			// nofile=1048576, nproc=1048576, rss=unlimited, stack=8192
			nproc := ctx.GetParamString("ymp_nproc", "1048576")
			nofile := ctx.GetParamString("ymp_nofile", "1048576")
			rss := ctx.GetParamString("ymp_rss", "unlimited")
			stack := ctx.GetParamString("ymp_stack", "8192")

			// 幂等性：只补齐缺失项（不再依赖仅通过 nproc 来判断是否已配置完整）。
			ensureLine := func(line string) error {
				cmd := fmt.Sprintf("echo '%s' >> %s", line, limitsFile)
				if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to write limits line %q: %w", line, err)
				}
				return nil
			}

			hasAnyFor := func(scope string, kind string) bool {
				// 只要存在该 kind（不管具体 value 是多少）就认为已配置，避免用户已有自定义值时重复追加。
				// 示例：'^ymp[[:space:]]+soft[[:space:]]+nofile[[:space:]]+'
				pattern := fmt.Sprintf("^%s[[:space:]]+%s[[:space:]]+%s[[:space:]]+", user, scope, kind)
				cmd := fmt.Sprintf("grep -Eq '%s' %s 2>/dev/null", pattern, limitsFile)
				res, _ := ctx.Execute(cmd, false)
				return res != nil && res.GetExitCode() == 0
			}

			var toEnsure []string

			if !hasAnyFor("soft", "nofile") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s soft nofile %s", user, nofile))
			}
			if !hasAnyFor("hard", "nofile") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s hard nofile %s", user, nofile))
			}
			if !hasAnyFor("soft", "nproc") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s soft nproc %s", user, nproc))
			}
			if !hasAnyFor("hard", "nproc") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s hard nproc %s", user, nproc))
			}
			if !hasAnyFor("soft", "rss") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s soft rss %s", user, rss))
			}
			if !hasAnyFor("hard", "rss") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s hard rss %s", user, rss))
			}
			if !hasAnyFor("soft", "stack") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s soft stack %s", user, stack))
			}
			if !hasAnyFor("hard", "stack") {
				toEnsure = append(toEnsure, fmt.Sprintf("%s hard stack %s", user, stack))
			}

			if len(toEnsure) == 0 {
				ctx.Logger.Info("Limits already configured for %s; nothing to add", user)
				return nil
			}

			ctx.Logger.Info("Writing missing limits for user %s", user)
			for _, line := range toEnsure {
				if err := ensureLine(line); err != nil {
					return err
				}
			}

			ctx.Logger.Info("Limits ensured for %s", user)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("ymp_user", "ymp")
			limitsFile := ctx.GetParamString("ymp_limits_file", "/etc/security/limits.conf")

			requiredKinds := []string{"nofile", "nproc", "rss", "stack"}
			for _, kind := range requiredKinds {
				softPattern := fmt.Sprintf("^%s[[:space:]]+soft[[:space:]]+%s[[:space:]]+", user, kind)
				hardPattern := fmt.Sprintf("^%s[[:space:]]+hard[[:space:]]+%s[[:space:]]+", user, kind)

				softCmd := fmt.Sprintf("grep -Eq '%s' %s 2>/dev/null", softPattern, limitsFile)
				softRes, _ := ctx.Execute(softCmd, false)
				if softRes == nil || softRes.GetExitCode() != 0 {
					return fmt.Errorf("limits missing: %s soft %s in %s", user, kind, limitsFile)
				}

				hardCmd := fmt.Sprintf("grep -Eq '%s' %s 2>/dev/null", hardPattern, limitsFile)
				hardRes, _ := ctx.Execute(hardCmd, false)
				if hardRes == nil || hardRes.GetExitCode() != 0 {
					return fmt.Errorf("limits missing: %s hard %s in %s", user, kind, limitsFile)
				}
			}

			ctx.Logger.Info("OK: Limits verified for %s in %s", user, limitsFile)
			return nil
		},
	}
}
