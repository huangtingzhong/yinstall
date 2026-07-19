// r014_time_ntp.go - 时间与 NTP 同步信息采集（可选）
// 采集系统时间、时区、chrony/ntpd 同步状态等信息，
// 写入 os/time/ 目录。chronyc 命令不存在则标记为 warning 继续。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepTimeNtp 返回 R-014 步骤：采集时间与 NTP 同步状态（Optional）。
func stepTimeNtp() *runner.Step {
	return &runner.Step{
		Name:     "Collect time and NTP status",
		Optional: true,
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "time")

			cmds := []struct {
				cmd  string
				dest string
			}{
				{"date -u '+%Y-%m-%dT%H:%M:%SZ'", filepath.Join(dir, "date.txt")},
				{"timedatectl status 2>/dev/null || date", filepath.Join(dir, "timedatectl.txt")},
				{"cat /etc/timezone 2>/dev/null || readlink /etc/localtime 2>/dev/null || true", filepath.Join(dir, "timezone.txt")},
				{"chronyc tracking 2>/dev/null || true", filepath.Join(dir, "chrony-tracking.txt")},
				{"chronyc sources 2>/dev/null || true", filepath.Join(dir, "chrony-sources.txt")},
				{"ntpstat 2>/dev/null || true", filepath.Join(dir, "ntpstat.txt")},
			}

			collectLogPhase(ctx, "plan", fmt.Sprintf("cmds=%d dir=os/time", len(cmds)))

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
					appendWarning(ctx, err.Error())
				}
			}

			ctx.Logger.Info("[R-014] time/NTP info collected to %s", dir)
			return nil
		},
	}
}
