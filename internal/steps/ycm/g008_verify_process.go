// g008_verify_process.go - 验证 YCM 进程存在
// G-008: 检查 YCM 相关进程是否正在运行

package ycm

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepG008VerifyProcess 验证 YCM 进程存在
func StepG008VerifyProcess() *runner.Step {
	return &runner.Step{
		ID:          "G-008",
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
				StepID:      "G-008",
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
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			ycmDir := fmt.Sprintf("%s/ycm", installDir)

			ctx.Logger.Info("Checking YCM processes...")

			// 在路径后添加 / 以避免误匹配（如 /opt/ycm 不会匹配到 /opt/ycm2）
			ycmDirPattern := ycmDir
			if !strings.HasSuffix(ycmDirPattern, "/") {
				ycmDirPattern = ycmDirPattern + "/"
			}
			cmd := fmt.Sprintf("ps -ef | grep '%s' | grep -v grep", ycmDirPattern)
			result, _ := ctx.Execute(cmd, false)

			if result == nil || result.GetExitCode() != 0 || strings.TrimSpace(result.GetStdout()) == "" {
				return fmt.Errorf("no YCM processes found (expected processes matching '%s')", ycmDir)
			}

			// 统计进程数
			lines := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
			processCount := 0
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					processCount++
					ctx.Logger.Info("  PID: %s", line)
				}
			}

			ctx.Logger.Info("OK: Found %d YCM process(es) running", processCount)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
