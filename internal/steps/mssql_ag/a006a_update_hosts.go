package mssql_ag

import (
	"fmt"
	"net"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// StepA006aUpdateHostsFile ensures every host in the topology can resolve
// every other host's Windows computer name to its IP via the local hosts file
// (C:\Windows\System32\drivers\etc\hosts).
//
// This is required in workgroup environments where no DNS/NetBIOS resolves
// @@SERVERNAME (the computer name used as replica_server_name). Missing
// entries cause HADR endpoint connections to fail with "target principal name
// is incorrect" or "no such host is known".
//
// The step reads replica server names collected by A-005 from shared results
// and writes one line per remote host:
//
//	<IP>  <ComputerName>
//
// Existing entries for the same IP or name are replaced; all other lines are
// preserved.
func StepA006aUpdateHostsFile() *runner.Step {
	return &runner.Step{
		ID:          "A-006a",
		Name:        "Update Hosts File",
		Description: "Add IP→hostname entries to Windows hosts file for all AG replicas",
		Tags:        []string{"mssql-ha", "ag", "network"},
		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			self := ctx.Executor.Host()
			hostsFile := `C:\Windows\System32\drivers\etc\hosts`
			allEntries := collectHostsEntries(ctx, self)

			if len(allEntries) == 0 {
				ctx.Logger.Info("A-006a: no remote hosts to add (single node?)")
				return nil
			}

			ctx.Logger.Info("A-006a: updating hosts file on %s with %d entries", self, len(allEntries))
			return updateHostsFile(ctx, hostsFile, allEntries)
		},
	}
}

// hostsEntry is one `<IP> <Name>` line.
type hostsEntry struct {
	IP   string
	Name string
}

// collectHostsEntries reads all hosts from the topology params and their
// server names from A-005 shared results. Returns entries for hosts other
// than self.
func collectHostsEntries(ctx *runner.StepContext, self string) []hostsEntry {
	allHosts := commonmssql.HATopologyHosts(ctx)

	var entries []hostsEntry
	for _, ip := range allHosts {
		ip = strings.TrimSpace(ip)
		if ip == "" || strings.EqualFold(ip, self) {
			continue
		}
		name := resolveHostNameForHostsFile(ctx, ip)
		if name == "" {
			ctx.Logger.Warn("A-006a: cannot resolve server name for %s, skip hosts entry", ip)
			continue
		}
		entries = append(entries, hostsEntry{IP: ip, Name: name})
	}
	return entries
}

// resolveHostNameForHostsFile returns the Windows computer name for the given
// host IP. Prefers the A-005 cached replica server name; falls back to
// reverse DNS.
func resolveHostNameForHostsFile(ctx *runner.StepContext, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	// A-005 stored @@SERVERNAME under ha_replica_server_<ip>.
	if name, ok := commonmssql.HAReplicaServerNameFromResults(ctx.Results, ip); ok && name != "" {
		return name
	}
	// Fallback: reverse DNS (never use local $env:COMPUTERNAME for a remote IP).
	ps := fmt.Sprintf(`$h=(Resolve-DnsName %s -ErrorAction SilentlyContinue | Select-Object -First 1).NameHost; if ($h) { Write-Output $h }`, commonmssql.PSSingleQuote(ip))
	out, err := commonmssql.RunHAPowerShellScalar(ctx, "A-006a resolve "+ip, ps)
	if err != nil || strings.TrimSpace(out) == "" {
		if net.ParseIP(ip) != nil {
			return ""
		}
		return shortHostForHosts(ip)
	}
	return shortHostForHosts(strings.TrimSpace(out))
}

func shortHostForHosts(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx]
	}
	return name
}

// updateHostsFile adds or replaces entries in the Windows hosts file.
// Existing lines for the same IP or same name are removed before appending.
// psSafe wraps a string for embedding inside a PS single-quoted literal:
// ' becomes ”. Used here because PSSingleQuote would double-wrap with the
// outer -Command "..." quotes.
func psSafe(s string) string { return strings.ReplaceAll(s, "'", "''") }

func updateHostsFile(ctx *runner.StepContext, hostsFile string, entries []hostsEntry) error {
	// Build a single-line PS script. All string values are wrapped with
	// '...' (PS single quotes) so no $ expansion occurs.
	psFile := psSafe(hostsFile)
	var sb strings.Builder
	sb.WriteString(`$f='` + psFile + `'; $lines=@(); if (Test-Path -LiteralPath $f) { $lines=Get-Content -LiteralPath $f -Encoding UTF8 }; $ips=@(`)
	for i, e := range entries {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`'` + psSafe(e.IP) + `'`)
	}
	sb.WriteString(`); $names=@(`)
	for i, e := range entries {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`'` + psSafe(e.Name) + `'`)
	}
	sb.WriteString(`); $out=@(); foreach ($l in $lines) { $keep=$true; for ($i=0; $i -lt $ips.Count; $i++) { $p='^\s*'+[regex]::Escape($ips[$i])+'\s+|^\s*'+[regex]::Escape($names[$i])+'\s'; if ($l -match $p) { $keep=$false; break } }; if ($keep) { $out+=$l } }; foreach ($i in 0..($ips.Count-1)) { $out+=(($ips[$i]+'  '+$names[$i])) }; Set-Content -LiteralPath $f -Value $out -Encoding UTF8 -Force`)

	cmd := sb.String()
	ctx.LogScriptPreview("powershell", "A-006a update hosts", cmd)
	if ctx.DryRun {
		return nil
	}
	if _, err := ctx.ExecuteWithCheck(wrapPSCommand(cmd), false); err != nil {
		return fmt.Errorf("A-006a: failed to update hosts file on %s: %w", ctx.Executor.Host(), err)
	}
	return nil
}

// wrapPSCommand wraps a single-line PS command for ExecuteWithCheck.
func wrapPSCommand(cmd string) string {
	esc := strings.ReplaceAll(cmd, `"`, `\"`)
	return `powershell -NoProfile -Command "` + esc + `"`
}
