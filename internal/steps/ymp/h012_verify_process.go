// h011_verify_process.go - 验证 YMP 进程
// H-012: 检查 YMP 进程是否存在

package ymp

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH012VerifyProcess 验证 YMP 进程
func StepH012VerifyProcess() *runner.Step {
	return &runner.Step{
		ID:          "H-012",
		Name:        "Verify YMP Process",
		Description: "Check that YMP processes are running",
		Tags:        []string{"ymp", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Checking YMP processes...")

			result, _ := ctx.Execute("ps -ef | grep -v grep | grep ymp", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("no YMP processes found")
			}

			output := strings.TrimSpace(result.GetStdout())
			if output == "" {
				return fmt.Errorf("no YMP processes found")
			}

			lines := strings.Split(output, "\n")
			ctx.Logger.Info("YMP processes found: %d", len(lines))
			for _, line := range lines {
				ctx.Logger.Info("  %s", strings.TrimSpace(line))
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("ps -ef | grep -v grep | grep ymp | wc -l", false)
			if result != nil {
				count := strings.TrimSpace(result.GetStdout())
				ctx.Logger.Info("✓ YMP process count: %s", count)
			}
			return nil
		},
	}
}
