// om_stop_primary.go - 停止当前主 OM (升主前须已同步)
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepStopPrimary() *runner.Step {
	return &runner.Step{
		Name:        "OM Stop Primary",
		Description: "Stop current primary yasom after secondary is synced",
		Tags:        []string{"om", "migrate"},
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipIfOMMigrateAlreadyDone(ctx); err != nil {
				return err
			}
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			listen, _ := ctx.Results["om_new_listen"].(string)
			if listen == "" {
				var err error
				listen, err = YasomListenAddr(nw, omBeginPort(ctx))
				if err != nil {
					return err
				}
			}
			// 硬门禁: 未同步禁止 stop
			if err := WaitSecondarySynced(ctx, nw, listen, DefaultSyncWaitTimeout, DefaultSyncWaitInterval); err != nil {
				return fmt.Errorf("refuse to stop primary: %w", err)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Stop Primary")
			cur := strings.TrimSpace(ctx.GetParamString("om_current", ""))
			rows, _, err := YasomStatus(ctx)
			if err == nil {
				if r := FindRowByIP(rows, cur); r == nil || !IsPIDRunning(r.PID) {
					ctx.Logger.Info("Primary OM on %s already stopped; skip stop", cur)
					omLogPhase(ctx, "stop-skip", "already-stopped")
					return nil
				}
			}
			return StopYasom(ctx)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				// status 可能因主已停而从 secondary 查询; 允许 Warn
				ctx.Logger.Warn("yasom status after stop: %v", err)
				return nil
			}
			cur := strings.TrimSpace(ctx.GetParamString("om_current", ""))
			if r := FindRowByIP(rows, cur); r != nil && IsPIDRunning(r.PID) && strings.EqualFold(r.Role, "primary") {
				return fmt.Errorf("primary yasom on %s still running after stop", cur)
			}
			return nil
		},
	}
}
