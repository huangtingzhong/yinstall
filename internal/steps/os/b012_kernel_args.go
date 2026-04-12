package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB012WriteKernelArgs Write kernel boot args (optional)
func StepB012WriteKernelArgs() *runner.Step {
	return &runner.Step{
		ID:          "B-012",
		Name:        "Write Kernel Args",
		Description: "Configure grub kernel boot arguments",
		Tags:        []string{"os", "kernel", "reboot"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			enabled := ctx.GetParamBool("os_kernel_args_enable", false)
			if !enabled {
				return fmt.Errorf("kernel args configuration not enabled")
			}
			result, _ := ctx.Execute("which grubby", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("grubby command not found")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			args := ctx.GetParamString("os_kernel_args", "transparent_hugepage=never elevator=deadline LANG=en_US.UTF-8")
			cmd := fmt.Sprintf("grubby --update-kernel=ALL --args='%s'", args)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			if err == nil {
				ctx.SetResult("needs_reboot", true)
			}
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("grubby --info=ALL | grep args", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("failed to verify kernel args")
			}
			return nil
		},
	}
}
