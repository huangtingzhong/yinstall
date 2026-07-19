package mysql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// TargetHost returns executor host or localhost.
func TargetHost(ctx *runner.StepContext) string {
	if ctx != nil && ctx.Executor != nil {
		if h := strings.TrimSpace(ctx.Executor.Host()); h != "" {
			return h
		}
	}
	return "localhost"
}

// RemoteMySQLAddress formats host:port for mysql client.
func RemoteMySQLAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}
	if port <= 0 {
		port = 3306
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// DisplayRootPassword returns root password for terminal summary.
func DisplayRootPassword(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	pwd := strings.TrimSpace(ctx.GetParamString("mysql_root_password", ""))
	if pwd == "" {
		return "(not configured)"
	}
	return pwd
}

// DisplayReplicationPassword returns replication password for terminal summary.
func DisplayReplicationPassword(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	pwd := strings.TrimSpace(ctx.GetParamString("rep_password", ""))
	if pwd == "" {
		return "(not configured)"
	}
	return pwd
}

// ParseMysqlVersionOutput extracts version string from mysql -e "SELECT VERSION()" output.
func ParseMysqlVersionOutput(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if upper == "VERSION()" || strings.HasPrefix(upper, "VERSION") {
			continue
		}
		return line
	}
	return strings.TrimSpace(out)
}

// ServiceNameForPort returns Windows service or systemd unit name for a port.
func ServiceNameForPort(platform string, port int) string {
	if platform == PlatformWindows {
		return fmt.Sprintf("MySQL%d", port)
	}
	return fmt.Sprintf("mysqld%d.service", port)
}
