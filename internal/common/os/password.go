package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ProductUserPasswordShellCmd 生成 passwd/chpasswd 命令（含密码，仅用于远端执行；日志请用 ProductUserPasswordShellCmdLabel）。
func ProductUserPasswordShellCmd(user, password string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("product user is empty")
	}
	if password == "" {
		return "", fmt.Errorf("password is empty")
	}
	quoted := ShellSingleQuote(password)
	return fmt.Sprintf("echo %s | passwd %s --stdin 2>/dev/null || { echo %s:%s | chpasswd; }", quoted, user, user, quoted), nil
}

// ProductUserPasswordShellCmdLabel 供 debug 记录的命令摘要（不含密码明文）。
func ProductUserPasswordShellCmdLabel(user string) string {
	return fmt.Sprintf("passwd %s --stdin || chpasswd", strings.TrimSpace(user))
}

// SetProductUserPassword 以 root/sudo 将产品用户密码设为 password（passwd --stdin 或 chpasswd）。
func SetProductUserPassword(ctx *runner.StepContext, user, password string) error {
	if ctx == nil {
		return fmt.Errorf("step context is nil")
	}
	cmd, err := ProductUserPasswordShellCmd(user, password)
	if err != nil {
		return err
	}
	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
		return fmt.Errorf("failed to set password for %s: %w", user, err)
	}
	return nil
}
