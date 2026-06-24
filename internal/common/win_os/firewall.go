package win_os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// FirewallMode reads os_firewall_mode param.
func FirewallMode(ctx *runner.StepContext) string {
	return ctx.GetParamString("os_firewall_mode", "keep")
}

// ShouldConfigureFirewall returns true when W-004 should run.
func ShouldConfigureFirewall(ctx *runner.StepContext) bool {
	mode := FirewallMode(ctx)
	return mode == "open-ports" || mode == "disable" || mode == "disable-lab"
}

// OpenFirewallPorts creates Windows firewall rules for TCP ports.
func OpenFirewallPorts(ctx *runner.StepContext) error {
	mode := FirewallMode(ctx)
	if mode == "keep" {
		return fmt.Errorf("firewall mode is keep")
	}
	if mode == "disable" || mode == "disable-lab" {
		script := `Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled False`
		ctx.LogScriptPreview("powershell", "W-004 disable firewall", script)
		_, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
		return err
	}

	portsStr := ctx.GetParamString("os_firewall_ports", "")
	if portsStr == "" {
		return nil
	}
	ports := commonos.ParseFirewallPorts(portsStr)
	for _, port := range ports {
		ruleName := fmt.Sprintf("yinstall-tcp-%s", port)
		script := fmt.Sprintf(
			`if (-not (Get-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue)) { New-NetFirewallRule -DisplayName '%s' -Direction Inbound -Action Allow -Protocol TCP -LocalPort %s | Out-Null }`,
			ruleName, ruleName, port,
		)
		ctx.LogScriptPreview("powershell", "W-004 open port "+port, script)
		if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false); err != nil {
			return fmt.Errorf("open port %s: %w", port, err)
		}
	}
	return nil
}

// ListFirewallRulesSummary returns display names of yinstall rules.
func ListFirewallRulesSummary(ctx *runner.StepContext) string {
	res, _ := ctx.Execute(`powershell -NoProfile -Command "Get-NetFirewallRule -DisplayName 'yinstall-*' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty DisplayName"`, false)
	if res == nil {
		return ""
	}
	return strings.TrimSpace(res.GetStdout())
}
