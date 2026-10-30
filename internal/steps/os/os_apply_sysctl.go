package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepApplySysctl 应用 /etc/sysctl.conf 托管块中的内核参数
func stepApplySysctl() *runner.Step {
	return &runner.Step{
		Name:        "Apply Sysctl Config",
		Description: "Apply kernel parameters from /etc/sysctl.conf",
		Tags:        []string{"os", "kernel"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			confFile := ctx.GetParamString("os_sysctl_file", commonos.DefaultSysctlConfFile)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(confFile)), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("sysctl config file not found"))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "Apply Sysctl Config")
			changed, _ := ctx.Results["os_sysctl_changed"].(bool)
			if !ctx.IsForceStep() && !changed && sysctlRuntimeMatchesWrittenKeys(ctx) {
				ctx.Logger.Info("Sysctl runtime already matches written keys, skipping apply (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=sysctl_runtime")
				return nil
			}

			confFile := ctx.GetParamString("os_sysctl_file", commonos.DefaultSysctlConfFile)
			osLogPhase(ctx, "apply-backend-start", fmt.Sprintf("sysctl -p %s", confFile))
			if _, err := ctx.ExecuteWithCheck("sysctl -p "+commonos.ShellSingleQuote(confFile), true); err != nil {
				osLogPhase(ctx, "apply-backend-fail", runner.TruncateForLog(err.Error(), 80))
				return err
			}
			osLogPhase(ctx, "apply-backend-done", "sysctl -p conf")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return verifyWrittenSysctlKeys(ctx)
		},
	}
}

// sysctlRuntimeMatchesWrittenKeys 抽检本步写入键是否已在运行时生效。
func sysctlRuntimeMatchesWrittenKeys(ctx *runner.StepContext) bool {
	kvs, ok := ctx.Results["os_sysctl_written_keys"].([]commonos.SysctlKV)
	if !ok || len(kvs) == 0 {
		return sysctlRuntimeMatchesConfSwappiness(ctx)
	}
	for _, kv := range kvs {
		r, _ := ctx.Execute(fmt.Sprintf("sysctl -n %s 2>/dev/null", commonos.ShellSingleQuote(kv.Key)), false)
		got := ""
		if r != nil {
			got = r.GetStdout()
		}
		if normalizeSysctlRuntimeValue(got) != normalizeSysctlRuntimeValue(kv.Value) {
			return false
		}
	}
	return true
}

func normalizeSysctlRuntimeValue(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func verifyWrittenSysctlKeys(ctx *runner.StepContext) error {
	kvs, ok := ctx.Results["os_sysctl_written_keys"].([]commonos.SysctlKV)
	if !ok || len(kvs) == 0 {
		want := "0"
		result, _ := ctx.Execute("sysctl -n vm.swappiness 2>/dev/null", false)
		got := ""
		if result != nil {
			got = strings.TrimSpace(result.GetStdout())
		}
		if got != want {
			return fmt.Errorf("sysctl vm.swappiness=%s want %s", got, want)
		}
		return nil
	}
	osLogPhase(ctx, "verify-keys-start", fmt.Sprintf("n=%d", len(kvs)))
	var mismatches []string
	for _, kv := range kvs {
		r, _ := ctx.Execute(fmt.Sprintf("sysctl -n %s 2>/dev/null", commonos.ShellSingleQuote(kv.Key)), false)
		got := ""
		if r != nil {
			got = r.GetStdout()
		}
		if normalizeSysctlRuntimeValue(got) != normalizeSysctlRuntimeValue(kv.Value) {
			mismatches = append(mismatches, fmt.Sprintf("%s got=%q want=%q", kv.Key, strings.TrimSpace(got), strings.TrimSpace(kv.Value)))
		}
	}
	if len(mismatches) > 0 {
		osLogPhase(ctx, "verify-keys-fail", runner.TruncateForLog(strings.Join(mismatches, "; "), 160))
		return fmt.Errorf("sysctl runtime mismatch: %s", strings.Join(mismatches, "; "))
	}
	osLogPhase(ctx, "verify-keys-done", "ok")
	return nil
}

// sysctlRuntimeMatchesConfSwappiness 无 written_keys 时的回退抽检。
func sysctlRuntimeMatchesConfSwappiness(ctx *runner.StepContext) bool {
	r, _ := ctx.Execute("sysctl -n vm.swappiness 2>/dev/null", false)
	if r == nil {
		return false
	}
	return normalizeSysctlRuntimeValue(r.GetStdout()) == "0"
}
