// g010_verify_web.go - 验证 YCM Web 可访问
// G-010: HTTP 健康探测与健康摘要

package ycm

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// stepVerifyWeb 验证 YCM Web 可访问
func stepVerifyWeb() *runner.Step {
	return &runner.Step{
		Name:        "Verify YCM Web Access",
		Description: "HTTP health probe and post-install health summary",
		Tags:        []string{"ycm", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Verify YCM Web Access",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.YCM.VERIFY.APPLY_ONLY",
				Message:     "This step verifies HTTP access after apply; in --precheck it does not require the web interface to be up.",
				Remediation: "Run after installation completes (or run without --precheck) to perform the real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmLogPhase(ctx, "plan", "G-010: Verify YCM Web Access")
			ycmPort := ctx.GetParamInt("ycm_port", 9060)
			host := strings.TrimSpace(ctx.Executor.Host())
			if host == "" {
				host = "localhost"
			}
			accessURL := fmt.Sprintf("http://%s:%d", host, ycmPort)

			curlOK := commandAvailable(ctx, "curl")
			var httpOK bool
			var httpCode string
			var actionErr error

			if !curlOK {
				ctx.SetResult(resultKeyYCMHTTPSkipped, true)
				ctx.Logger.Warn("curl not found: skipping HTTP probe (warn only, non-blocking); install curl or verify Web manually")
			} else {
				ctx.Logger.Info("Performing HTTP health probe: http://127.0.0.1:%d", ycmPort)
				for attempt := 1; attempt <= ycmHealthRetryAttempts; attempt++ {
					if attempt > 1 {
						ctx.Logger.Info("HTTP probe retry %d/%d (waiting %ds)...", attempt, ycmHealthRetryAttempts, int(ycmHealthRetryInterval.Seconds()))
						time.Sleep(ycmHealthRetryInterval)
					}
					httpCode, httpOK = probeYCMHTTP(ctx, ycmPort)
					if httpOK {
						break
					}
					ctx.Logger.Warn("attempt %d/%d: HTTP probe failed (code=%s)", attempt, ycmHealthRetryAttempts, httpCode)
				}
				ctx.SetResult(resultKeyYCMHTTPOK, httpOK)
				ctx.SetResult(resultKeyYCMHTTPCode, httpCode)
				ctx.SetResult(resultKeyYCMHTTPSkipped, false)

				if httpOK {
					ctx.Logger.Info("OK: YCM web interface is accessible (HTTP %s)", httpCode)
				} else {
					actionErr = fmt.Errorf("HTTP probe failed (code=%s); YCM may still be starting — try %s", httpCode, accessURL)
				}
			}

			ctx.Logger.Info("YCM access URL: %s", accessURL)
			ctx.Logger.Info("Default credentials: admin / admin (change on first login)")
			ctx.Logger.Info("YCM service management: %s", ycmManageCommand(ctx))

			if !ctx.DryRun && !ctx.Precheck {
				logYCMHealthSummary(ctx, buildYCMHealthSnapshot(ctx))
			}
			return actionErr
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
