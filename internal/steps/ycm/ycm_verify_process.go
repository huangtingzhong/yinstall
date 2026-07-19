// g008_verify_process.go - 验证 YCM 进程存在
// G-008: 检查 YCM 相关进程是否正在运行

package ycm

import (
	"fmt"
	"time"

	"github.com/yinstall/internal/runner"
)

// stepVerifyProcess 验证 YCM 进程存在
func stepVerifyProcess() *runner.Step {
	return &runner.Step{
		Name:        "Verify YCM Processes",
		Description: "Check that YCM processes are running",
		Tags:        []string{"ycm", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// Read-only capability check: ps must exist.
			r, _ := ctx.Execute("command -v ps >/dev/null 2>&1", false)
			if r == nil || r.GetExitCode() != 0 {
				return fmt.Errorf("ps command not found")
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Verify YCM Processes",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.YCM.VERIFY.APPLY_ONLY",
				Message:     "This step verifies processes after apply; in --precheck it only checks command availability (it does not require processes to already exist).",
				Remediation: "Run after installation completes (or run without --precheck) to perform the real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmLogPhase(ctx, "plan", "G-008: Verify YCM Processes")
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			pattern := ycmDirPattern(installDir)

			ctx.Logger.Info("Checking YCM processes...")

			ctx.Logger.Info("Waiting %ds for YCM to become ready...", int(ycmHealthInitialWait.Seconds()))
			time.Sleep(ycmHealthInitialWait)

			var processCount int
			var processLines []string
			processOK := false
			for attempt := 1; attempt <= ycmHealthRetryAttempts; attempt++ {
				if attempt > 1 {
					ctx.Logger.Info("Process check retry %d/%d (waiting %ds)...", attempt, ycmHealthRetryAttempts, int(ycmHealthRetryInterval.Seconds()))
					time.Sleep(ycmHealthRetryInterval)
				}

				processCount, processLines, _ = countYCMProcesses(ctx, pattern)
				processOK = processCount > 0
				for _, line := range processLines {
					ctx.Logger.Info("  process: %s", line)
				}
				if processOK {
					break
				}
				ctx.Logger.Warn("attempt %d/%d: no YCM processes (expected matching '%s')", attempt, ycmHealthRetryAttempts, pattern)
			}

			ctx.SetResult(resultKeyYCMProcessOK, processOK)
			ctx.SetResult(resultKeyYCMProcessCount, processCount)

			if !processOK {
				return fmt.Errorf("no YCM processes found (expected processes matching '%s')", pattern)
			}

			ctx.Logger.Info("OK: Found %d YCM process(es) running", processCount)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
