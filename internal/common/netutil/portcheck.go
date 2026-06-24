package netutil

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	PlatformWindows = "windows"
	PlatformLinux   = "linux"
)

// TestTCPPort probes remoteHost:port from the current executor (SSH-safe on Windows).
func TestTCPPort(ctx *runner.StepContext, remoteHost string, port int) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	remoteHost = strings.TrimSpace(remoteHost)
	if remoteHost == "" {
		return fmt.Errorf("empty remote host")
	}
	if port <= 0 {
		return fmt.Errorf("invalid port %d", port)
	}

	platform := strings.ToLower(strings.TrimSpace(ctx.GetTargetPlatform()))
	label := fmt.Sprintf("TCP %s:%d", remoteHost, port)

	switch platform {
	case PlatformWindows:
		qHost := strings.ReplaceAll(remoteHost, `'`, `''`)
		script := fmt.Sprintf(
			`$ErrorActionPreference='Stop'; $c=$null; try { $c=New-Object System.Net.Sockets.TcpClient; $iar=$c.BeginConnect('%s',%d,$null,$null); if (-not $iar.AsyncWaitHandle.WaitOne(8000,$false)) { exit 1 }; $c.EndConnect($iar); 'ok' } catch { exit 1 } finally { if ($c) { $c.Close() } }`,
			qHost, port,
		)
		ctx.LogScriptPreview("powershell", label, script)
		if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false); err != nil {
			return fmt.Errorf("TCP test failed for %s:%d", remoteHost, port)
		}
		return nil
	default:
		qHost := shellSingleQuote(remoteHost)
		cmd := fmt.Sprintf(`timeout 8 bash -c 'cat </dev/null >/dev/tcp/%s/%d' 2>/dev/null && echo ok || exit 1`, qHost, port)
		ctx.LogScriptPreview("shell", label, cmd)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("TCP test failed for %s:%d", remoteHost, port)
		}
		return nil
	}
}

// EnsureInboundTCPPort opens inbound allow rule for port (Windows always; Linux when firewalld active).
func EnsureInboundTCPPort(ctx *runner.StepContext, rulePrefix string, port int) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	if port <= 0 {
		return nil
	}
	rulePrefix = strings.TrimSpace(rulePrefix)
	if rulePrefix == "" {
		rulePrefix = "yinstall"
	}
	platform := strings.ToLower(strings.TrimSpace(ctx.GetTargetPlatform()))
	portStr := strconv.Itoa(port)

	switch platform {
	case PlatformWindows:
		ruleName := fmt.Sprintf("%s-tcp-%d", rulePrefix, port)
		script := fmt.Sprintf(
			`$name='%s'; $port=%s; if (-not (Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue)) { New-NetFirewallRule -DisplayName $name -Direction Inbound -Action Allow -Protocol TCP -LocalPort $port | Out-Null; 'added' } else { 'exists' }; exit 0`,
			strings.ReplaceAll(ruleName, "'", "''"), portStr,
		)
		ctx.LogScriptPreview("powershell", "firewall inbound TCP "+portStr, script)
		res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
		if err != nil {
			return fmt.Errorf("open inbound TCP %d: %w", port, err)
		}
		if res != nil {
			out := strings.TrimSpace(res.GetStdout())
			if out == "added" {
				ctx.Logger.Info("firewall: added inbound rule %s (TCP %d)", ruleName, port)
			} else {
				ctx.Logger.Info("firewall: rule %s already present (TCP %d)", ruleName, port)
			}
		}
		return nil
	case PlatformLinux:
		check := `systemctl is-active firewalld 2>/dev/null`
		res, _ := ctx.Execute(check, false)
		if res == nil || strings.TrimSpace(res.GetStdout()) != "active" {
			ctx.Logger.Info("firewall: firewalld inactive on %s; skip open-port (TCP probe will verify)", ctx.Executor.Host())
			return nil
		}
		cmd := fmt.Sprintf("firewall-cmd --zone=public --add-port=%s/tcp --permanent 2>/dev/null && firewall-cmd --reload 2>/dev/null", portStr)
		ctx.LogScriptPreview("shell", "firewalld open TCP "+portStr, cmd)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("firewalld open TCP %d: %w", port, err)
		}
		ctx.Logger.Info("firewall: opened TCP %d via firewalld on %s", port, ctx.Executor.Host())
		return nil
	default:
		return nil
	}
}

// VerifyLocalTCPListening checks port is listening on localhost.
func VerifyLocalTCPListening(ctx *runner.StepContext, port int) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	platform := strings.ToLower(strings.TrimSpace(ctx.GetTargetPlatform()))
	switch platform {
	case PlatformWindows:
		script := fmt.Sprintf(
			`$n=(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Measure-Object).Count; if ($n -gt 0) { 'listening' } else { exit 1 }`,
			port,
		)
		ctx.LogScriptPreview("powershell", fmt.Sprintf("verify local listen :%d", port), script)
		if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false); err != nil {
			return fmt.Errorf("port %d not listening locally", port)
		}
		return nil
	default:
		cmd := fmt.Sprintf(`ss -ltn 2>/dev/null | grep -E ':%d([^0-9]|$)' >/dev/null || netstat -ltn 2>/dev/null | grep -E ':%d([^0-9]|$)' >/dev/null`, port, port)
		ctx.LogScriptPreview("shell", fmt.Sprintf("verify local listen :%d", port), cmd)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("port %d not listening locally", port)
		}
		return nil
	}
}

func shellSingleQuote(s string) string {
	return strings.ReplaceAll(s, `'`, `'\''`)
}
