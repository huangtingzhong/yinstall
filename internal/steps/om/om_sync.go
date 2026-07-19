// om_sync.go - 升主后 sync OM env 到全节点
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepSync() *runner.Step {
	return &runner.Step{
		Name:        "OM Sync",
		Description: "Sync OM env to all hosts after promote",
		Tags:        []string{"om", "migrate"},

		PreCheck: func(ctx *runner.StepContext) error {
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
			}
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			r := FindRowByIP(rows, nw)
			if r == nil || !strings.EqualFold(r.Role, "primary") {
				// dry-run/precheck 也不得假绿：迁主未完成时明确失败
				return fmt.Errorf("local yasom is not primary on %s (role=%s); complete promote before OM Sync",
					nw, roleOrNone(r))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Sync")
			return SyncYasom(ctx, true)
		},
	}
}

func roleOrNone(r *YasomHostRow) string {
	if r == nil {
		return "none"
	}
	if strings.TrimSpace(r.Role) == "" {
		return "unknown"
	}
	return r.Role
}
