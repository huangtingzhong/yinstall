// h012_verify_process.go - 验证 YMP 进程
// H-012: 检查 YMP 进程是否存在

package ymp

import (
	"fmt"
	"time"

	"github.com/yinstall/internal/runner"
)

// stepVerifyProcess 验证 YMP 进程
func stepVerifyProcess() *runner.Step {
	return &runner.Step{
		Name:        "Verify YMP Process",
		Description: "Check that YMP processes are running",
		Tags:        []string{"ymp", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			r, _ := ctx.Execute("command -v ps >/dev/null 2>&1", false)
			if r == nil || r.GetExitCode() != 0 {
				return fmt.Errorf("ps command not found")
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Verify YMP Process",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.YMP.VERIFY.APPLY_ONLY",
				Message:     "This step verifies processes after apply; in --precheck it only checks command availability (it does not require processes to already exist).",
				Remediation: "Run after installation completes (or run without --precheck) to perform the real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympLogPhase(ctx, "plan", "H-012: Verify YMP Process")
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			pattern := ympInstallDirPattern(installDir)

			ctx.Logger.Info("Checking YMP processes...")
			ctx.Logger.Info("Waiting %ds for YMP to become ready...", int(ympHealthInitialWait.Seconds()))
			time.Sleep(ympHealthInitialWait)

			var processCount int
			var processLines []string
			processOK := false
			for attempt := 1; attempt <= ympHealthRetryAttempts; attempt++ {
				if attempt > 1 {
					ctx.Logger.Info("Process check retry %d/%d (waiting %ds)...", attempt, ympHealthRetryAttempts, int(ympHealthRetryInterval.Seconds()))
					time.Sleep(ympHealthRetryInterval)
				}

				processCount, processLines = countYMPProcesses(ctx, pattern)
				processOK = processCount > 0
				for _, line := range processLines {
					ctx.Logger.Info("  process: %s", line)
				}
				if processOK {
					break
				}
				ctx.Logger.Warn("attempt %d/%d: no YMP processes (expected matching '%s')", attempt, ympHealthRetryAttempts, pattern)
			}

			ctx.SetResult(resultKeyYMPProcessOK, processOK)
			ctx.SetResult(resultKeyYMPProcessCount, processCount)

			if !processOK {
				return fmt.Errorf("no YMP processes found (expected processes matching '%s')", pattern)
			}

			ctx.Logger.Info("OK: Found %d YMP process(es) running", processCount)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
