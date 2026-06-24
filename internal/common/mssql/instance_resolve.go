package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ResolveMode controls instance resolution behavior.
type ResolveMode int

const (
	ResolveModeInstallNew ResolveMode = iota
	ResolveModeExisting
)

// IsInstanceAuto reports whether instance param requests auto-discovery.
func IsInstanceAuto(instance string) bool {
	instance = strings.TrimSpace(instance)
	return instance == "" || strings.EqualFold(instance, InstanceAuto)
}

// EnsureInstanceResolved resolves instance/port when not already done (Existing mode).
func EnsureInstanceResolved(ctx *runner.StepContext) (InstanceRegistryEntry, error) {
	if entry, ok := RegistryEntryFromContext(ctx); ok && !registryEntryNeedsRefresh(entry) && cachedRegistryEntryMatchesSelection(ctx, entry) {
		return entry, nil
	}
	return ResolveInstanceTarget(ctx, ResolveModeExisting)
}

// ResolvedInstanceName returns the SQL instance name for the current executor host.
func ResolvedInstanceName(ctx *runner.StepContext) string {
	if entry, ok := RegistryEntryFromContext(ctx); ok && strings.TrimSpace(entry.Name) != "" {
		return entry.Name
	}
	if ctx != nil {
		inst, _, _ := HAInstanceSelection(ctx)
		if !IsInstanceAuto(inst) {
			return inst
		}
	}
	return DefaultInstance
}

// HAInstanceSelection returns instance/port for the current host (primary vs replica params).
func HAInstanceSelection(ctx *runner.StepContext) (instance string, port int, portAuto bool) {
	if ctx == nil {
		return InstanceAuto, 0, true
	}
	instance = strings.TrimSpace(ctx.GetParamString("mssql_instance", InstanceAuto))
	portAuto = IsPortAuto(ctx.GetParam("mssql_port"))
	port = PortParamInt(ctx)

	if IsPrimaryHost(ctx) {
		if v := strings.TrimSpace(ctx.GetParamString("mssql_primary_instance", "")); v != "" {
			instance = v
		}
		if v, ok := ctx.Params["mssql_primary_port"]; ok {
			portAuto = IsPortAuto(v)
			if !portAuto {
				port = portParamInt(v)
			}
		}
	} else {
		// Existing AG member (add-node topology maintenance): resolve instance from cached @@SERVERNAME.
		if !IsListedReplicaHost(ctx) {
			if name, ok := HAReplicaServerNameFromResults(ctx.Results, TargetHost(ctx)); ok && strings.TrimSpace(name) != "" {
				inst := InstanceNameFromReplicaServerName(name)
				return inst, 0, true
			}
		}
		if v := strings.TrimSpace(ctx.GetParamString("mssql_replica_instance", "")); v != "" {
			instance = v
		}
		if v, ok := ctx.Params["mssql_replica_port"]; ok {
			portAuto = IsPortAuto(v)
			if !portAuto {
				port = portParamInt(v)
			}
		}
	}
	return instance, port, portAuto
}

func portParamInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		if x > 0 {
			return x
		}
	case int64:
		if x > 0 {
			return int(x)
		}
	}
	return 0
}

func registryEntryNeedsRefresh(entry InstanceRegistryEntry) bool {
	if strings.TrimSpace(entry.Name) == "" {
		return true
	}
	if strings.TrimSpace(entry.InternalID) == "" {
		return true
	}
	if strings.TrimSpace(entry.SqlcmdPath) == "" {
		return true
	}
	return false
}

func cachedRegistryEntryMatchesSelection(ctx *runner.StepContext, entry InstanceRegistryEntry) bool {
	if ctx == nil {
		return true
	}
	wantInst, wantPort, portAuto := HAInstanceSelection(ctx)
	if !IsInstanceAuto(wantInst) && !strings.EqualFold(strings.TrimSpace(entry.Name), wantInst) {
		return false
	}
	if !portAuto && wantPort > 0 && entry.ListenPort != wantPort {
		return false
	}
	return true
}

// ResolveInstanceTarget resolves instance name and registry metadata.
func ResolveInstanceTarget(ctx *runner.StepContext, mode ResolveMode) (InstanceRegistryEntry, error) {
	if ctx == nil {
		return InstanceRegistryEntry{}, fmt.Errorf("nil context")
	}
	instance, portAuto := "", false
	port := 0
	if mode == ResolveModeExisting || mode == ResolveModeInstallNew {
		instance, port, portAuto = HAInstanceSelection(ctx)
	}

	if mode == ResolveModeInstallNew {
		if IsInstanceAuto(instance) {
			return InstanceRegistryEntry{}, fmt.Errorf("mssql install requires explicit --mssql-instance (auto not allowed for new install)")
		}
		listenPort := port
		if portAuto || listenPort <= 0 {
			listenPort = DefaultPort
		}
		entry := InstanceRegistryEntry{
			Name:        instance,
			ListenPort:  listenPort,
			ServiceName: ServiceNameForInstance(instance),
		}
		ApplyResolvedInstance(ctx, entry)
		return entry, nil
	}

	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return InstanceRegistryEntry{}, err
	}
	if len(entries) == 0 {
		return InstanceRegistryEntry{}, fmt.Errorf("no SQL Server instances found in registry")
	}

	switch {
	case IsInstanceAuto(instance) && portAuto:
		return resolveSingleOrList(ctx, entries)
	case IsInstanceAuto(instance) && !portAuto:
		return resolveByPort(ctx, entries, port)
	case !IsInstanceAuto(instance) && portAuto:
		return resolveByInstanceName(ctx, entries, instance)
	default:
		return resolveByInstanceAndPort(ctx, entries, instance, port)
	}
}

func resolveSingleOrList(ctx *runner.StepContext, entries []InstanceRegistryEntry) (InstanceRegistryEntry, error) {
	switch len(entries) {
	case 1:
		ApplyResolvedInstance(ctx, entries[0])
		logInstanceResolve(ctx, entries[0].ListenPort, entries[0])
		return entries[0], nil
	default:
		return InstanceRegistryEntry{}, fmt.Errorf("%s", FormatMultipleInstancesError(entries))
	}
}

func resolveByPort(ctx *runner.StepContext, entries []InstanceRegistryEntry, port int) (InstanceRegistryEntry, error) {
	if port <= 0 {
		return InstanceRegistryEntry{}, fmt.Errorf("invalid mssql_port: %d", port)
	}
	matches := FindInstanceByPort(entries, port)
	switch len(matches) {
	case 0:
		return InstanceRegistryEntry{}, fmt.Errorf("no SQL instance listens on TCP port %d (registry scan)", port)
	case 1:
		ApplyResolvedInstance(ctx, matches[0])
		logInstanceResolve(ctx, port, matches[0])
		return matches[0], nil
	default:
		return InstanceRegistryEntry{}, fmt.Errorf("%s", FormatMultipleInstancesError(matches))
	}
}

func resolveByInstanceName(ctx *runner.StepContext, entries []InstanceRegistryEntry, instance string) (InstanceRegistryEntry, error) {
	entry, ok := FindInstanceByName(entries, instance)
	if !ok {
		return InstanceRegistryEntry{}, fmt.Errorf("SQL instance %q not found in registry", instance)
	}
	if entry.ListenPort <= 0 {
		return InstanceRegistryEntry{}, fmt.Errorf(
			"instance %q has no TCP port in registry; set --mssql-port explicitly",
			instance,
		)
	}
	ApplyResolvedInstance(ctx, entry)
	logInstanceResolve(ctx, entry.ListenPort, entry)
	return entry, nil
}

func resolveByInstanceAndPort(ctx *runner.StepContext, entries []InstanceRegistryEntry, instance string, port int) (InstanceRegistryEntry, error) {
	if port <= 0 {
		return InstanceRegistryEntry{}, fmt.Errorf("invalid mssql_port: %d", port)
	}
	entry, ok := FindInstanceByName(entries, instance)
	if !ok {
		return InstanceRegistryEntry{}, fmt.Errorf("SQL instance %q not found in registry", instance)
	}
	if entry.ListenPort != port {
		return InstanceRegistryEntry{}, fmt.Errorf(
			"instance %s registry port %d != requested %d",
			instance, entry.ListenPort, port,
		)
	}
	ApplyResolvedInstance(ctx, entry)
	logInstanceResolve(ctx, port, entry)
	return entry, nil
}

// InstanceNameForHost returns SQL instance name for a topology host (registry or split HA params).
func InstanceNameForHost(ctx *runner.StepContext, host string) string {
	host = strings.TrimSpace(host)
	if host != "" && ctx != nil && ctx.Results != nil {
		if entry, ok := ctx.Results[RegistryEntryResultKey(host)].(InstanceRegistryEntry); ok && strings.TrimSpace(entry.Name) != "" {
			return entry.Name
		}
	}
	if ctx != nil && ctx.Executor != nil && (host == "" || strings.EqualFold(host, ctx.Executor.Host())) {
		return LayoutInstanceName(ctx)
	}
	if ctx == nil {
		return DefaultInstance
	}
	primary := ResolvePrimaryHost(ctx)
	if strings.EqualFold(host, primary) {
		if v := strings.TrimSpace(ctx.GetParamString("mssql_primary_instance", "")); v != "" && !IsInstanceAuto(v) {
			return v
		}
	} else if host != "" {
		if name, ok := HAReplicaServerNameFromResults(ctx.Results, host); ok && strings.TrimSpace(name) != "" {
			if inst := InstanceNameFromReplicaServerName(name); !IsInstanceAuto(inst) {
				return inst
			}
		}
		if v := strings.TrimSpace(ctx.GetParamString("mssql_replica_instance", "")); v != "" && !IsInstanceAuto(v) {
			return v
		}
	}
	inst, _, _ := HAInstanceSelection(ctx)
	if !IsInstanceAuto(inst) {
		return inst
	}
	return DefaultInstance
}

// LayoutInstanceName returns instance name for setup.ini / path layout on the current host.
func LayoutInstanceName(ctx *runner.StepContext) string {
	if entry, ok := RegistryEntryFromContext(ctx); ok && strings.TrimSpace(entry.Name) != "" {
		return entry.Name
	}
	if inst, _, _ := HAInstanceSelection(ctx); !IsInstanceAuto(inst) {
		return inst
	}
	return DefaultInstance
}

// ApplyResolvedInstance stores per-host registry entry; updates shared Params only on single-host install.
func ApplyResolvedInstance(ctx *runner.StepContext, entry InstanceRegistryEntry) {
	if ctx == nil {
		return
	}
	StoreRegistryEntry(ctx, entry)
	if IsHATopology(ctx) {
		return
	}
	if ctx.Params == nil {
		ctx.Params = map[string]interface{}{}
	}
	if strings.TrimSpace(entry.Name) != "" {
		ctx.Params["mssql_instance"] = entry.Name
	}
	if entry.ListenPort > 0 {
		ctx.Params["mssql_port"] = entry.ListenPort
	}
}

func logInstanceResolve(ctx *runner.StepContext, port int, entry InstanceRegistryEntry) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	ctx.Logger.DebugWrite("INFO", fmt.Sprintf(
		"phase=instance-resolve host=%s port=%d instance=%s internalId=%s sqlpath=%s sqlcmd=%s",
		TargetHost(ctx), port, entry.Name, entry.InternalID, entry.SQLPath, entry.SqlcmdPath,
	))
	ctx.Logger.Info("Resolved SQL instance: port=%d -> %s (ProductMajor=%d, ListenPort=%d)",
		port, entry.Name, entry.ProductMajor, entry.ListenPort)
}
