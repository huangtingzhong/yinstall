// om_switch_context.go - 回写 om_ip 供 standby 后续扩容使用新主 OM
package om

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepSwitchContext() *runner.Step {
	return &runner.Step{
		Name:        "OM Switch Context",
		Description: "Point om_ip to new primary OM for subsequent standby phases",
		Tags:        []string{"om", "migrate"},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Switch Context")
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			ctx.Params["om_ip"] = nw
			ctx.Results["om_ip"] = nw
			ctx.Results["om_migrate_done"] = true
			if omMigrateAlreadyDone(ctx) {
				ctx.Logger.Info("OM context already on %s (migrate previously complete)", nw)
			} else {
				ctx.Logger.Info("OM context switched to %s", nw)
			}
			return nil
		},
	}
}
