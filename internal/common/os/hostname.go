package os

import (
	"fmt"
	"strings"
)

const (
	DefaultHostnamePrefixYashan = "yashandb"
	DefaultHostnamePrefixMySQL  = "mysql"
	DefaultHostnamePrefixSQL    = "sql"
)

// ParseHostnames splits comma-separated hostname values.
func ParseHostnames(hostnameParam string) []string {
	if hostnameParam == "" {
		return nil
	}
	parts := strings.Split(hostnameParam, ",")
	var hostnames []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			hostnames = append(hostnames, trimmed)
		}
	}
	return hostnames
}

// TargetHostnameFromRules computes the target hostname from prefix, node count, and explicit names.
func TargetHostnameFromRules(prefix string, hostCount, index int, explicit []string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = DefaultHostnamePrefixYashan
	}
	if hostCount > 1 {
		if len(explicit) == 0 {
			return fmt.Sprintf("%s%02d", prefix, index+1)
		}
		if len(explicit) == 1 {
			return fmt.Sprintf("%s%02d", explicit[0], index+1)
		}
		return explicit[index]
	}
	if len(explicit) == 0 {
		return prefix
	}
	return explicit[0]
}

// ShouldReplaceHostnameWhenUnset returns true when --os-hostname is unset and current name is a system default.
func ShouldReplaceHostnameWhenUnset(current string) bool {
	current = NormalizeHostname(current)
	if current == "" || current == "(none)" {
		return true
	}
	switch current {
	case "localhost", "localhost.localdomain":
		return true
	default:
		// Windows default names: WIN-*, EC2AMAZ-*, etc.
		if strings.HasPrefix(current, "win-") || strings.HasPrefix(current, "ec2amaz-") {
			return true
		}
		return false
	}
}

// NormalizeHostname lowercases and strips domain suffix for comparison.
func NormalizeHostname(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	if idx := strings.Index(h, "."); idx > 0 {
		h = h[:idx]
	}
	return h
}
