package mssql

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// InstanceServiceStatus is Windows service state for a SQL instance.
type InstanceServiceStatus string

const (
	InstanceServiceRunning InstanceServiceStatus = "running"
	InstanceServiceStopped InstanceServiceStatus = "stopped"
	InstanceServiceMissing InstanceServiceStatus = "missing"
)

// QueryInstanceServiceStatus returns MSSQL Windows service state on the target host.
func QueryInstanceServiceStatus(ctx *runner.StepContext, entry InstanceRegistryEntry) (InstanceServiceStatus, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil context")
	}
	if ctx.DryRun {
		return InstanceServiceRunning, nil
	}
	svc := strings.TrimSpace(entry.ServiceName)
	if svc == "" {
		svc = ServiceNameForInstance(entry.Name)
	}
	svcEsc := strings.ReplaceAll(svc, `'`, `''`)
	script := fmt.Sprintf(
		`$s=Get-Service -Name '%s' -ErrorAction SilentlyContinue; if (-not $s) { 'missing' } elseif ($s.Status -eq 'Running') { 'running' } else { $s.Status.ToString() }`,
		svcEsc,
	)
	res, err := ctx.Execute(`powershell -NoProfile -Command "`+script+`"`, false)
	if err != nil {
		return "", fmt.Errorf("query service %s: %w", svc, err)
	}
	if res == nil {
		return "", fmt.Errorf("query service %s: empty result", svc)
	}
	raw := strings.TrimSpace(strings.ToLower(res.GetStdout()))
	switch {
	case raw == "running":
		return InstanceServiceRunning, nil
	case raw == "missing" || raw == "":
		return InstanceServiceMissing, nil
	default:
		return InstanceServiceStopped, nil
	}
}

// EnsureInstanceServiceRunning fails fast when the SQL Server service is not running.
func EnsureInstanceServiceRunning(ctx *runner.StepContext, entry InstanceRegistryEntry) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if ctx.DryRun {
		return nil
	}
	status, err := QueryInstanceServiceStatus(ctx, entry)
	if err != nil {
		return err
	}
	if status == InstanceServiceRunning {
		if ctx.Logger != nil {
			svc := entry.ServiceName
			if svc == "" {
				svc = ServiceNameForInstance(entry.Name)
			}
			ctx.Logger.DebugWrite("INFO", fmt.Sprintf(
				"phase=instance-service host=%s service=%s status=running instance=%s port=%d",
				TargetHost(ctx), svc, entry.Name, entry.ListenPort,
			))
		}
		return nil
	}
	svc := strings.TrimSpace(entry.ServiceName)
	if svc == "" {
		svc = ServiceNameForInstance(entry.Name)
	}
	return errors.New(formatInstanceServiceNotRunningError(entry, svc, status))
}

func formatInstanceServiceNotRunningError(entry InstanceRegistryEntry, svc string, status InstanceServiceStatus) string {
	port := entry.ListenPort
	if port <= 0 {
		port = 0
	}
	switch status {
	case InstanceServiceMissing:
		return fmt.Sprintf(
			"SQL Server service %s not found (instance %s, port %d); verify instance name or install",
			svc, entry.Name, port,
		)
	default:
		return fmt.Sprintf(
			"SQL Server service %s is not running (instance %s, port %d, status=%s); start the service before continuing",
			svc, entry.Name, port, status,
		)
	}
}

// EnsureConnectableInstance resolves registry target and requires SQL Server service Running.
func EnsureConnectableInstance(ctx *runner.StepContext) (InstanceRegistryEntry, error) {
	entry, err := EnsureInstanceResolved(ctx)
	if err != nil {
		return entry, err
	}
	if err := EnsureInstanceServiceRunning(ctx, entry); err != nil {
		return entry, err
	}
	return entry, nil
}
