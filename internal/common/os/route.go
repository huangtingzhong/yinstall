package os

import (
	"strings"
)

// DefaultGatewayShell returns a shell snippet that prints the IPv4 default gateway, or nothing.
func DefaultGatewayShell() string {
	return `ip -4 route show default 2>/dev/null | awk '/default/ {print $3; exit}'`
}

// ParseDefaultGatewayOutput extracts a gateway IP from command stdout.
func ParseDefaultGatewayOutput(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		gw := strings.TrimSpace(line)
		if gw != "" && IsValidIPv4(gw) {
			return gw
		}
	}
	return ""
}
