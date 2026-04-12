// h014_show_ports.go - 显示 YMP 相关端口状态
// H-014: 检查并显示 YMP 使用的所有端口是否已启动

package ymp

import (
	"fmt"
	"os"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH014ShowPorts 显示 YMP 相关端口状态
func StepH014ShowPorts() *runner.Step {
	return &runner.Step{
		ID:          "H-014",
		Name:        "Show YMP Ports Status",
		Description: "Check and display status of all YMP-related ports",
		Tags:        []string{"ymp", "verify", "ports"},
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
			ympPort := ctx.GetParamInt("ymp_port", 8090)
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			// YMP 可能使用的端口列表
			// 1. YMP Web服务端口（主端口）
			// 2. 嵌入式数据库端口（通常从1876开始，根据配置可能不同）
			ports := []struct {
				name     string
				port     int
				required bool
			}{
				{"YMP Web Service", ympPort, true},
				// 嵌入式数据库端口通常从1876开始，但需要从配置中获取
				// 这里先检查常见的数据库端口范围
			}

			ctx.Logger.Info("Checking YMP ports status...")

			var allPortsOk bool = true

			// 检查每个端口
			for _, p := range ports {
				// 使用 ss 检查端口监听
				cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", p.port, p.port)
				result, _ := ctx.Execute(cmd, false)

				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					output := strings.TrimSpace(result.GetStdout())
					// 只输出端口监听信息
					fmt.Fprintf(os.Stdout, "%s\n", output)
					ctx.Logger.Info("Port %d (%s): %s", p.port, p.name, output)
				} else {
					if p.required {
						ctx.Logger.Warn("Port %d (%s): NOT LISTENING", p.port, p.name)
						allPortsOk = false
					} else {
						ctx.Logger.Info("Port %d (%s): NOT LISTENING (optional)", p.port, p.name)
					}
				}
			}

			// 检查嵌入式数据库端口（通过查看YMP进程）
			ctx.Logger.Info("Checking embedded database ports...")
			dbPortCmd := fmt.Sprintf("ps -ef | grep -E '%s.*yasagent|%s.*yasdb' | grep -v grep | head -1", ympUser, ympUser)
			dbProcessResult, _ := ctx.Execute(dbPortCmd, false)
			if dbProcessResult != nil && dbProcessResult.GetExitCode() == 0 {
				processInfo := strings.TrimSpace(dbProcessResult.GetStdout())
				if processInfo != "" {
					ctx.Logger.Info("Database process found")
					// 检查常见的数据库端口范围（通常YashanDB端口从1876开始）
					for dbPort := 1876; dbPort <= 1880; dbPort++ {
						cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", dbPort, dbPort)
						result, _ := ctx.Execute(cmd, false)
						if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
							output := strings.TrimSpace(result.GetStdout())
							// 只输出端口监听信息
							fmt.Fprintf(os.Stdout, "%s\n", output)
							ctx.Logger.Info("Port %d (Embedded DB): %s", dbPort, output)
							break // 找到第一个数据库端口即可
						}
					}
				}
			}

			if !allPortsOk {
				return fmt.Errorf("some required ports are not listening")
			}

			ctx.Logger.Info("All required ports are listening")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("[OK] Port status check completed")
			return nil
		},
	}
}
