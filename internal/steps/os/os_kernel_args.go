package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepKernelArgs 写入内核启动参数（可选）
func stepKernelArgs() *runner.Step {
	return &runner.Step{
		Name:        "Write Kernel Args",
		Description: "Configure grub kernel boot arguments",
		Tags:        []string{"os", "kernel", "reboot"},

		PreCheck: func(ctx *runner.StepContext) error {
			enabled := ctx.GetParamBool("os_kernel_args_enable", false)
			if !enabled {
				// Explicitly disabled by user; treat as a no-op.
				return nil
			}
			result, _ := ctx.Execute("which grubby", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf(
					"grubby command not found. Remediation: install grubby (e.g. `yum -y install grubby` / `dnf -y install grubby`), " +
						"or configure kernel args via GRUB manually (update /etc/default/grub then run `grub2-mkconfig -o /boot/grub2/grub.cfg` " +
						"(BIOS) or `grub2-mkconfig -o /boot/efi/EFI/*/grub.cfg` (UEFI)), then reboot",
				)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-012: Write Kernel Args")
			enabled := ctx.GetParamBool("os_kernel_args_enable", false)
			if !enabled {
				ctx.Logger.Info("Kernel args configuration disabled, skipping")
				return nil
			}
			args := ctx.GetParamString("os_kernel_args", "transparent_hugepage=never elevator=deadline LANG=en_US.UTF-8")
			if !ctx.IsForceStep() && kernelArgsAlreadyPresent(ctx, args) {
				ctx.Logger.Info("Kernel args already present, skipping grubby update (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=kernel_args")
				return nil
			}
			cmd := fmt.Sprintf("grubby --update-kernel=ALL --args='%s'", args)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			if err == nil {
				ctx.SetResult("needs_reboot", true)
			}
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			enabled := ctx.GetParamBool("os_kernel_args_enable", false)
			if !enabled {
				return nil
			}
			result, _ := ctx.Execute("grubby --info=ALL | grep args", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("failed to verify kernel args. Remediation: run `grubby --info=ALL` to inspect current kernel args")
			}
			out := result.GetStdout()
			if !strings.Contains(out, "transparent_hugepage=never") {
				return fmt.Errorf(
					"kernel args verification failed: expected transparent_hugepage=never to be present, but it was not found in grubby output. " +
						"Remediation: re-run with `--os-kernel-args` including transparent_hugepage=never, or configure GRUB manually, then reboot",
				)
			}
			return nil
		},
	}
}

// kernelArgsAlreadyPresent 检查 grubby 输出是否已包含全部所需 args 片段。
func kernelArgsAlreadyPresent(ctx *runner.StepContext, args string) bool {
	result, _ := ctx.Execute("grubby --info=ALL 2>/dev/null | grep -E '^args=' || true", false)
	if result == nil {
		return false
	}
	out := result.GetStdout()
	for _, tok := range strings.Fields(args) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if !strings.Contains(out, tok) {
			return false
		}
	}
	return true
}
