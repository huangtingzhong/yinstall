// g009_verify_ports.go - 验证 YCM 端口监听
// G-009: 检查 YCM 端口是否处于 LISTEN 状态

package ycm

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const (
	ycmHealthInitialWait   = 5 * time.Second
	ycmHealthRetryAttempts = 3
	ycmHealthRetryInterval = 5 * time.Second

	resultKeyYCMProcessOK      = "ycm_health_process_ok"
	resultKeyYCMProcessCount   = "ycm_health_process_count"
	resultKeyYCMMainPortOK     = "ycm_health_main_port_ok"
	resultKeyYCMBackend        = "ycm_health_backend"
	resultKeyYCMBackendOK      = "ycm_health_backend_ok"
	resultKeyYCMBackendDetail  = "ycm_health_backend_detail"
	resultKeyYCMExtraPorts     = "ycm_health_extra_ports"
	resultKeyYCMSystemdActive  = "ycm_health_systemd_active"
	resultKeyYCMSystemdEnabled = "ycm_health_systemd_enabled"
	resultKeyYCMHTTPOK         = "ycm_health_http_ok"
	resultKeyYCMHTTPCode       = "ycm_health_http_code"
	resultKeyYCMHTTPSkipped    = "ycm_health_http_skipped"
)

var (
	jdbcYashanURLPattern = regexp.MustCompile(`(?i)^jdbc:yasdb://([^:/]+):(\d+)`)
	ycmTCPHostPattern    = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	ycmTCPPortPattern    = regexp.MustCompile(`^\d+$`)
)

type ycmExtraPortStatus struct {
	Name      string
	Port      int
	Listening bool
	Detail    string
}

type ycmHealthSnapshot struct {
	ProcessOK      bool
	ProcessCount   int
	MainPortOK     bool
	HTTPOK         bool
	HTTPCode       string
	HTTPSkipped    bool
	Backend        string
	BackendDetail  string
	BackendOK      bool
	ExtraPorts     []ycmExtraPortStatus
	SystemdActive  string
	SystemdEnabled string
	AccessURL      string
	ManageScript   string
}

// stepVerifyPorts 验证 YCM 端口监听
func stepVerifyPorts() *runner.Step {
	return &runner.Step{
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
			ycmLogPhase(ctx, "plan", "G-009: Verify YCM Port Listening")
			ycmPort := ctx.GetParamInt("ycm_port", 9060)
			backend := strings.TrimSpace(ctx.GetParamString("ycm_db_driver", "sqlite3"))
			if backend == "" {
				backend = "sqlite3"
			}
			ctx.SetResult(resultKeyYCMBackend, backend)

			ctx.Logger.Info("Checking if YCM is listening on port %d...", ycmPort)

			mainPortOK := false
			var mainPortDetail string
			for attempt := 1; attempt <= ycmHealthRetryAttempts; attempt++ {
				if attempt > 1 {
					ctx.Logger.Info("Port check retry %d/%d (waiting %ds)...", attempt, ycmHealthRetryAttempts, int(ycmHealthRetryInterval.Seconds()))
					time.Sleep(ycmHealthRetryInterval)
				}
				mainPortOK, mainPortDetail = isPortListening(ctx, ycmPort)
				if mainPortOK {
					break
				}
				ctx.Logger.Warn("attempt %d/%d: YCM is not listening on port %d", attempt, ycmHealthRetryAttempts, ycmPort)
			}

			ctx.SetResult(resultKeyYCMMainPortOK, mainPortOK)
			if mainPortOK {
				ctx.Logger.Info("OK: YCM is listening on port %d", ycmPort)
				if mainPortDetail != "" {
					ctx.Logger.Info("  %s", mainPortDetail)
				}
			}

			extraPorts := collectYCMExtraPortStatus(ctx)
			ctx.SetResult(resultKeyYCMExtraPorts, extraPorts)
			for _, p := range extraPorts {
				if p.Listening {
					ctx.Logger.Info("  extra port OK: %s %d — %s", p.Name, p.Port, p.Detail)
				} else {
					ctx.Logger.Warn("  extra port not listening: %s %d (informational, non-blocking)", p.Name, p.Port)
				}
			}

			backendOK, backendDetail := checkYCMBackend(ctx, backend)
			ctx.SetResult(resultKeyYCMBackendOK, backendOK)
			ctx.SetResult(resultKeyYCMBackendDetail, backendDetail)
			ctx.Logger.Info("backend (%s): %s", backend, backendDetail)

			if ctx.GetParamBool("ycm_autostart", true) && commonos.CheckSystemdAvailable(ctx) {
				svc := ServiceNameFromContext(ctx)
				active := systemdProp(ctx, svc, "is-active")
				enabled := systemdProp(ctx, svc, "is-enabled")
				ctx.SetResult(resultKeyYCMSystemdActive, active)
				ctx.SetResult(resultKeyYCMSystemdEnabled, enabled)
				ctx.Logger.Info("systemd %s: active=%s enabled=%s", svc, active, enabled)
			}

			var blockers []string
			if !mainPortOK {
				blockers = append(blockers, fmt.Sprintf("main Web port %d not listening", ycmPort))
			}
			if strings.EqualFold(backend, "yashandb") && !backendOK {
				blockers = append(blockers, backendDetail)
			}
			if len(blockers) > 0 {
				return fmt.Errorf("YCM port/backend check failed: %s", strings.Join(blockers, "; "))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

func ycmDirPattern(installDir string) string {
	p := strings.TrimRight(strings.TrimSpace(installDir), "/") + "/ycm"
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func countYCMProcesses(ctx *runner.StepContext, pattern string) (int, []string, error) {
	cmd := fmt.Sprintf("ps -ef | grep '%s' | grep -v grep", pattern)
	result, _ := ctx.Execute(cmd, false)
	if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
		return 0, nil, nil
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return len(lines), lines, nil
}

func isPortListening(ctx *runner.StepContext, port int) (bool, string) {
	cmd := fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", port, port)
	result, _ := ctx.Execute(cmd, false)
	if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
		return true, strings.TrimSpace(result.GetStdout())
	}
	return false, ""
}

func ycmDeployPortDisplayName(yamlKey string) string {
	switch yamlKey {
	case "prometheus_port":
		return "Prometheus"
	case "loki_http_port":
		return "Loki HTTP"
	case "loki_grpc_port":
		return "Loki gRPC"
	case "yasdb_exporter_port":
		return "YasDB Exporter"
	case "agent_port":
		return "Agent"
	case "export_port":
		return "Export"
	default:
		return yamlKey
	}
}

func collectYCMExtraPortStatus(ctx *runner.StepContext) []ycmExtraPortStatus {
	var out []ycmExtraPortStatus
	for _, f := range ycmDeployPortFields() {
		if f.yamlKey == "ycm_port" {
			continue
		}
		port := ycmDeployPortValue(ctx, f)
		ok, detail := isPortListening(ctx, port)
		out = append(out, ycmExtraPortStatus{
			Name:      ycmDeployPortDisplayName(f.yamlKey),
			Port:      port,
			Listening: ok,
			Detail:    detail,
		})
	}
	webPort := ctx.GetParamInt("ycm_port", 9060)
	aioPort := webPort + ycmYasAioAPIPortOffset
	ok, detail := isPortListening(ctx, aioPort)
	out = append(out, ycmExtraPortStatus{
		Name:      "Yas AIO API",
		Port:      aioPort,
		Listening: ok,
		Detail:    detail,
	})
	return out
}

func checkYCMBackend(ctx *runner.StepContext, driver string) (ok bool, detail string) {
	driver = strings.TrimSpace(driver)
	if driver == "" {
		driver = "sqlite3"
	}
	switch strings.ToLower(driver) {
	case "yashandb":
		url := strings.TrimSpace(ctx.GetParamString("ycm_db_url", ""))
		if url == "" {
			return false, "yashandb driver but ycm_db_url is empty"
		}
		host, port, parsed := parseJDBCYashanURL(url)
		if !parsed {
			ctx.Logger.Warn("cannot parse jdbc:yasdb:// URL; skipping TCP check")
			return true, "URL is not jdbc:yasdb:// format; TCP check skipped"
		}
		if tcpReachable(ctx, host, port) {
			return true, fmt.Sprintf("external DB %s:%s reachable", host, port)
		}
		return false, fmt.Sprintf("external DB %s:%s unreachable", host, port)
	default:
		path := findSQLiteMetaPath(ctx)
		if path == "" {
			return true, "sqlite3 (meta-db path not resolved from deploy.yml; file check skipped)"
		}
		result, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(path)), false)
		if result != nil && result.GetExitCode() == 0 {
			return true, fmt.Sprintf("meta-db file exists: %s", path)
		}
		ctx.Logger.Warn("sqlite3 meta-db file not found: %s", path)
		return true, fmt.Sprintf("meta-db file not found: %s (warn only, non-blocking)", path)
	}
}

func parseJDBCYashanURL(raw string) (host, port string, ok bool) {
	m := jdbcYashanURLPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) != 3 {
		return "", "", false
	}
	return m[1], m[2], true
}

func tcpReachable(ctx *runner.StepContext, host, port string) bool {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if !ycmTCPHostPattern.MatchString(host) || !ycmTCPPortPattern.MatchString(port) {
		return false
	}
	cmd := fmt.Sprintf(`timeout 3 bash -c 'echo > /dev/tcp/%s/%s' 2>/dev/null`, host, port)
	result, _ := ctx.Execute(cmd, false)
	return result != nil && result.GetExitCode() == 0
}

func findSQLiteMetaPath(ctx *runner.StepContext) string {
	deployFile := ctx.GetParamString("ycm_deploy_file", "")
	if deployFile == "" {
		installDir := ctx.GetParamString("ycm_install_dir", "/opt")
		deployFile = installDir + "/ycm/etc/deploy.yml"
	}
	deployQ := commonos.ShellSingleQuote(deployFile)
	cmd := fmt.Sprintf(`grep -E '(^|[[:space:]])(url|path|dsn):' %s 2>/dev/null | head -5`, deployQ)
	result, _ := ctx.Execute(cmd, false)
	if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
		return ""
	}
	for _, line := range strings.Split(result.GetStdout(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, ":"); idx >= 0 {
			val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
			val = strings.TrimPrefix(val, "file:")
			if strings.Contains(val, ".db") || strings.Contains(val, ".sqlite") || strings.HasPrefix(val, "/") {
				return val
			}
		}
	}
	return ""
}

func systemdProp(ctx *runner.StepContext, service, prop string) string {
	r, _ := ctx.Execute(fmt.Sprintf("systemctl %s %s 2>/dev/null", prop, service), false)
	if r == nil {
		return "unknown"
	}
	s := strings.TrimSpace(r.GetStdout())
	if s == "" {
		return "unknown"
	}
	return s
}

func commandAvailable(ctx *runner.StepContext, name string) bool {
	r, _ := ctx.Execute(fmt.Sprintf("command -v %s >/dev/null 2>&1", name), false)
	return r != nil && r.GetExitCode() == 0
}

func probeYCMHTTP(ctx *runner.StepContext, port int) (code string, ok bool) {
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --connect-timeout 10 --max-time 30 %s", url)
	result, err := ctx.Execute(cmd, false)
	if err != nil || result == nil {
		return "000", false
	}
	code = strings.TrimSpace(result.GetStdout())
	return code, ycmHTTPOK(code)
}

func ycmHTTPOK(code string) bool {
	switch code {
	case "200", "301", "302", "303", "401", "403":
		return true
	default:
		return false
	}
}

func buildYCMHealthSnapshot(ctx *runner.StepContext) ycmHealthSnapshot {
	ycmPort := ctx.GetParamInt("ycm_port", 9060)
	host := strings.TrimSpace(ctx.Executor.Host())
	if host == "" {
		host = "localhost"
	}

	snap := ycmHealthSnapshot{
		AccessURL:    fmt.Sprintf("http://%s:%d", host, ycmPort),
		ManageScript: ycmManageCommand(ctx),
	}

	if v, ok := ctx.Results[resultKeyYCMProcessOK].(bool); ok {
		snap.ProcessOK = v
	}
	if v, ok := ctx.Results[resultKeyYCMProcessCount].(int); ok {
		snap.ProcessCount = v
	}
	if v, ok := ctx.Results[resultKeyYCMMainPortOK].(bool); ok {
		snap.MainPortOK = v
	}
	if v, ok := ctx.Results[resultKeyYCMBackend].(string); ok {
		snap.Backend = v
	}
	if v, ok := ctx.Results[resultKeyYCMBackendOK].(bool); ok {
		snap.BackendOK = v
	}
	if v, ok := ctx.Results[resultKeyYCMBackendDetail].(string); ok {
		snap.BackendDetail = v
	}
	if v, ok := ctx.Results[resultKeyYCMExtraPorts].([]ycmExtraPortStatus); ok {
		snap.ExtraPorts = v
	}
	if v, ok := ctx.Results[resultKeyYCMSystemdActive].(string); ok {
		snap.SystemdActive = v
	}
	if v, ok := ctx.Results[resultKeyYCMSystemdEnabled].(string); ok {
		snap.SystemdEnabled = v
	}
	if v, ok := ctx.Results[resultKeyYCMHTTPOK].(bool); ok {
		snap.HTTPOK = v
	}
	if v, ok := ctx.Results[resultKeyYCMHTTPCode].(string); ok {
		snap.HTTPCode = v
	}
	if v, ok := ctx.Results[resultKeyYCMHTTPSkipped].(bool); ok {
		snap.HTTPSkipped = v
	}
	return snap
}

func ycmManageCommand(ctx *runner.StepContext) string {
	installDir := ctx.GetParamString("ycm_install_dir", "/opt")
	return fmt.Sprintf("%s ycm start|stop", yasadmPath(installDir))
}

func logYCMHealthSummary(ctx *runner.StepContext, snap ycmHealthSnapshot) {
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
	notice("========== YCM Health Summary ==========")
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
	if snap.Backend != "" {
		notice(fmt.Sprintf("  Backend(%s): %s", snap.Backend, snap.BackendDetail))
	}
	for _, p := range snap.ExtraPorts {
		st := "NOT LISTEN"
		if p.Listening {
			st = "LISTEN"
		}
		notice(fmt.Sprintf("  Extra %s (%d): %s", p.Name, p.Port, st))
	}
	if snap.SystemdActive != "" {
		notice(fmt.Sprintf("  systemd:    active=%s enabled=%s", snap.SystemdActive, snap.SystemdEnabled))
	}
	notice("========================================")
}
