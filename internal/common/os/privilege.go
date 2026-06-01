// privilege.go - 判断当前 SSH 会话是否可执行需 root 权限的操作（root 或免密 sudo）

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ResultKeySkipPrivileged 写入 ctx.Results 时表示因无 root/sudo 权限而跳过需特权步骤。
const ResultKeySkipPrivileged = "skip_no_privileged_access"

// PrivilegedAccess 特权执行能力探测结果。
type PrivilegedAccess struct {
	Allowed bool
	User    string
	ViaRoot bool
	ViaSudo bool
	Message string // Allowed==false 时的说明
}

// CheckPrivilegedAccess 检测登录用户是否为 root，或能否非交互 sudo（sudo -n）。
func CheckPrivilegedAccess(ctx *runner.StepContext) PrivilegedAccess {
	if ctx == nil {
		return PrivilegedAccess{
			Allowed: false,
			Message: "step context is nil",
		}
	}

	user, err := GetCurrentUser(ctx)
	if err != nil {
		return PrivilegedAccess{
			Allowed: false,
			Message: fmt.Sprintf("cannot determine login user: %v", err),
		}
	}

	if user == "root" {
		return PrivilegedAccess{
			Allowed: true,
			User:    user,
			ViaRoot: true,
		}
	}

	// 与 SSH/local Executor 一致：sudo -n，避免等待密码
	r, _ := ctx.Execute("sudo -n true", false)
	if r != nil && r.GetExitCode() == 0 {
		return PrivilegedAccess{
			Allowed: true,
			User:    user,
			ViaSudo: true,
		}
	}

	hint := "login as root (e.g. -u root), or configure passwordless sudo (NOPASSWD), and use --sudo=true"
	if !ctx.GetParamBool("sudo", true) {
		hint = "current --sudo=false; privileged steps need root login or --sudo=true with passwordless sudo"
	}

	return PrivilegedAccess{
		Allowed: false,
		User:    user,
		Message: fmt.Sprintf("user %q cannot run privileged commands (%s)", user, hint),
	}
}

// PrivilegedAccessSkipError 若无 root/sudo 则返回 *runner.StepSkippedError（RunStep 在终端显示 skipped）；否则 nil。
func PrivilegedAccessSkipError(ctx *runner.StepContext, operation string) error {
	pa := CheckPrivilegedAccess(ctx)
	if pa.Allowed {
		if ctx != nil && ctx.Logger != nil {
			how := "sudo"
			if pa.ViaRoot {
				how = "root"
			}
			ctx.Logger.Info("Privileged access OK for %s (user=%s, via=%s)", operation, pa.User, how)
		}
		return nil
	}
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.Warn("Skipping %s: %s", operation, pa.Message)
	}
	if ctx != nil && ctx.Results != nil {
		ctx.Results[ResultKeySkipPrivileged] = true
	}
	return runner.NewStepSkippedError(pa.Message)
}

// SkipIfNoPrivilegedAccess 若无 root/sudo 能力则打日志、写入 Results 并返回 true（调用方应跳过特权操作）。
func SkipIfNoPrivilegedAccess(ctx *runner.StepContext, operation string) bool {
	pa := CheckPrivilegedAccess(ctx)
	if pa.Allowed {
		if ctx.Logger != nil {
			how := "sudo"
			if pa.ViaRoot {
				how = "root"
			}
			ctx.Logger.Info("Privileged access OK for %s (user=%s, via=%s)", operation, pa.User, how)
		}
		return false
	}

	if ctx.Logger != nil {
		ctx.Logger.Warn("Skipping %s: %s", operation, pa.Message)
	}
	if ctx.Results != nil {
		ctx.Results[ResultKeySkipPrivileged] = true
	}
	return true
}

// IsPrivilegedSkipped 是否已因无特权而跳过（见 ResultKeySkipPrivileged）。
func IsPrivilegedSkipped(ctx *runner.StepContext) bool {
	if ctx == nil || ctx.Results == nil {
		return false
	}
	v, ok := ctx.Results[ResultKeySkipPrivileged]
	if !ok {
		return false
	}
	skipped, _ := v.(bool)
	return skipped
}

// PrivilegedSkipMessage 返回跳过原因的说明文本（未跳过时为空）。
func PrivilegedSkipMessage(ctx *runner.StepContext) string {
	if !IsPrivilegedSkipped(ctx) {
		return ""
	}
	pa := CheckPrivilegedAccess(ctx)
	return strings.TrimSpace(pa.Message)
}
