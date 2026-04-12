// e006_check_standby_begin_port.go - 备库端检查 db 起始端口是否可用

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepE006CheckStandbyBeginPort 在每台备库上检查 db_begin_port（与 --db-port 一致）是否已被占用
func StepE006CheckStandbyBeginPort() *runner.Step {
	return &runner.Step{
		ID:          "E-006",
		Name:        "Check Standby Begin Port Available",
		Description: "Verify db begin port is not in use on each standby host",
		Tags:        []string{"standby", "port", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				hctx.Logger.Info("Checking if port %d is in use on standby %s...", beginPort, th.Host)

				portCmd := fmt.Sprintf("ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", beginPort, beginPort)
				result, err := hctx.Execute(portCmd, false)
				if err != nil {
					return fmt.Errorf("failed to check port %d on %s: %w", beginPort, th.Host, err)
				}
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					portInfo := strings.TrimSpace(result.GetStdout())
					hctx.Logger.Warn("Port %d is already in use on %s: %s", beginPort, th.Host, portInfo)
					yasdbCheckCmd := fmt.Sprintf("netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' | grep -i yasdb", beginPort)
					yasdbResult, _ := hctx.Execute(yasdbCheckCmd, false)
					if yasdbResult != nil && yasdbResult.GetExitCode() == 0 {
						return fmt.Errorf("port %d is already in use by YashanDB on %s; port info: %s; stop the database or use another port", beginPort, th.Host, portInfo)
					}
					return fmt.Errorf("port %d is already in use on %s (--db-port); port info: %s; choose another port or stop the process using it", beginPort, th.Host, portInfo)
				}
				hctx.Logger.Info("✓ Port %d is available on %s", beginPort, th.Host)
			}
			return nil
		},
	}
}
