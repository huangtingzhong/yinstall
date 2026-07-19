// om_recover_secondary.go - 在新 OM 拉起/确认已同步的 secondary (迁主用)
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepRecoverSecondary() *runner.Step {
	return &runner.Step{
		Name:        "OM Recover Secondary",
		Description: "Ensure target has synced secondary yasom before promote",
		Tags:        []string{"om", "migrate"},

		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipIfOMMigrateAlreadyDone(ctx); err != nil {
				return err
			}
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			if nw == "" {
				return fmt.Errorf("om_new is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Recover Secondary")
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			if err := EnsureSecondaryYasom(ctx, nw); err != nil {
				return err
			}
			if listen, ok := ctx.Results["om_secondary_listen"].(string); ok {
				ctx.Results["om_new_listen"] = listen
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			listen, _ := ctx.Results["om_new_listen"].(string)
			if listen == "" {
				listen, _ = ctx.Results["om_secondary_listen"].(string)
			}
			return WaitSecondarySynced(ctx, nw, listen, DefaultSyncWaitTimeout, DefaultSyncWaitInterval)
		},
	}
}

// StepRecoverSecondary 导出供 P2 / CloneStep 复用。
func StepRecoverSecondary() *runner.Step {
	return stepRecoverSecondary()
}
