// g010_verify_web.go - 验证 YCM Web 可访问
// G-010: 可选的 HTTP 健康探测

package ycm

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepG010VerifyWeb 验证 YCM Web 可访问
func StepG010VerifyWeb() *runner.Step {
	return &runner.Step{
		ID:          "G-010",
		Name:        "Verify YCM Web Access",
		Description: "Optional HTTP health probe to verify YCM web interface is accessible",
		Tags:        []string{"ycm", "verify"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// 检查 curl 是否可用
			result, _ := ctx.Execute("which curl 2>/dev/null", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("curl command not found, cannot perform HTTP health check")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmPort := ctx.GetParamInt("ycm_port", 9060)
			host := ctx.Executor.Host()

			// 使用 localhost 进行探测（在目标主机上执行）
			url := fmt.Sprintf("http://localhost:%d", ycmPort)
			ctx.Logger.Info("Performing HTTP health probe: %s", url)

			// curl 带超时，接受 HTTP 200/301/302/303 等正常响应
			cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --connect-timeout 10 --max-time 30 %s", url)
			result, err := ctx.Execute(cmd, false)

			if err != nil {
				ctx.Logger.Warn("HTTP probe failed: %v", err)
				ctx.Logger.Info("YCM may still be starting up. Access URL: http://%s:%d", host, ycmPort)
				return nil // 可选步骤，不阻塞
			}

			if result != nil {
				httpCode := strings.TrimSpace(result.GetStdout())
				switch {
				case httpCode == "200":
					ctx.Logger.Info("OK: YCM web interface is accessible (HTTP 200)")
				case httpCode == "301" || httpCode == "302" || httpCode == "303":
					ctx.Logger.Info("OK: YCM web interface is accessible (HTTP %s redirect)", httpCode)
				case httpCode == "000":
					ctx.Logger.Warn("YCM web interface connection refused or timeout")
					ctx.Logger.Info("YCM may still be starting up. Try: http://%s:%d", host, ycmPort)
				default:
					ctx.Logger.Info("YCM web interface responded with HTTP %s", httpCode)
				}
			}

			ctx.Logger.Info("YCM access URL: http://%s:%d", host, ycmPort)
			ctx.Logger.Info("Default credentials: admin / admin (change on first login)")

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
