// om_recover_old_secondary.go - 旧主降为 secondary (可选)
package om

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepRecoverOldSecondary() *runner.Step {
	return &runner.Step{
		Name:        "OM Recover Old Secondary",
		Description: "Optional: recover old OM host as secondary after migrate",
		Tags:        []string{"om", "migrate"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipIfOMMigrateAlreadyDone(ctx); err != nil {
				return err
			}
			cur := strings.TrimSpace(ctx.GetParamString("om_current", ""))
			if cur == "" {
				return runner.NewStepSkippedError("no om_current")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Recover Old Secondary")
			cur := strings.TrimSpace(ctx.GetParamString("om_current", ""))
			listen, err := YasomListenAddr(cur, omBeginPort(ctx))
			if err != nil {
				ctx.Logger.Warn("old secondary listen addr: %v (non-fatal)", err)
				return nil
			}
			_ = CleanYasom(ctx, true)
			if err := RecoverYasom(ctx, "secondary", listen, true); err != nil {
				// Optional 步: Action 返回 skip 仍会被 RunStep 当成失败, 故仅 Warn 后成功返回
				ctx.Logger.Warn("recover old OM as secondary failed (non-fatal): %v", err)
				return nil
			}
			return nil
		},
	}
}
