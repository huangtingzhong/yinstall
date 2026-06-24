package mssql

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

const replicaSQLPendingInstallKey = "replica_sql_pending_install"

// ReplicaSQLPendingInstall reports replica has no SQL yet; MSH-091/092 will install.
func ReplicaSQLPendingInstall(results map[string]interface{}) bool {
	if results == nil {
		return false
	}
	v, ok := results[replicaSQLPendingInstallKey].(bool)
	return ok && v
}

// HAStageFromContext returns normalized mssql_ha_stage from params.
func HAStageFromContext(ctx *runner.StepContext) string {
	if ctx == nil {
		return DefaultHAStage()
	}
	if s := strings.TrimSpace(ctx.GetParamString("mssql_ha_stage", "")); s != "" {
		if st, err := ParseHAStage(s); err == nil {
			return st
		}
		return s
	}
	if s := strings.TrimSpace(ctx.GetParamString("stage", "")); s != "" {
		if st, err := ParseHAStage(s); err == nil {
			return st
		}
	}
	return DefaultHAStage()
}

// TargetInstanceInstalled reports whether the requested instance exists in registry with version metadata.
func TargetInstanceInstalled(ctx *runner.StepContext) (bool, error) {
	if ctx == nil {
		return false, nil
	}
	if ctx.DryRun {
		return false, nil
	}
	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	instance := strings.TrimSpace(ctx.GetParamString("mssql_instance", InstanceAuto))
	if IsInstanceAuto(instance) {
		instance, _, _ = HAInstanceSelection(ctx)
	}
	if IsInstanceAuto(instance) {
		return len(entries) > 0, nil
	}
	entry, ok := FindInstanceByName(entries, instance)
	return ok && strings.TrimSpace(entry.Version) != "", nil
}

// EnsureHAInstanceTarget resolves instance for HA: primary/replica-existing require running SQL;
// replica with --stage all/software skips connect when target instance is not installed yet.
func EnsureHAInstanceTarget(ctx *runner.StepContext) (InstanceRegistryEntry, error) {
	if ctx == nil {
		return InstanceRegistryEntry{}, nil
	}
	if IsPrimaryHost(ctx) {
		return EnsureConnectableInstance(ctx)
	}
	if !HAIncludesSoftwareInstall(HAStageFromContext(ctx)) {
		return EnsureConnectableInstance(ctx)
	}
	installed, err := TargetInstanceInstalled(ctx)
	if err != nil {
		return InstanceRegistryEntry{}, err
	}
	if installed {
		return EnsureConnectableInstance(ctx)
	}
	entry, err := ResolveInstanceTarget(ctx, ResolveModeInstallNew)
	if err != nil {
		return entry, err
	}
	ctx.SetResult(replicaSQLPendingInstallKey, true)
	if ctx.Logger != nil {
		ctx.Logger.Info("Replica SQL not installed on %s; deferred install in MSH-091/092 (instance=%s port=%d)",
			TargetHost(ctx), entry.Name, entry.ListenPort)
	}
	return entry, nil
}
