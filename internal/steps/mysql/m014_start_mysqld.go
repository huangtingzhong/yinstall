package mysql

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepM014StartMysqld starts mysql instance.
func StepM014StartMysqld() *runner.Step {
	return &runner.Step{
		ID:          "M-014",
		Name:        "Start mysqld",
		Description: "Start mysql via systemd, mysqld_safe, or net start",
		Tags:        []string{"mysql", "start", "mysql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout, _ := layoutFromCtx(ctx)
			port := layout.Port
			cfgPath := layout.Other + "/my.cnf"
			if ctx.GetTargetPlatform() == PlatformWindows {
				svc := fmt.Sprintf("MySQL%d", port)
				res, err := ctx.Execute("sc start "+svc, false)
				if err != nil {
					return err
				}
				if res != nil && res.GetExitCode() != 0 {
					out := res.GetStdout() + res.GetStderr()
					// 1056: service already running; 1053/1060 may appear while SCM catches up.
					if !strings.Contains(out, "1056") {
						return fmt.Errorf("sc start %s failed (exit %d): %s", svc, res.GetExitCode(), strings.TrimSpace(out))
					}
				}
				return nil
			}
			if ctx.GetTargetPlatform() == PlatformDarwin {
				cmd := fmt.Sprintf("nohup %s --defaults-file=%s --mysqld-safe-log-timestamps=SYSTEM >/dev/null 2>&1 &",
					quotedBin(layout.Home, "mysqld_safe"), commonos.ShellSingleQuote(cfgPath))
				_, err := ctx.ExecuteWithCheck(cmd, false)
				return err
			}
			unit := ctx.GetParamString("mysql_systemd_unit", fmt.Sprintf("mysqld%d.service", port))
			if v, ok := ctx.Results["mysql_systemd_unit"].(string); ok && v != "" {
				unit = v
			}
			if ctx.GetParamBool("mysql_skip_systemd", false) {
				cmd := fmt.Sprintf("%s --defaults-file=%s --daemonize",
					quotedBin(layout.Home, "mysqld"), commonos.ShellSingleQuote(cfgPath))
				user := ctx.GetParamString("os_user", "mysql")
				_, err := commonos.ExecuteAsUserWithCheck(ctx, user, cmd, true)
				return err
			}
			_, err := ctx.ExecuteWithCheck("systemctl start "+unit, true)
			return err
		},
		PostCheck: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			timeout := 90 * time.Second
			if ctx.GetTargetPlatform() == PlatformWindows {
				timeout = 10 * time.Minute
			}
			return WaitForMysqlReady(ctx, layout, timeout)
		},
	}
}
