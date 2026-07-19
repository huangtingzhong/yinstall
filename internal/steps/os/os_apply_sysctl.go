package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepApplySysctl 应用 sysctl 配置
func stepApplySysctl() *runner.Step {
	return &runner.Step{
		Name:        "Apply Sysctl Config",
		Description: "Apply kernel parameters",
		Tags:        []string{"os", "kernel"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			configFile := ctx.GetParamString("os_sysctl_file", "/etc/sysctl.d/yashandb.conf")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", configFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("sysctl config file not found"))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-009: Apply Sysctl Config")
			changed, _ := ctx.Results["os_sysctl_changed"].(bool)
			if !ctx.IsForceStep() && !changed && sysctlRuntimeMatchesFile(ctx) {
				ctx.Logger.Info("Sysctl runtime already matches config file, skipping sysctl --system (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=sysctl_runtime")
				return nil
			}
			_, err := ctx.ExecuteWithCheck("sysctl --system", true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			want := "0"
			if ctx.GetParamString("os_sysctl_profile", "") == "mysql" {
				want = "1"
			}
			result, _ := ctx.Execute("sysctl -n vm.swappiness 2>/dev/null", false)
			got := ""
			if result != nil {
				got = strings.TrimSpace(result.GetStdout())
			}
			if got != want {
				return fmt.Errorf("sysctl vm.swappiness=%s want %s", got, want)
			}
			return nil
		},
	}
}

// sysctlRuntimeMatchesFile 抽检配置文件中的关键键是否已在运行时生效。
func sysctlRuntimeMatchesFile(ctx *runner.StepContext) bool {
	configFile := ctx.GetParamString("os_sysctl_file", "/etc/sysctl.d/yashandb.conf")
	raw, _ := ctx.Execute(fmt.Sprintf("cat %s 2>/dev/null || true", configFile), false)
	if raw == nil || strings.TrimSpace(raw.GetStdout()) == "" {
		return false
	}
	want := map[string]string{}
	for _, line := range strings.Split(raw.GetStdout(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "vm.swappiness", "kernel.shmmax":
			want[key] = val
		}
	}
	if len(want) == 0 {
		return false
	}
	for key, expect := range want {
		r, _ := ctx.Execute(fmt.Sprintf("sysctl -n %s 2>/dev/null", key), false)
		if r == nil || strings.TrimSpace(r.GetStdout()) != expect {
			return false
		}
	}
	return true
}
