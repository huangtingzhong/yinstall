package win_os

import (
	"fmt"

	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func StepW006PrepareDataVolume() *runner.Step {
	return &runner.Step{
		ID:          "W-006",
		Name:        "Prepare Data Volume",
		Description: "Initialize disk and assign data drive letter",
		Tags:        []string{"win-os", "win-os-install", "disk"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			disks := ctx.GetParamStringSlice("os_local_disks")
			if len(disks) == 0 {
				letter := commonwin.DataDriveLetter(ctx)
				if commonwin.VolumeExists(ctx, letter) {
					return runner.NewStepSkippedError("data volume already exists")
				}
				return fmt.Errorf("os_local_disks empty and data volume missing")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-006 data volume")
			if err := commonwin.PrepareDataVolume(ctx); err != nil {
				return err
			}
			ctx.SetResult("os_data_volume_ready", true)
			return nil
		},
	}
}
