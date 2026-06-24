package mssql

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

const agTopologyHostsParam = "mssql_ag_topology_hosts"

// MergeAGTopologyHosts merges host IPs into mssql_ag_topology_hosts params (deduped).
func MergeAGTopologyHosts(params map[string]interface{}, hosts ...string) {
	if params == nil {
		return
	}
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[strings.ToLower(h)] {
			return
		}
		seen[strings.ToLower(h)] = true
		out = append(out, h)
	}
	for _, h := range agTopologyHostsFromParams(params) {
		add(h)
	}
	for _, h := range hosts {
		add(h)
	}
	if len(out) > 0 {
		params[agTopologyHostsParam] = out
	}
}

func agTopologyHostsFromParams(params map[string]interface{}) []string {
	if params == nil {
		return nil
	}
	switch v := params[agTopologyHostsParam].(type) {
	case []string:
		return append([]string(nil), v...)
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return strings.Split(v, ",")
	default:
		return nil
	}
}

// HATopologyHosts returns primary + listed replicas + auto-discovered AG topology hosts (deduped).
func HATopologyHosts(ctx *runner.StepContext) []string {
	if ctx == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[strings.ToLower(h)] {
			return
		}
		seen[strings.ToLower(h)] = true
		out = append(out, h)
	}
	add(ResolvePrimaryHost(ctx))
	for _, h := range ReplicaHosts(ctx) {
		add(h)
	}
	for _, h := range agTopologyHostsFromParams(ctx.Params) {
		add(h)
	}
	for _, th := range ctx.HostsToRun() {
		add(th.Host)
	}
	return out
}

// InstanceNameFromReplicaServerName extracts SQL instance from @@SERVERNAME (HOST\INSTANCE).
func InstanceNameFromReplicaServerName(serverName string) string {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return DefaultInstance
	}
	if i := strings.LastIndex(serverName, `\`); i >= 0 {
		if inst := strings.TrimSpace(serverName[i+1:]); inst != "" {
			return inst
		}
	}
	return DefaultInstance
}

// StoreHAReplicaServerName caches AG replica_server_name for a topology host IP.
func StoreHAReplicaServerName(results map[string]interface{}, hostIP, replicaServerName string) {
	if results == nil {
		return
	}
	hostIP = strings.TrimSpace(hostIP)
	replicaServerName = strings.TrimSpace(replicaServerName)
	if hostIP == "" || replicaServerName == "" {
		return
	}
	results[HAReplicaServerNameResultKey(hostIP)] = replicaServerName
}
