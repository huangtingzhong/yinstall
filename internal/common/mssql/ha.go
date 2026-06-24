package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ResolvePrimaryHost returns the HA primary host (param or first target).
func ResolvePrimaryHost(ctx *runner.StepContext) string {
	if p := strings.TrimSpace(ctx.GetParamString("mssql_primary_host", "")); p != "" {
		return p
	}
	hosts := ctx.HostsToRun()
	if len(hosts) > 0 {
		return hosts[0].Host
	}
	if ctx.Executor != nil {
		return ctx.Executor.Host()
	}
	return ""
}

// IsPrimaryHost reports whether the current executor host is the HA primary.
func IsPrimaryHost(ctx *runner.StepContext) bool {
	primary := ResolvePrimaryHost(ctx)
	if primary == "" || ctx.Executor == nil {
		return true
	}
	return strings.EqualFold(primary, ctx.Executor.Host())
}

// IsHATopology reports mirror/ag HA commands where Params are shared across ForHost contexts.
func IsHATopology(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	switch Topology(strings.TrimSpace(ctx.GetParamString("mssql_topology", ""))) {
	case TopologyMirror, TopologyAGWSFC:
		return true
	default:
		return false
	}
}

// IsSecondaryHost reports whether the current host is a non-primary replica node.
func IsSecondaryHost(ctx *runner.StepContext) bool {
	primary := ResolvePrimaryHost(ctx)
	if primary == "" || ctx.Executor == nil {
		return false
	}
	return !strings.EqualFold(primary, ctx.Executor.Host())
}

// IsListedReplicaHost reports whether the current host is listed in -t (mssql_replica_hosts).
// Used when ensureAGExistingReplicas adds already-joined nodes for cert/hosts only.
func IsListedReplicaHost(ctx *runner.StepContext) bool {
	if ctx == nil || ctx.Executor == nil {
		return false
	}
	self := strings.TrimSpace(ctx.Executor.Host())
	for _, h := range ReplicaHosts(ctx) {
		if strings.EqualFold(strings.TrimSpace(h), self) {
			return true
		}
	}
	return false
}

// AGName returns availability group name from params.
func AGName(ctx *runner.StepContext) string {
	if n := strings.TrimSpace(ctx.GetParamString("mssql_ag_name", "")); n != "" {
		return n
	}
	return "AG1"
}

// AGListenerName returns listener DNS name.
func AGListenerName(ctx *runner.StepContext) string {
	if n := strings.TrimSpace(ctx.GetParamString("mssql_ag_listener", "")); n != "" {
		return n
	}
	return AGName(ctx) + "-lst"
}

// IsHadrEnabledSQL returns 1 when Always On is already enabled (SMO/SERVERPROPERTY).
func IsHadrEnabledSQL() string {
	return `SELECT CAST(SERVERPROPERTY('IsHadrEnabled') AS INT);`
}

// VerifyAvailabilityGroupSQL returns AG inventory query.
func VerifyAvailabilityGroupSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf("SELECT name, automated_backup_preference_desc FROM sys.availability_groups WHERE name = N'%s';", agName)
}
