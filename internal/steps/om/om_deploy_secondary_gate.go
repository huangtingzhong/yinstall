// om_deploy_secondary_gate.go - P2 门禁: --om-secondary 与主 OM 可用性
package om

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

func stepDeploySecondaryGate() *runner.Step {
	return &runner.Step{
		Name:        "OM Deploy Secondary Gate",
		Description: "Validate --om-secondary and that primary yasom is healthy",
		Tags:        []string{"om", "deploy-secondary"},

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("om_deploy_secondary", true) {
				return runner.NewStepSkippedError("om secondary disabled (--om-secondary=false)")
			}
			return ensurePrimaryYasomHealthyForSecondary(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Deploy Secondary Gate")
			// PreCheck 已校验并写入 Results；此处再确认一次供正常安装路径日志
			if err := ensurePrimaryYasomHealthyForSecondary(ctx); err != nil {
				return err
			}
			pri, _ := ctx.Results["om_primary_ip"].(string)
			omLogPhase(ctx, "gate-done", pri)
			return nil
		},
	}
}

// ensurePrimaryYasomHealthyForSecondary 确认存在 running primary yasom，并写入 om_primary_* Results。
func ensurePrimaryYasomHealthyForSecondary(ctx *runner.StepContext) error {
	rows, _, err := YasomStatus(ctx)
	if err != nil {
		return err
	}
	pri := FindPrimaryRow(rows)
	if pri == nil || !IsPIDRunning(pri.PID) {
		return fmt.Errorf("no healthy primary yasom; cannot deploy secondary OM")
	}
	ctx.Results["om_primary_ip"] = pri.IPAddr
	ctx.Results["om_primary_hostid"] = pri.HostID
	begin := omBeginPort(ctx)
	ctx.Results["om_yasom_port"] = YasomListenPort(begin)
	ctx.Logger.Info("OM deploy secondary gate: primary=%s beginPort=%d yasomPort=%d",
		pri.IPAddr, begin, YasomListenPort(begin))
	return nil
}
