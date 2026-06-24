package mssql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// PortAuto discovers TCP port from registry (single instance) or by instance name.
	PortAuto = "auto"
)

// IsPortAuto reports whether a port parameter value means auto-discovery.
func IsPortAuto(v interface{}) bool {
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		return s == "" || strings.EqualFold(s, PortAuto)
	case nil:
		return true
	default:
		return false
	}
}

// NormalizePortParam converts CLI --mssql-port string to int or PortAuto string for params.
func NormalizePortParam(raw string) (interface{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, PortAuto) {
		return PortAuto, nil
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 || p > 65535 {
		return nil, fmt.Errorf("invalid --mssql-port %q (use auto or 1-65535)", raw)
	}
	return p, nil
}

// PortParamInt returns numeric port from params; 0 when auto or unset.
func PortParamInt(ctx *runner.StepContext) int {
	if ctx == nil {
		return 0
	}
	v := ctx.GetParam("mssql_port")
	if IsPortAuto(v) {
		return 0
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case string:
		p, _ := strconv.Atoi(strings.TrimSpace(x))
		return p
	default:
		return 0
	}
}

// ResolvedListenPort returns TCP port after instance resolution, else explicit param, else DefaultPort.
func ResolvedListenPort(ctx *runner.StepContext) int {
	if entry, ok := RegistryEntryFromContext(ctx); ok && entry.ListenPort > 0 {
		return entry.ListenPort
	}
	if p := PortParamInt(ctx); p > 0 {
		return p
	}
	return DefaultPort
}

// SQLPortForHost returns the SQL TCP port for a peer host (split primary/replica params aware).
func SQLPortForHost(ctx *runner.StepContext, host string) int {
	if ctx != nil && ctx.Results != nil {
		if entry, ok := ctx.Results[RegistryEntryResultKey(host)].(InstanceRegistryEntry); ok && entry.ListenPort > 0 {
			return entry.ListenPort
		}
	}
	host = strings.TrimSpace(host)
	primary := ResolvePrimaryHost(ctx)
	if strings.EqualFold(host, primary) {
		if v, ok := ctx.Params["mssql_primary_port"]; ok && !IsPortAuto(v) {
			if p := portParamInt(v); p > 0 {
				return p
			}
		}
	} else {
		if v, ok := ctx.Params["mssql_replica_port"]; ok && !IsPortAuto(v) {
			if p := portParamInt(v); p > 0 {
				return p
			}
		}
	}
	if ctx.Executor != nil && strings.EqualFold(host, strings.TrimSpace(ctx.Executor.Host())) {
		return ResolvedListenPort(ctx)
	}
	return 0
}

// MergeOSFirewallSQLPorts adds split primary/replica SQL ports into os_firewall_ports (deduped).
func MergeOSFirewallSQLPorts(p map[string]interface{}) {
	if p == nil {
		return
	}
	seen := map[int]struct{}{}
	var ordered []int
	add := func(v interface{}) {
		if IsPortAuto(v) {
			return
		}
		port := 0
		switch x := v.(type) {
		case int:
			port = x
		case int64:
			port = int(x)
		case string:
			port, _ = strconv.Atoi(strings.TrimSpace(x))
		}
		if port <= 0 {
			return
		}
		if _, ok := seen[port]; ok {
			return
		}
		seen[port] = struct{}{}
		ordered = append(ordered, port)
	}
	for _, part := range strings.Split(strings.TrimSpace(fmt.Sprint(p["os_firewall_ports"])), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			add(n)
		}
	}
	add(p["mssql_primary_port"])
	add(p["mssql_replica_port"])
	if len(ordered) == 0 {
		return
	}
	parts := make([]string, len(ordered))
	for i, port := range ordered {
		parts[i] = strconv.Itoa(port)
	}
	p["os_firewall_ports"] = strings.Join(parts, ",")
}

// CLIFirewallSQLPort returns numeric port for OS firewall rules from CLI --mssql-port (auto → DefaultPort).
func CLIFirewallSQLPort(raw string) int {
	p, err := NormalizePortParam(raw)
	if err != nil || IsPortAuto(p) {
		return DefaultPort
	}
	return p.(int)
}

func FormatMultipleInstancesError(entries []InstanceRegistryEntry) string {
	var b strings.Builder
	b.WriteString("multiple SQL Server instances found in registry; specify --mssql-port, --mssql-instance, or per-host --primary-mssql-instance / --replica-mssql-instance:\n")
	for _, e := range entries {
		port := e.ListenPort
		if port <= 0 {
			port = 0
		}
		fmt.Fprintf(&b, "  instance=%s port=%d version=%s path=%s\n",
			e.Name, port, strings.TrimSpace(e.Version), strings.TrimSpace(e.SQLPath))
	}
	return strings.TrimRight(b.String(), "\n")
}
