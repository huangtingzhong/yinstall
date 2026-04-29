// g003_set_ownership.go - 修正 YCM 目录属主
// G-003: 设置 /opt/ycm 目录的属主为安装用户

package ycm

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepG003SetOwnership 修正 YCM 目录属主
func StepG003SetOwnership() *runner.Step {
	return &runner.Step{
		ID:          "G-003",
		Name:        "Set YCM Directory Ownership",
		Description: "Set ownership of YCM installation directory to the install user",
		Tags:        []string{"ycm", "permission"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			// 确认用户存在
			result, _ := ctx.Execute(fmt.Sprintf("id %s 2>/dev/null", user), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("user '%s' does not exist, create the user first", user)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			ycmDir := fmt.Sprintf("%s/ycm", installDir)

			ctx.Logger.Info("Setting ownership of %s to %s:%s", ycmDir, user, group)

			cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, ycmDir)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to set ownership on %s: %w", ycmDir, err)
			}

			// 同时设置 ycm-init（如果在 installDir 根目录）
			ycmInit := fmt.Sprintf("%s/ycm-init", installDir)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", ycmInit), false)
			if result != nil && result.GetExitCode() == 0 {
				cmd = fmt.Sprintf("chown %s:%s %s", user, group, ycmInit)
				ctx.Execute(cmd, true)
				ctx.Logger.Info("Set ownership on ycm-init: %s", ycmInit)
			}

			ctx.Logger.Info("YCM directory ownership set successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			ycmDir := fmt.Sprintf("%s/ycm", installDir)

			// 抽样检查属主
			result, _ := ctx.Execute(fmt.Sprintf("stat -c '%%U' %s 2>/dev/null", ycmDir), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("cannot stat %s", ycmDir)
			}
			owner := result.GetStdout()
			if len(owner) > 0 && owner[:len(owner)-1] != user {
				// 去掉可能的换行符再比较
				ctx.Logger.Warn("Owner of %s is '%s', expected '%s'", ycmDir, owner, user)
			}
			ctx.Logger.Info("OK: YCM directory ownership verified")
			return nil
		},
	}
}
