// g009_verify_ports.go - 验证 YCM 端口监听
// G-009: 检查 YCM 端口是否处于 LISTEN 状态

package ycm

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepG009VerifyPorts 验证 YCM 端口监听
func StepG009VerifyPorts() *runner.Step {
	return &runner.Step{
		ID:          "G-009",
		Name:        "Verify YCM Port Listening",
		Description: "Verify YCM service is listening on configured ports",
		Tags:        []string{"ycm", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// Read-only capability check: ss/netstat exists.
			r, _ := ctx.Execute("which ss 2>/dev/null || which netstat 2>/dev/null", false)
			if r == nil || r.GetExitCode() != 0 {
				return fmt.Errorf("neither ss nor netstat command found")
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepID:      "G-009",
				StepName:    "Verify YCM Port Listening",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.YCM.VERIFY.APPLY_ONLY",
				Message:     "This step verifies port listening after apply; in --precheck it only checks that probing commands exist (it does not require ports to be listening).",
				Remediation: "Run after installation completes (or run without --precheck) to perform the real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmPort := ctx.GetParamInt("ycm_port", 9060)

			ctx.Logger.Info("Checking if YCM is listening on port %d...", ycmPort)

			// 检查主要 YCM 端口
			// 使用精确匹配避免误匹配（如 9060 不会匹配到 90600）
			cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", ycmPort, ycmPort)
			result, _ := ctx.Execute(cmd, false)

			if result == nil || result.GetExitCode() != 0 || strings.TrimSpace(result.GetStdout()) == "" {
				return fmt.Errorf("YCM is not listening on port %d", ycmPort)
			}

			ctx.Logger.Info("OK: YCM is listening on port %d", ycmPort)
			ctx.Logger.Info("  %s", strings.TrimSpace(result.GetStdout()))

			// 可选：检查其他端口（非阻塞）
			otherPorts := []struct {
				name     string
				paramKey string
				defVal   int
			}{
				{"Prometheus", "ycm_prometheus_port", 9061},
				{"Loki HTTP", "ycm_loki_http_port", 9062},
				{"Loki gRPC", "ycm_loki_grpc_port", 9063},
				{"YasDB Exporter", "ycm_yasdb_exporter_port", 9064},
			}

			for _, p := range otherPorts {
				portVal := ctx.GetParamInt(p.paramKey, p.defVal)
				// 使用精确匹配避免误匹配（如 9061 不会匹配到 90610）
				cmd = fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", portVal, portVal)
				result, _ = ctx.Execute(cmd, false)
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					ctx.Logger.Info("OK: %s listening on port %d", p.name, portVal)
				} else {
					ctx.Logger.Warn("  %s not yet listening on port %d (may start later)", p.name, portVal)
				}
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
