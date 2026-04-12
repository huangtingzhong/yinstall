// h013_verify_port.go - 验证 YMP 端口监听
// H-013: 检查 YMP 端口（默认 8090）是否监听

package ymp

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH013VerifyPort 验证 YMP 端口监听
func StepH013VerifyPort() *runner.Step {
	return &runner.Step{
		ID:          "H-013",
		Name:        "Verify YMP Port",
		Description: "Check that YMP port is listening (default: 8090)",
		Tags:        []string{"ymp", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			port := ctx.GetParamInt("ymp_port", 8090)

			ctx.Logger.Info("Checking port %d...", port)

			// 尝试 ss 命令
			// 使用精确匹配避免误匹配（如 8090 不会匹配到 80900）
			result, _ := ctx.Execute(fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", port), false)
			if result != nil && result.GetExitCode() == 0 {
				output := strings.TrimSpace(result.GetStdout())
				if output != "" {
					ctx.Logger.Info("Port %d is listening: %s", port, output)
					return nil
				}
			}

			// 尝试 netstat
			// 使用精确匹配避免误匹配（如 8090 不会匹配到 80900）
			result, _ = ctx.Execute(fmt.Sprintf("netstat -anp 2>/dev/null | grep -E ':%d([^0-9]|$)'", port), false)
			if result != nil && result.GetExitCode() == 0 {
				output := strings.TrimSpace(result.GetStdout())
				if output != "" {
					ctx.Logger.Info("Port %d is listening: %s", port, output)
					return nil
				}
			}

			return fmt.Errorf("port %d is not listening", port)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			port := ctx.GetParamInt("ymp_port", 8090)
			ctx.Logger.Info("✓ YMP port %d verified", port)
			return nil
		},
	}
}
