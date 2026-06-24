package mssql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
)

func TestResolveSingleOrList(t *testing.T) {
	ctx := &runner.StepContext{
		Params:  map[string]interface{}{},
		Results: map[string]interface{}{},
	}
	single := []InstanceRegistryEntry{{
		Name: "MSSQLSERVER", ListenPort: 1433, SQLPath: `D:\SQL`, Version: "13.0",
	}}
	got, err := resolveSingleOrList(ctx, single)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "MSSQLSERVER" || got.ListenPort != 1433 {
		t.Fatalf("got %+v", got)
	}
	if ctx.GetParamString("mssql_instance", "") != "MSSQLSERVER" {
		t.Fatalf("instance param: %v", ctx.Params["mssql_instance"])
	}
	if ctx.Params["mssql_port"] != 1433 {
		t.Fatalf("port param: %v", ctx.Params["mssql_port"])
	}

	multi := []InstanceRegistryEntry{
		{Name: "MSSQLSERVER", ListenPort: 1433, SQLPath: `D:\A`, Version: "13.0"},
		{Name: "MSSQLSERVER2", ListenPort: 1435, SQLPath: `D:\B`, Version: "13.0"},
	}
	_, err = resolveSingleOrList(ctx, multi)
	if err == nil {
		t.Fatal("expected error for multiple instances")
	}
	if !strings.Contains(err.Error(), "multiple SQL Server instances") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "MSSQLSERVER2") {
		t.Fatalf("missing instance list: %v", err)
	}
}

func TestResolveByInstanceName(t *testing.T) {
	ctx := &runner.StepContext{
		Params:  map[string]interface{}{},
		Results: map[string]interface{}{},
	}
	entries := []InstanceRegistryEntry{
		{Name: "MSSQLSERVER", ListenPort: 1433},
		{Name: "MSSQLSERVER2", ListenPort: 1435, SQLPath: `D:\SQL2`},
	}
	got, err := resolveByInstanceName(ctx, entries, "MSSQLSERVER2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenPort != 1435 {
		t.Fatalf("got %+v", got)
	}
	if ctx.Params["mssql_port"] != 1435 {
		t.Fatalf("port not set: %v", ctx.Params["mssql_port"])
	}
}

func TestResolveByPort(t *testing.T) {
	ctx := &runner.StepContext{
		Params:  map[string]interface{}{},
		Results: map[string]interface{}{},
	}
	entries := []InstanceRegistryEntry{
		{Name: "MSSQLSERVER", ListenPort: 1433},
		{Name: "MSSQLSERVER2", ListenPort: 1435},
	}
	got, err := resolveByPort(ctx, entries, 1435)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "MSSQLSERVER2" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveInstallNewPortAuto(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_instance": "MYINST",
			"mssql_port":     PortAuto,
		},
		Results: map[string]interface{}{},
	}
	got, err := ResolveInstanceTarget(ctx, ResolveModeInstallNew)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenPort != DefaultPort {
		t.Fatalf("listen port: %d", got.ListenPort)
	}
	if ctx.Params["mssql_port"] != DefaultPort {
		t.Fatalf("param port: %v", ctx.Params["mssql_port"])
	}
}

func TestRegistryEntryNeedsRefresh(t *testing.T) {
	stub := InstanceRegistryEntry{Name: "MSSQLSERVER", ListenPort: 1433, ServiceName: "MSSQLSERVER"}
	if !registryEntryNeedsRefresh(stub) {
		t.Fatal("install-new placeholder should need refresh")
	}
	full := InstanceRegistryEntry{
		Name: "MSSQLSERVER", InternalID: "MSSQL13.MSSQLSERVER", ListenPort: 1433,
		SqlcmdPath: `C:\Program Files\Microsoft SQL Server\Client SDK\ODBC\130\Tools\Binn\sqlcmd.exe`,
	}
	if registryEntryNeedsRefresh(full) {
		t.Fatal("complete registry entry should not need refresh")
	}
}

func TestIsPortAuto(t *testing.T) {
	for _, v := range []interface{}{PortAuto, "AUTO", "", nil, "auto"} {
		if !IsPortAuto(v) {
			t.Fatalf("expected auto for %v", v)
		}
	}
	if IsPortAuto(1433) {
		t.Fatal("1433 is not auto")
	}
}

func TestNormalizePortParam(t *testing.T) {
	v, err := NormalizePortParam("auto")
	if err != nil || v != PortAuto {
		t.Fatalf("auto: %v %v", v, err)
	}
	v, err = NormalizePortParam("1435")
	if err != nil {
		t.Fatal(err)
	}
	if v.(int) != 1435 {
		t.Fatalf("got %v", v)
	}
	if _, err := NormalizePortParam("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyResolvedInstanceHATopologySkipsSharedParams(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_topology": string(TopologyMirror),
			"mssql_instance": InstanceAuto,
		},
		Results: map[string]interface{}{},
	}
	entry := InstanceRegistryEntry{Name: "MSSQLSERVER", ListenPort: 1433, InternalID: "MSSQL13.MSSQLSERVER"}
	ApplyResolvedInstance(ctx, entry)
	if ctx.GetParamString("mssql_instance", "") != InstanceAuto {
		t.Fatalf("HA topology should not overwrite shared mssql_instance: %v", ctx.Params["mssql_instance"])
	}
	if _, ok := RegistryEntryFromContext(ctx); !ok {
		t.Fatal("expected per-host registry entry in Results")
	}
}

func TestSQLPortForHostPeerUnknown(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_primary_host": "10.0.0.1",
			"mssql_port":         1433,
		},
		Results: map[string]interface{}{
			RegistryEntryResultKey("10.0.0.1"): InstanceRegistryEntry{Name: "MSSQLSERVER", ListenPort: 1433},
		},
	}
	if p := SQLPortForHost(ctx, "10.0.0.2"); p != 0 {
		t.Fatalf("unknown peer port should be 0, got %d", p)
	}
	if p := SQLPortForHost(ctx, "10.0.0.1"); p != 1433 {
		t.Fatalf("primary registry port: got %d", p)
	}
}

func TestResolveInstallNewUsesReplicaInstanceSelection(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_topology":         string(TopologyMirror),
			"mssql_primary_host":     "10.0.0.1",
			"mssql_instance":         "MSSQLSERVER",
			"mssql_replica_instance": "SQL2",
			"mssql_port":             PortAuto,
		},
		Results:  map[string]interface{}{},
		Executor: &resolveTestExecutor{host: "10.0.0.2"},
	}
	got, err := ResolveInstanceTarget(ctx, ResolveModeInstallNew)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "SQL2" {
		t.Fatalf("got instance %q", got.Name)
	}
}

func TestHAReplicaServerNameRejectsBareIP(t *testing.T) {
	ctx := &runner.StepContext{Results: map[string]interface{}{}}
	if got := HAReplicaServerName(ctx, "10.0.0.2"); got != "" {
		t.Fatalf("expected empty for bare IP, got %q", got)
	}
}

type resolveTestExecutor struct {
	host string
}

func (e *resolveTestExecutor) Host() string { return e.host }
func (e *resolveTestExecutor) Execute(string, bool) (runner.ExecResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (e *resolveTestExecutor) Close() error { return nil }
func (e *resolveTestExecutor) Upload(string, string, *ssh.UploadContext) error {
	return fmt.Errorf("not implemented")
}
