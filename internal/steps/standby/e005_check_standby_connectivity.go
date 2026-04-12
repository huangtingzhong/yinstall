// e005_check_standby_connectivity.go - 备库节点连通性检查
// 本步骤验证备库节点连通性和 yashan 用户密码
// 当 --with-os=false 时，需要验证 yashan 用户密码是否正确

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE005CheckStandbyConnectivity 备库节点连通性检查步骤
func StepE005CheckStandbyConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "E-005",
		Name:        "Check Standby Connectivity",
		Description: "Verify standby node connectivity and user password",
		Tags:        []string{"standby", "connectivity"},

		PreCheck: func(ctx *runner.StepContext) error {
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("no standby targets specified")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			withOS := ctx.GetParamBool("with_os", true)
			user := ctx.GetParamString("os_user", "yashan")
			password := ctx.GetParamString("os_user_password", "")

			// 如果配置了 OS（with_os=true），密码会在 B-004 步骤中设置，此处跳过验证
			if withOS {
				ctx.Logger.Info("OS configuration enabled, user password will be set in B-004 step")
				return nil
			}

			// 如果跳过 OS 配置（with_os=false），需要验证 yashan 用户密码
			ctx.Logger.Info("OS configuration skipped, validating user password...")

			if password == "" {
				return fmt.Errorf("--os-user-password is required when --with-os=false")
			}

			// 测试 yashan 用户 SSH 登录
			// 使用 sshpass 测试密码是否正确
			ctx.Logger.Info("Testing SSH login for user: %s", user)

			// 先检查 sshpass 是否可用
			result, _ := ctx.Execute("which sshpass 2>/dev/null || echo 'NOT_FOUND'", false)
			if result != nil && strings.Contains(result.GetStdout(), "NOT_FOUND") {
				ctx.Logger.Warn("sshpass not found on target, cannot verify user password automatically")
				ctx.Logger.Warn("Please ensure the password for user '%s' matches --os-user-password", user)
				return nil
			}

			// 获取当前主机（备库）的 IP
			host := ctx.Executor.Host()

			// 尝试使用提供的密码 SSH 登录
			// 注意：这里是在备库本机上执行，测试 localhost 的 yashan 用户
			testCmd := fmt.Sprintf("sshpass -p %s ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 %s@localhost 'echo SSH_OK' 2>&1", commonos.ShellSingleQuote(password), user)
			result, _ = ctx.Execute(testCmd, false)

			if result == nil || !strings.Contains(result.GetStdout(), "SSH_OK") {
				ctx.Logger.Error("SSH login test failed for user '%s' on %s", user, host)
				ctx.Logger.Error("Output: %s", result.GetStdout())
				ctx.Logger.Error("")
				ctx.Logger.Error("The password provided via --os-user-password does not match the actual password.")
				ctx.Logger.Error("Please manually update the password on standby node to match the primary:")
				ctx.Logger.Error("  ssh root@%s \"echo '%s:<password>' | chpasswd\"", host, user)
				ctx.Logger.Error("")
				ctx.Logger.Error("Or run with --with-os=true to configure OS baseline (which sets the password).")
				return fmt.Errorf("user '%s' password verification failed on %s", user, host)
			}

			ctx.Logger.Info("SSH login test successful for user '%s'", user)
			return nil
		},
	}
}
