// g006_check_ports.go - 校验 YCM 端口未占用
// G-006: 检查 YCM 使用的 5 个端口是否可用

package ycm

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepG006CheckPorts 校验 YCM 端口未占用
func StepG006CheckPorts() *runner.Step {
	return &runner.Step{
		ID:          "G-006",
		Name:        "Check YCM Ports",
		Description: "Verify all YCM ports are free and not in use",
		Tags:        []string{"ycm", "network"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// 检查 ss 命令是否可用
			result, _ := ctx.Execute("which ss 2>/dev/null || which netstat 2>/dev/null", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("neither ss nor netstat command found")
			}
			// Also check all ports in precheck (read-only) so --precheck can surface issues.
			ports := []struct {
				name     string
				paramKey string
				defVal   int
			}{
				{"YCM Web", "ycm_port", 9060},
				{"Prometheus", "ycm_prometheus_port", 9061},
				{"Loki HTTP", "ycm_loki_http_port", 9062},
				{"Loki gRPC", "ycm_loki_grpc_port", 9063},
				{"YasDB Exporter", "ycm_yasdb_exporter_port", 9064},
			}
			var usedPorts []string
			for _, p := range ports {
				portVal := ctx.GetParamInt(p.paramKey, p.defVal)
				cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", portVal, portVal)
				r, _ := ctx.Execute(cmd, false)
				if r != nil && r.GetExitCode() == 0 && strings.TrimSpace(r.GetStdout()) != "" {
					usedPorts = append(usedPorts, fmt.Sprintf("%d(%s)", portVal, p.name))
				}
			}
			if len(usedPorts) > 0 {
				return fmt.Errorf("the following ports are already in use: %s", strings.Join(usedPorts, ", "))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ports := []struct {
				name     string
				paramKey string
				defVal   int
			}{
				{"YCM Web", "ycm_port", 9060},
				{"Prometheus", "ycm_prometheus_port", 9061},
				{"Loki HTTP", "ycm_loki_http_port", 9062},
				{"Loki gRPC", "ycm_loki_grpc_port", 9063},
				{"YasDB Exporter", "ycm_yasdb_exporter_port", 9064},
			}

			var usedPorts []string
			for _, p := range ports {
				portVal := ctx.GetParamInt(p.paramKey, p.defVal)
				ctx.Logger.Info("Checking port %d (%s)...", portVal, p.name)

				// 使用 ss 检查端口占用
				// 使用精确匹配避免误匹配（如 9060 不会匹配到 90600）
				cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", portVal, portVal)
				result, _ := ctx.Execute(cmd, false)

				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					ctx.Logger.Warn("Port %d (%s) is in use: %s", portVal, p.name, strings.TrimSpace(result.GetStdout()))
					usedPorts = append(usedPorts, fmt.Sprintf("%d(%s)", portVal, p.name))
				} else {
					ctx.Logger.Info("OK: Port %d (%s) is free", portVal, p.name)
				}
			}

			if len(usedPorts) > 0 {
				return fmt.Errorf("the following ports are already in use: %s", strings.Join(usedPorts, ", "))
			}

			ctx.Logger.Info("All YCM ports are available")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// PostCheck 与 Action 逻辑相同，此步骤为检查类步骤
			return nil
		},
	}
}
