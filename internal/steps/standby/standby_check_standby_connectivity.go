// standby_check_standby_connectivity.go - 备库节点连通性检查
// 本步骤验证备库节点连通性和 yashan 用户密码
// 当 --with-os=false 时，需要验证 yashan 用户密码是否正确

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepCheckStandbyConnectivity 备库节点连通性检查步骤
func stepCheckStandbyConnectivity() *runner.Step {
	return &runner.Step{
		Name:        "Check Standby Connectivity",
		Description: "Verify standby node connectivity and user password",
		Tags:        []string{"standby", "connectivity"},

		PreCheck: func(ctx *runner.StepContext) error {
			return checkStandbyConnectivity(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Standby Connectivity")
			return checkStandbyConnectivity(ctx)
		},
	}
}

// checkStandbyConnectivity 只读：--skip-os 时用 sshpass 测产品用户密码。
func checkStandbyConnectivity(ctx *runner.StepContext) error {
	targets := ctx.GetParamStringSlice("standby_targets")
	if len(targets) == 0 {
		return fmt.Errorf("no standby targets specified")
	}

	standbyLogPhase(ctx, "check-start", "standby ssh password probe")
	withOS := ctx.GetParamBool("with_os", true)
	user := ctx.GetParamString("os_user", "yashan")
	password := ctx.GetParamString("os_user_password", "")

	if withOS {
		ctx.Logger.Info("OS configuration enabled, user password will be set in B-004 step")
		standbyLogPhase(ctx, "check-done", "skip=with_os")
		return nil
	}

	ctx.Logger.Info("OS configuration skipped, validating user password...")

	if password == "" {
		return fmt.Errorf("--os-user-password is required when --with-os=false")
	}

	ctx.Logger.Info("Testing SSH login for user: %s", user)

	result, _ := ctx.Execute("which sshpass 2>/dev/null || echo 'NOT_FOUND'", false)
	if result != nil && strings.Contains(result.GetStdout(), "NOT_FOUND") {
		ctx.Logger.Warn("sshpass not found on target, cannot verify user password automatically")
		ctx.Logger.Warn("Please ensure the password for user '%s' matches --os-user-password", user)
		standbyLogPhase(ctx, "check-done", "skip=sshpass_missing")
		return nil
	}

	host := ctx.Executor.Host()

	testCmd := fmt.Sprintf("sshpass -p %s ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 %s@localhost 'echo SSH_OK' 2>&1", commonos.ShellSingleQuote(password), user)
	result, _ = ctx.Execute(testCmd, false)

	if result == nil || !strings.Contains(result.GetStdout(), "SSH_OK") {
		out := ""
		if result != nil {
			out = result.GetStdout()
		}
		ctx.Logger.Error("SSH login test failed for user '%s' on %s", user, host)
		ctx.Logger.Error("Output: %s", out)
		ctx.Logger.Error("")
		ctx.Logger.Error("The password provided via --os-user-password does not match the actual password.")
		ctx.Logger.Error("Please manually update the password on standby node to match the primary:")
		ctx.Logger.Error("  ssh root@%s \"echo '%s:<password>' | chpasswd\"", host, user)
		ctx.Logger.Error("")
		ctx.Logger.Error("Or run with --with-os=true to configure OS baseline (which sets the password).")
		return fmt.Errorf("user '%s' password verification failed on %s", user, host)
	}

	standbyLogPhase(ctx, "check-done", fmt.Sprintf("user=%s host=%s", user, host))
	ctx.Logger.Info("SSH login test successful for user '%s'", user)
	return nil
}
