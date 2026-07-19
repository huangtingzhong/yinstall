// om_host_prepare.go - M2 新机前置: tar/防火墙/空安装目录
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepHostPrepare() *runner.Step {
	return &runner.Step{
		Name:        "OM Host Prepare",
		Description: "Prepare new OM host (tar, firewall, empty install path); skip on M1",
		Tags:        []string{"om", "migrate", "m2"},

		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipIfOMMigrateAlreadyDone(ctx); err != nil {
				return err
			}
			// 仅明确 M1 时跳过; 缺 Gate Results 时按 M2 做幂等准备
			if omMigrateMode(ctx) == "m1" {
				return runner.NewStepSkippedError("M1: skip OM host prepare")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Host Prepare")
			user := omProductUser(ctx)
			home := fmt.Sprintf("/data/%s/yasdb_home", user)
			data := fmt.Sprintf("/data/%s/yasdb_data", user)
			logp := fmt.Sprintf("/data/%s/log", user)

			res, _ := ctx.Execute("command -v tar", false)
			if res == nil || res.GetExitCode() != 0 {
				return fmt.Errorf("tar not found on new OM host; install tar (e.g. yum from lab mirror BaseOS)")
			}
			// host add 以产品用户创建安装目录, 须先有可写的 /data/<user>
			prep := fmt.Sprintf(
				`mkdir -p /data %s %s %s && chown -R %s:%s /data/%s && chmod 755 /data /data/%s`,
				home, data, logp, user, user, user, user)
			if _, err := ctx.ExecuteWithCheck(prep, true); err != nil {
				return fmt.Errorf("prepare install directories on new OM host: %w", err)
			}
			res, _ = ctx.Execute(fmt.Sprintf("ls -A %s 2>/dev/null | head -5", home), true)
			if res != nil && strings.TrimSpace(res.GetStdout()) != "" {
				ctx.Logger.Warn("install home %s is not empty; host add may require empty version path", home)
			}
			_, _ = ctx.Execute("systemctl stop firewalld 2>/dev/null; systemctl disable firewalld 2>/dev/null; true", true)
			_ = EnsureYasbootPathInBashrc(ctx)
			omLogPhase(ctx, "prepare-done", "ok")
			return nil
		},
	}
}
