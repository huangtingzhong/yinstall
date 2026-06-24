package mssql

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

const defaultInstanceReadyTimeout = 15 * time.Minute

// WaitForInstanceReady polls until SQL service, registry, sqlcmd path, and SELECT 1 succeed.
func WaitForInstanceReady(ctx *runner.StepContext, timeout time.Duration) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	if timeout <= 0 {
		timeout = defaultInstanceReadyTimeout
	}
	entry, err := EnsureInstanceResolved(ctx)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := checkInstanceReadyOnce(ctx, entry); err == nil {
			if ctx.Logger != nil {
				ctx.Logger.Info("SQL instance ready: %s port=%d", entry.Name, entry.ListenPort)
			}
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(15 * time.Second)
		entry, err = EnsureInstanceResolved(ctx)
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout waiting for SQL instance")
	}
	return fmt.Errorf("SQL instance %s not ready within %s: %w", entry.Name, timeout, lastErr)
}

// WaitForInstanceReadyAfterInstall waits for a freshly installed instance using registry
// metadata only (TCP port may still be dynamic until MS-009 configures it).
func WaitForInstanceReadyAfterInstall(ctx *runner.StepContext, timeout time.Duration) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	if timeout <= 0 {
		timeout = defaultInstanceReadyTimeout
	}
	instance := strings.TrimSpace(LayoutInstanceName(ctx))
	if instance == "" {
		return fmt.Errorf("empty SQL instance name (set --mssql-instance or --primary/replica-mssql-instance)")
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		entries, err := ListInstanceRegistry(ctx)
		if err != nil {
			lastErr = err
		} else if entry, ok := FindInstanceByName(entries, instance); ok {
			if entry.ListenPort <= 0 {
				lastErr = fmt.Errorf("instance %q has no TCP port in registry yet", instance)
			} else if err := checkInstanceReadyOnce(ctx, entry); err == nil {
				ApplyResolvedInstance(ctx, entry)
				if ctx.Logger != nil {
					ctx.Logger.Info("SQL instance ready after install: %s port=%d", entry.Name, entry.ListenPort)
				}
				return nil
			} else {
				lastErr = err
			}
		} else {
			lastErr = fmt.Errorf("SQL instance %q not found in registry", instance)
		}
		time.Sleep(15 * time.Second)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout waiting for SQL instance")
	}
	return fmt.Errorf("SQL instance %s not ready within %s: %w", instance, timeout, lastErr)
}

func checkInstanceReadyOnce(ctx *runner.StepContext, entry InstanceRegistryEntry) error {
	StoreRegistryEntry(ctx, entry)
	if err := EnsureInstanceServiceRunning(ctx, entry); err != nil {
		return err
	}
	if _, err := ListInstanceRegistry(ctx); err != nil {
		return err
	}
	if err := PrepareSqlcmdSession(ctx); err != nil {
		return err
	}
	cmd := SqlcmdQueryCommand(ctx, "SELECT 1 AS ok;")
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return err
	}
	if res == nil || res.GetExitCode() != 0 {
		return fmt.Errorf("sqlcmd ping failed")
	}
	return nil
}
