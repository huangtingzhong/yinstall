// r012_user_limits.go - 产品用户 limits 信息采集
// 采集产品用户（os_user）的 ulimit / limits.conf / pam 限制配置，
// 写入 os/user-limits.json 和相关文本文件。
package collect

import (
	"fmt"
	"path/filepath"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepR012UserLimits 返回 R-012 步骤：采集产品用户资源限制。
func StepR012UserLimits() *runner.Step {
	return &runner.Step{
		ID:   "R-012",
		Name: "Collect user resource limits",
		Action: func(ctx *runner.StepContext) error {
			osUser := getCollectOSUser(ctx)
			dir := filepath.Join(collectHostDir(ctx), "os")

			collectLogPhase(ctx, "plan", fmt.Sprintf("user=%s cmds=7 dir=os", osUser))

			// 用户基本信息
			cmds := []struct {
				cmd  string
				dest string
			}{
				{fmt.Sprintf("id %s 2>/dev/null || true", osUser), filepath.Join(dir, "user-id.txt")},
				{fmt.Sprintf("getent passwd %s 2>/dev/null || true", commonos.ShellSingleQuote(osUser)), filepath.Join(dir, "user-passwd.txt")},
				{fmt.Sprintf("getent group $(id -gn %s) 2>/dev/null || true", osUser), filepath.Join(dir, "user-group.txt")},
				{"cat /etc/security/limits.conf 2>/dev/null || true", filepath.Join(dir, "limits.conf.txt")},
				{"ls /etc/security/limits.d/ 2>/dev/null && cat /etc/security/limits.d/*.conf 2>/dev/null || true", filepath.Join(dir, "limits.d.txt")},
				{"cat /etc/pam.d/system-auth 2>/dev/null || cat /etc/pam.d/common-session 2>/dev/null || true", filepath.Join(dir, "pam-session.txt")},
				// ulimit 需要切换到对应用户执行
				{fmt.Sprintf("su - %s -c 'ulimit -a' 2>/dev/null || true", osUser), filepath.Join(dir, "ulimit.txt")},
			}

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
					appendWarning(ctx, "R-012", err.Error())
				}
			}

			// 结构化摘要
			limits := map[string]string{
				"os_user": osUser,
			}
			r, _ := collectExecute(ctx, fmt.Sprintf("id %s 2>/dev/null", osUser), false, collectCmdTimeout(ctx))
			if r != nil && r.GetExitCode() == 0 {
				limits["id_output"] = r.GetStdout()
			}
			if err := writeJSON(filepath.Join(dir, "user-limits.json"), limits); err != nil {
				appendWarning(ctx, "R-012", err.Error())
			}

			ctx.Logger.Info("[R-012] user limits collected for user %s", osUser)
			return nil
		},
	}
}
