package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// HAHostKey returns stable host key for cert/file naming (prefer IP).
func HAHostKey(host string) string {
	return strings.TrimSpace(host)
}

// HAPartnerHost returns the partner SQL host for the current executor.
func HAPartnerHost(ctx *runner.StepContext) string {
	self := ""
	if ctx != nil && ctx.Executor != nil {
		self = strings.TrimSpace(ctx.Executor.Host())
	}
	primary := ResolvePrimaryHost(ctx)
	if strings.EqualFold(self, primary) || self == "" {
		replicas := ReplicaHosts(ctx)
		if len(replicas) > 0 {
			return replicas[0]
		}
	}
	return primary
}

// HAPartnerAddress returns TCP partner address host:port.
func HAPartnerAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	return fmt.Sprintf("TCP://%s:%d", host, port)
}

// HAEndpointPort returns global HADR/mirror endpoint port from CLI.
func HAEndpointPort(ctx *runner.StepContext) int {
	if ctx != nil {
		if p := ctx.GetParamInt("mssql_ha_endpoint_port", 0); p > 0 {
			return p
		}
	}
	return 5022
}

// HAEndpointPortForHost returns HA/mirror endpoint port for a peer host (split primary/replica params aware).
func HAEndpointPortForHost(ctx *runner.StepContext, host string) int {
	if ctx != nil {
		host = strings.TrimSpace(host)
		primary := ResolvePrimaryHost(ctx)
		if strings.EqualFold(host, primary) {
			if p := ctx.GetParamInt("mssql_primary_ha_endpoint_port", 0); p > 0 {
				return p
			}
		} else if host != "" {
			if p := ctx.GetParamInt("mssql_replica_ha_endpoint_port", 0); p > 0 {
				return p
			}
		}
	}
	return HAEndpointPort(ctx)
}

// LocalHAEndpointPort returns HA/mirror endpoint port for the current executor host.
func LocalHAEndpointPort(ctx *runner.StepContext) int {
	if ctx != nil && ctx.Executor != nil {
		return HAEndpointPortForHost(ctx, ctx.Executor.Host())
	}
	return HAEndpointPort(ctx)
}

// HAAdminUser returns SSH/admin user for Admin$ access to partnerHost.
func HAAdminUser(ctx *runner.StepContext, partnerHost string) string {
	if ctx == nil {
		return "Administrator"
	}
	primary := ResolvePrimaryHost(ctx)
	if strings.EqualFold(partnerHost, primary) {
		if u := strings.TrimSpace(ctx.GetParamString("primary_ssh_user", "")); u != "" {
			return u
		}
	}
	if u := strings.TrimSpace(ctx.GetParamString("replica_ssh_user", "")); u != "" {
		return u
	}
	if u := strings.TrimSpace(ctx.GetParamString("primary_ssh_user", "")); u != "" {
		return u
	}
	return "Administrator"
}

// HAAdminPassword returns SSH/admin password for Admin$ access to partnerHost.
func HAAdminPassword(ctx *runner.StepContext, partnerHost string) string {
	if ctx == nil {
		return ""
	}
	primary := ResolvePrimaryHost(ctx)
	if strings.EqualFold(partnerHost, primary) {
		if p := ctx.GetParamString("primary_ssh_password", ""); p != "" {
			return p
		}
	}
	if p := ctx.GetParamString("replica_ssh_password", ""); p != "" {
		return p
	}
	return ctx.GetParamString("primary_ssh_password", "")
}
