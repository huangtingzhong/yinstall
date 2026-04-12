// h000_check_port.go - 检查 YMP 端口是否被占用
// H-001: 在安装前检查 YMP 端口是否已被使用，如果已使用则报错退出

package ymp

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH001CheckPort 检查 YMP 端口是否被占用
func StepH001CheckPort() *runner.Step {
	return &runner.Step{
		ID:          "H-001",
		Name:        "Check YMP Port Availability",
		Description: "Verify YMP port is not in use before installation",
		Tags:        []string{"ymp", "precheck", "network"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// 检查 ss 或 netstat 命令是否可用
			result, _ := ctx.Execute("which ss 2>/dev/null || which netstat 2>/dev/null", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("neither ss nor netstat command found")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			port := ctx.GetParamInt("ymp_port", 8090)

			// 根据 YMP 端口计算所有相关端口
			ports := []struct {
				name string
				port int
			}{
				{"YMP Web Service", port},
				{"Embedded Database", port + 1},
				{"yasom", port + 3},
				{"yasagent", port + 4},
			}

			ctx.Logger.Info("Checking if all YMP ports are available (base port: %d)...", port)

			// 检查每个端口是否被占用
			for _, p := range ports {
				ctx.Logger.Info("Checking port %d (%s)...", p.port, p.name)

				// 使用 ss 检查端口占用
				// 使用精确匹配避免误匹配（如 8090 不会匹配到 80900）
				cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", p.port, p.port)
				result, _ := ctx.Execute(cmd, false)

				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					portInfo := strings.TrimSpace(result.GetStdout())
					return fmt.Errorf("port %d (%s) is already in use; port info: %s; please choose another base port (--ymp-port) or stop the process using it", p.port, p.name, portInfo)
				}

				ctx.Logger.Info("✓ Port %d (%s) is available", p.port, p.name)
			}

			ctx.Logger.Info("✓ All YMP ports are available: Web=%d, DB=%d, yasom=%d, yasagent=%d", port, port+1, port+3, port+4)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			port := ctx.GetParamInt("ymp_port", 8090)
			ctx.Logger.Info("✓ All YMP ports availability verified: Web=%d, DB=%d, yasom=%d, yasagent=%d", port, port+1, port+3, port+4)
			return nil
		},
	}
}
