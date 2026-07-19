// h013_verify_port.go - 验证 YMP 端口监听
// H-013: 检查 YMP 主端口与相关端口是否处于 LISTEN 状态

package ymp

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// stepVerifyPort 验证 YMP 端口监听
func stepVerifyPort() *runner.Step {
	return &runner.Step{
		Name:        "Verify YMP Port Listening",
		Description: "Verify YMP service is listening on configured ports",
		Tags:        []string{"ymp", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			r, _ := ctx.Execute("which ss 2>/dev/null || which netstat 2>/dev/null", false)
			if r == nil || r.GetExitCode() != 0 {
				return fmt.Errorf("neither ss nor netstat command found")
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Verify YMP Port Listening",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.YMP.VERIFY.APPLY_ONLY",
				Message:     "This step verifies port listening after apply; in --precheck it only checks that probing commands exist (it does not require ports to be listening).",
				Remediation: "Run after installation completes (or run without --precheck) to perform the real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympLogPhase(ctx, "plan", "H-013: Verify YMP Port Listening")
			ympPort := ctx.GetParamInt("ymp_port", 8090)
			dbMode := strings.TrimSpace(ctx.GetParamString("ymp_db_mode", "yashandb"))
			if dbMode == "" {
				dbMode = "yashandb"
			}
			ctx.SetResult(resultKeyYMPDBMode, dbMode)

			ctx.Logger.Info("Checking if YMP is listening on port %d...", ympPort)

			mainPortOK := false
			var mainPortDetail string
			for attempt := 1; attempt <= ympHealthRetryAttempts; attempt++ {
				if attempt > 1 {
					ctx.Logger.Info("Port check retry %d/%d (waiting %ds)...", attempt, ympHealthRetryAttempts, int(ympHealthRetryInterval.Seconds()))
					time.Sleep(ympHealthRetryInterval)
				}
				mainPortOK, mainPortDetail = isYMPPortListening(ctx, ympPort)
				if mainPortOK {
					break
				}
				ctx.Logger.Warn("attempt %d/%d: YMP is not listening on port %d", attempt, ympHealthRetryAttempts, ympPort)
			}

			ctx.SetResult(resultKeyYMPMainPortOK, mainPortOK)
			if mainPortOK {
				ctx.Logger.Info("OK: YMP is listening on port %d", ympPort)
				if mainPortDetail != "" {
					ctx.Logger.Info("  %s", mainPortDetail)
				}
			}

			extraPorts := collectYMPExtraPortStatus(ctx)
			ctx.SetResult(resultKeyYMPExtraPorts, extraPorts)
			for _, p := range extraPorts {
				if p.Listening {
					ctx.Logger.Info("  extra port OK: %s %d — %s", p.Name, p.Port, p.Detail)
				} else {
					ctx.Logger.Warn("  extra port not listening: %s %d (informational, non-blocking)", p.Name, p.Port)
				}
			}

			if !mainPortOK {
				return fmt.Errorf("YMP port check failed: main Web port %d not listening", ympPort)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
