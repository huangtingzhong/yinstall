// om_migrate_gate.go - OM 迁主门禁: 参数成对、CUR 为 primary、判定 M1/M2、已完成则幂等跳过
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepMigrateGate() *runner.Step {
	return &runner.Step{
		Name:        "OM Migrate Gate",
		Description: "Validate OM migrate params and detect M1/M2 mode",
		Tags:        []string{"om", "migrate"},

		PreCheck: func(ctx *runner.StepContext) error {
			cur := ResolveOMMigrateCurrent(
				ctx.GetParamString("om_current", ""),
				ctx.GetParamString("om_ip", ""),
			)
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			omIP := strings.TrimSpace(ctx.GetParamString("om_ip", ""))
			ok, err := ValidateOMMigrateParams(cur, nw, omIP)
			if err != nil {
				return err
			}
			if !ok {
				return runner.NewStepSkippedError("om migrate not requested")
			}
			// 回写解析后的源 OM，供后续步使用
			if ctx.Params == nil {
				ctx.Params = map[string]interface{}{}
			}
			ctx.Params["om_current"] = cur
			ctx.Params["om_ip"] = cur
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Migrate Gate")
			cur := ResolveOMMigrateCurrent(
				ctx.GetParamString("om_current", ""),
				ctx.GetParamString("om_ip", ""),
			)
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			listen, err := YasomListenAddr(nw, omBeginPort(ctx))
			if err != nil {
				return err
			}
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				return err
			}

			switch ClassifyOMMigrateStatus(rows, cur, nw) {
			case OMMigrateDualPrimary:
				return fmt.Errorf("dual primary detected (%s and %s); refuse migrate", cur, nw)
			case OMMigrateAlreadyDone:
				ctx.Results["om_migrate_already_done"] = true
				ctx.Results["om_migrate_mode"] = "m1"
				ctx.Results["om_new_listen"] = listen
				if r := FindRowByIP(rows, nw); r != nil {
					ctx.Results["om_new_hostid"] = r.HostID
				}
				ctx.Params["om_ip"] = nw
				ctx.Results["om_ip"] = nw
				ctx.Results["om_migrate_done"] = true
				ctx.Logger.Info("OM migrate already complete: primary=%s (skip destructive steps)", nw)
				omLogPhase(ctx, "gate-done", "already-done")
				return nil
			case OMMigrateNoPrimary:
				return fmt.Errorf("no primary yasom found")
			case OMMigrateCurNotPrimary:
				pri := FindPrimaryRow(rows)
				who := ""
				if pri != nil {
					who = pri.IPAddr
				}
				return fmt.Errorf("current OM %s is not primary (primary=%s)", cur, who)
			}

			mode := MigrateModeFromStatus(rows, nw)
			ctx.Results["om_migrate_mode"] = mode
			ctx.Results["om_new_listen"] = listen
			if r := FindRowByIP(rows, nw); r != nil {
				ctx.Results["om_new_hostid"] = r.HostID
			}
			ctx.Logger.Info("OM migrate gate: mode=%s current=%s new=%s listen=%s", mode, cur, nw, listen)
			omLogPhase(ctx, "gate-done", mode)
			return nil
		},
	}
}
