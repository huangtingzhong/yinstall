package mssql

import (
	"strings"
	"testing"
)

func TestFormatInstanceServiceNotRunningError(t *testing.T) {
	entry := InstanceRegistryEntry{Name: "MSSQLSERVER", ListenPort: 1433, ServiceName: "MSSQLSERVER"}
	msg := formatInstanceServiceNotRunningError(entry, "MSSQLSERVER", InstanceServiceStopped)
	if !strings.Contains(msg, "MSSQLSERVER") || !strings.Contains(msg, "1433") || !strings.Contains(msg, "not running") {
		t.Fatalf("got %q", msg)
	}
	msg = formatInstanceServiceNotRunningError(entry, "MSSQLSERVER", InstanceServiceMissing)
	if !strings.Contains(msg, "not found") || !strings.Contains(msg, "MSSQLSERVER") {
		t.Fatalf("got %q", msg)
	}
}
