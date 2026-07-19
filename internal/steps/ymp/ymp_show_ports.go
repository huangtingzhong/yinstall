// h014_show_ports.go - 验证 YMP Web 可访问与健康摘要
// H-014: HTTP 健康探测与安装后健康摘要

package ymp

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

const (
	ympHealthInitialWait   = 5 * time.Second
	ympHealthRetryAttempts = 3
	ympHealthRetryInterval = 5 * time.Second

	resultKeyYMPProcessOK    = "ymp_health_process_ok"
	resultKeyYMPProcessCount = "ymp_health_process_count"
	resultKeyYMPMainPortOK   = "ymp_health_main_port_ok"
	resultKeyYMPDBMode       = "ymp_health_db_mode"
	resultKeyYMPExtraPorts   = "ymp_health_extra_ports"
	resultKeyYMPHTTPOK       = "ymp_health_http_ok"
	resultKeyYMPHTTPCode     = "ymp_health_http_code"
	resultKeyYMPHTTPSkipped  = "ymp_health_http_skipped"
)

type ympExtraPortStatus struct {
	Name      string
	Port      int
	Listening bool
	Detail    string
}

type ympHealthSnapshot struct {
	AccessURL    string
	ManageScript string
	ProcessOK    bool
	ProcessCount int
	MainPortOK   bool
	HTTPOK       bool
	HTTPCode     string
	HTTPSkipped  bool
	DBMode       string
	ExtraPorts   []ympExtraPortStatus
}

func ympInstallDirPattern(installDir string) string {
	p := strings.TrimRight(strings.TrimSpace(installDir), "/")
	if p == "" {
		p = "/opt/ymp"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func ympListenPortDefs(ctx *runner.StepContext) []struct {
	name string
	port int
} {
	web := ctx.GetParamInt("ymp_port", 8090)
	return []struct {
		name string
		port int
	}{
		{"YMP Web", web},
		{"Embedded DB", ctx.GetParamInt("ymp_db_port", web+1)},
		{"yasom", ctx.GetParamInt("ymp_yasom_port", web+3)},
		{"yasagent", ctx.GetParamInt("ymp_yasagent_port", web+4)},
	}
}

func isYMPPortListening(ctx *runner.StepContext, port int) (bool, string) {
	cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", port, port)
	result, _ := ctx.Execute(cmd, false)
	if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
		return true, strings.TrimSpace(result.GetStdout())
	}
	return false, ""
}

func collectYMPExtraPortStatus(ctx *runner.StepContext) []ympExtraPortStatus {
	defs := ympListenPortDefs(ctx)
	var out []ympExtraPortStatus
	for i, d := range defs {
		if i == 0 {
			continue
		}
		ok, detail := isYMPPortListening(ctx, d.port)
		out = append(out, ympExtraPortStatus{
			Name:      d.name,
			Port:      d.port,
			Listening: ok,
			Detail:    detail,
		})
	}
	return out
}

func countYMPProcesses(ctx *runner.StepContext, pattern string) (int, []string) {
	cmd := fmt.Sprintf("ps -ef | grep '%s' | grep -v grep | grep -v yinstall", pattern)
	result, _ := ctx.Execute(cmd, false)
	if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
		return 0, nil
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return len(lines), lines
}

func ympCommandAvailable(ctx *runner.StepContext, name string) bool {
	r, _ := ctx.Execute(fmt.Sprintf("command -v %s >/dev/null 2>&1", name), false)
	return r != nil && r.GetExitCode() == 0
}

func probeYMPHTTP(ctx *runner.StepContext, port int) (code string, ok bool) {
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --connect-timeout 10 --max-time 30 %s", url)
	result, err := ctx.Execute(cmd, false)
	if err != nil || result == nil {
		return "000", false
	}
	code = strings.TrimSpace(result.GetStdout())
	return code, ympHTTPOK(code)
}

func ympHTTPOK(code string) bool {
	switch code {
	case "200", "301", "302", "303", "401", "403":
		return true
	default:
		return false
	}
}

func buildYMPHealthSnapshot(ctx *runner.StepContext) ympHealthSnapshot {
	ympPort := ctx.GetParamInt("ymp_port", 8090)
	installDir := strings.TrimRight(ctx.GetParamString("ymp_install_dir", "/opt/ymp"), "/")
	host := strings.TrimSpace(ctx.Executor.Host())
	if host == "" {
		host = "localhost"
	}

	snap := ympHealthSnapshot{
		AccessURL:    fmt.Sprintf("http://%s:%d", host, ympPort),
		ManageScript: installDir + "/yashan-migrate-platform/bin/ymp.sh start|stop",
		DBMode:       strings.TrimSpace(ctx.GetParamString("ymp_db_mode", "yashandb")),
	}

	if v, ok := ctx.Results[resultKeyYMPProcessOK].(bool); ok {
		snap.ProcessOK = v
	}
	if v, ok := ctx.Results[resultKeyYMPProcessCount].(int); ok {
		snap.ProcessCount = v
	}
	if v, ok := ctx.Results[resultKeyYMPMainPortOK].(bool); ok {
		snap.MainPortOK = v
	}
	if v, ok := ctx.Results[resultKeyYMPDBMode].(string); ok && v != "" {
		snap.DBMode = v
	}
	if v, ok := ctx.Results[resultKeyYMPExtraPorts].([]ympExtraPortStatus); ok {
		snap.ExtraPorts = v
	}
	if v, ok := ctx.Results[resultKeyYMPHTTPOK].(bool); ok {
		snap.HTTPOK = v
	}
	if v, ok := ctx.Results[resultKeyYMPHTTPCode].(string); ok {
		snap.HTTPCode = v
	}
	if v, ok := ctx.Results[resultKeyYMPHTTPSkipped].(bool); ok {
		snap.HTTPSkipped = v
	}
	return snap
}

func logYMPHealthSummary(ctx *runner.StepContext, snap ympHealthSnapshot) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	okLabel := func(b bool) string {
		if b {
			return "OK"
		}
		return "FAIL"
	}
	notice := func(msg string) {
		ctx.Logger.ConsoleNotice(ctx.CurrentStepID, msg)
	}
	notice("========== YMP Health Summary ==========")
	notice(fmt.Sprintf("  URL:        %s", snap.AccessURL))
	notice("  Login:      admin / admin (change on first login)")
	notice(fmt.Sprintf("  Manage:     %s", snap.ManageScript))
	notice(fmt.Sprintf("  Processes:  %s (%d)", okLabel(snap.ProcessOK), snap.ProcessCount))
	notice(fmt.Sprintf("  Main port:  %s", okLabel(snap.MainPortOK)))
	if snap.HTTPSkipped {
		notice("  Web HTTP:   skipped (curl not found)")
	} else {
		notice(fmt.Sprintf("  Web HTTP:   %s (HTTP %s)", okLabel(snap.HTTPOK), snap.HTTPCode))
	}
	if snap.DBMode != "" {
		notice(fmt.Sprintf("  DB mode:    %s", snap.DBMode))
	}
	for _, p := range snap.ExtraPorts {
		st := "NOT LISTEN"
		if p.Listening {
			st = "LISTEN"
		}
		notice(fmt.Sprintf("  Extra %s (%d): %s", p.Name, p.Port, st))
	}
	notice("========================================")
}

// stepShowPorts 验证 YMP Web 可访问并输出健康摘要
func stepShowPorts() *runner.Step {
	return &runner.Step{
		Name:        "Verify YMP Web Access",
		Description: "HTTP health probe and post-install health summary",
		Tags:        []string{"ymp", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Verify YMP Web Access",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.YMP.VERIFY.APPLY_ONLY",
				Message:     "This step verifies HTTP access after apply; in --precheck it does not require the web interface to be up.",
				Remediation: "Run after installation completes (or run without --precheck) to perform the real verification.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympLogPhase(ctx, "plan", "H-014: Verify YMP Web Access")
			ympPort := ctx.GetParamInt("ymp_port", 8090)
			host := strings.TrimSpace(ctx.Executor.Host())
			if host == "" {
				host = "localhost"
			}
			accessURL := fmt.Sprintf("http://%s:%d", host, ympPort)

			curlOK := ympCommandAvailable(ctx, "curl")
			var httpOK bool
			var httpCode string
			var actionErr error

			if !curlOK {
				ctx.SetResult(resultKeyYMPHTTPSkipped, true)
				ctx.Logger.Warn("curl not found: skipping HTTP probe (warn only, non-blocking); install curl or verify Web manually")
			} else {
				ctx.Logger.Info("Performing HTTP health probe: http://127.0.0.1:%d", ympPort)
				for attempt := 1; attempt <= ympHealthRetryAttempts; attempt++ {
					if attempt > 1 {
						ctx.Logger.Info("HTTP probe retry %d/%d (waiting %ds)...", attempt, ympHealthRetryAttempts, int(ympHealthRetryInterval.Seconds()))
						time.Sleep(ympHealthRetryInterval)
					}
					httpCode, httpOK = probeYMPHTTP(ctx, ympPort)
					if httpOK {
						break
					}
					ctx.Logger.Warn("attempt %d/%d: HTTP probe failed (code=%s)", attempt, ympHealthRetryAttempts, httpCode)
				}
				ctx.SetResult(resultKeyYMPHTTPOK, httpOK)
				ctx.SetResult(resultKeyYMPHTTPCode, httpCode)
				ctx.SetResult(resultKeyYMPHTTPSkipped, false)

				if httpOK {
					ctx.Logger.Info("OK: YMP web interface is accessible (HTTP %s)", httpCode)
				} else {
					actionErr = fmt.Errorf("HTTP probe failed (code=%s); YMP may still be starting — try %s", httpCode, accessURL)
				}
			}

			ctx.Logger.Info("YMP access URL: %s", accessURL)
			ctx.Logger.Info("Default credentials: admin / admin (change on first login)")
			ctx.Logger.Info("YMP service management: ymp.sh start/stop under install dir")

			if !ctx.DryRun && !ctx.Precheck {
				logYMPHealthSummary(ctx, buildYMPHealthSnapshot(ctx))
			}
			return actionErr
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
