// om_host_add.go - M2: yasboot host add 将新机加入集群
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepHostAdd() *runner.Step {
	return &runner.Step{
		Name:        "OM Host Add",
		Description: "Add new OM host via yasboot host add; skip on M1",
		Tags:        []string{"om", "migrate", "m2"},

		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipIfOMMigrateAlreadyDone(ctx); err != nil {
				return err
			}
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			mode := omMigrateMode(ctx)
			if mode == "" {
				rows, _, err := YasomStatus(ctx)
				if err == nil {
					mode = MigrateModeFromStatus(rows, nw)
					ctx.Results["om_migrate_mode"] = mode
				}
			}
			if mode == "m1" {
				return runner.NewStepSkippedError("M1: skip OM host add")
			}
			stage := omStageDir(ctx)
			res, _ := ctx.Execute(fmt.Sprintf("test -d %s", stage), true)
			if res == nil || res.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx,
					fmt.Errorf("stage dir not found: %s", stage))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Host Add")
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			if err := HostAddForOM(ctx, nw, ""); err != nil {
				return err
			}
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				return err
			}
			if !HostInCluster(rows, nw) {
				return fmt.Errorf("host %s not present in yasom status after host add", nw)
			}
			if r := FindRowByIP(rows, nw); r != nil {
				ctx.Results["om_new_hostid"] = r.HostID
			}
			ctx.Results["om_migrate_mode"] = "m1" // 此后按已在集群路径继续
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				return err
			}
			if !HostInCluster(rows, nw) {
				return fmt.Errorf("postcheck: %s not in cluster", nw)
			}
			return nil
		},
	}
}
