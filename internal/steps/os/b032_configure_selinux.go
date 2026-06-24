// b032_configure_selinux.go - SELinux 探测、OS 模式调整（permissive/disabled）。
package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	selinuxModeKeep       = "keep"
	selinuxModePermissive = "permissive"
	selinuxModeDisabled   = "disabled"
)

func normalizeSELinuxMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", selinuxModeKeep:
		return selinuxModeKeep, nil
	case selinuxModePermissive:
		return selinuxModePermissive, nil
	case selinuxModeDisabled:
		return selinuxModeDisabled, nil
	default:
		return "", fmt.Errorf("invalid SELinux mode %q (use keep, permissive, or disabled)", mode)
	}
}

func getSELinuxEnforce(ctx *runner.StepContext) string {
	res, _ := ctx.Execute("getenforce 2>/dev/null || true", false)
	return strings.TrimSpace(res.GetStdout())
}

func configureSELinuxMode(ctx *runner.StepContext, mode string) error {
	normalized, err := normalizeSELinuxMode(mode)
	if err != nil {
		return err
	}
	if normalized == selinuxModeKeep {
		return fmt.Errorf("selinux mode is keep")
	}
	configVal := selinuxModePermissive
	if normalized == selinuxModeDisabled {
		configVal = selinuxModeDisabled
	}
	ctx.LogScriptPreview("shell", "selinux-config", fmt.Sprintf("SELINUX=%s in /etc/selinux/config", configVal))
	sedCmd := fmt.Sprintf(
		`if [ -f /etc/selinux/config ]; then sed -i 's/^SELINUX=.*/SELINUX=%s/' /etc/selinux/config; else echo "SELINUX=%s" > /etc/selinux/config; fi`,
		configVal, configVal,
	)
	if _, err := ctx.ExecuteWithCheck(sedCmd, true); err != nil {
		return err
	}
	_, err = ctx.ExecuteWithCheck("setenforce 0", true)
	return err
}

// StepB032ConfigureSELinux adjusts SELinux mode when --os-selinux-mode is permissive/disabled.
func StepB032ConfigureSELinux() *runner.Step {
	return &runner.Step{
		ID:          "B-032",
		Name:        "Configure SELinux",
		Description: "Set SELinux to permissive or disabled in /etc/selinux/config",
		Tags:        []string{"os", "selinux"},
		Optional:    true,
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			mode := ctx.GetParamString("os_selinux_mode", selinuxModeKeep)
			normalized, err := normalizeSELinuxMode(mode)
			if err != nil {
				return err
			}
			if normalized == selinuxModeKeep {
				return fmt.Errorf("selinux mode is keep")
			}
			current := strings.ToLower(getSELinuxEnforce(ctx))
			if normalized == selinuxModePermissive && (current == "permissive" || current == "disabled") {
				return fmt.Errorf("selinux already permissive or disabled")
			}
			if normalized == selinuxModeDisabled && current == "disabled" {
				return fmt.Errorf("selinux already disabled")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			mode := ctx.GetParamString("os_selinux_mode", selinuxModeKeep)
			osLogPhase(ctx, "plan", "B-032: Configure SELinux mode="+mode)
			return configureSELinuxMode(ctx, mode)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			mode := ctx.GetParamString("os_selinux_mode", selinuxModeKeep)
			normalized, _ := normalizeSELinuxMode(mode)
			current := strings.ToLower(getSELinuxEnforce(ctx))
			if normalized == selinuxModePermissive {
				if current != "permissive" && current != "disabled" {
					return fmt.Errorf("expected permissive or disabled, got %q", current)
				}
				return nil
			}
			if current == "disabled" {
				return nil
			}
			res, _ := ctx.Execute("grep -E '^SELINUX=disabled' /etc/selinux/config", false)
			if res != nil && res.GetExitCode() == 0 {
				ctx.Logger.Info("SELINUX=disabled in config; reboot required for full disable (current: %s)", current)
				return nil
			}
			return fmt.Errorf("failed to set SELinux disabled in config")
		},
	}
}
