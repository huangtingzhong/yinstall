// om_recover_primary.go - 在新 OM 升主
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepRecoverPrimary() *runner.Step {
	return &runner.Step{
		Name:        "OM Recover Primary",
		Description: "Promote target yasom to primary",
		Tags:        []string{"om", "migrate"},
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipIfOMMigrateAlreadyDone(ctx); err != nil {
				return err
			}
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			rows, _, err := YasomStatus(ctx)
			if err == nil {
				if r := FindRowByIP(rows, nw); r != nil && strings.EqualFold(r.Role, "primary") && IsPIDRunning(r.PID) {
					return nil
				}
			}
			synced, _ := ctx.Results["om_secondary_synced"].(bool)
			if !synced && !ctx.Precheck && !ctx.DryRun {
				return fmt.Errorf("secondary sync flag missing; run OM Recover Secondary first")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Recover Primary")
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			listen, _ := ctx.Results["om_new_listen"].(string)
			if listen == "" {
				var err error
				listen, err = YasomListenAddr(nw, omBeginPort(ctx))
				if err != nil {
					return err
				}
			}
			rows, _, err := YasomStatus(ctx)
			if err == nil {
				if r := FindRowByIP(rows, nw); r != nil && strings.EqualFold(r.Role, "primary") && IsPIDRunning(r.PID) {
					ctx.Logger.Info("Target OM %s already primary; skip recover", nw)
					omLogPhase(ctx, "recover-skip", "already-primary")
					return nil
				}
			}
			return RecoverYasom(ctx, "primary", listen, true)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				return err
			}
			r := FindRowByIP(rows, nw)
			if r == nil || !strings.EqualFold(r.Role, "primary") {
				role := ""
				if r != nil {
					role = r.Role
				}
				return fmt.Errorf("target %s role=%s, want primary", nw, role)
			}
			return nil
		},
	}
}
