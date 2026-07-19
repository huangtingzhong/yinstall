package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// areRequiredPackagesInstalledForMount 检查挂载 ISO 所需包是否已安装（与 b012 共用逻辑）
func areRequiredPackagesInstalledForMount(ctx *runner.StepContext) bool {
	return areRequiredPackagesInstalled(ctx)
}

// stepMountIso 挂载 ISO（local 模式，可选）
func stepMountIso() *runner.Step {
	return &runner.Step{
		Name:        "Mount ISO",
		Description: "Mount local ISO file for YUM source",
		Tags:        []string{"os", "yum"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !commonos.IsLocalYumMode(commonos.GetYumMode(ctx)) {
				return fmt.Errorf("yum mode is not local")
			}

			if areRequiredPackagesInstalledForMount(ctx) {
				return fmt.Errorf("all required packages already installed, skipping ISO mount")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-013: Mount ISO")
			if err := commonos.EnsureLocalISORepo(ctx); err != nil {
				return fmt.Errorf("failed to prepare local ISO: %w", err)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			mountpoint := ctx.GetParamString("os_iso_mountpoint", "/media")

			result, err := ctx.Execute(fmt.Sprintf("mountpoint -q %s", mountpoint), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("mount point %s is not mounted", mountpoint)
			}

			result, err = ctx.Execute(fmt.Sprintf("ls %s >/dev/null 2>&1", mountpoint), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("mount point %s is not readable", mountpoint)
			}

			result, _ = ctx.Execute(fmt.Sprintf("ls %s | head -5", mountpoint), false)
			if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
				osLogPhase(ctx, "mount-verify-done", fmt.Sprintf(
					"mountpoint=%s contents=%s",
					mountpoint,
					strings.Replace(strings.TrimSpace(result.GetStdout()), "\n", ", ", -1),
				))
			}

			return nil
		},
	}
}
