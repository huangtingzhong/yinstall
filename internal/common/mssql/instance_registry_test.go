package mssql

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
)

func TestParseInstanceRegistryLine(t *testing.T) {
	line := "MSSQLSERVER|MSSQL13.MSSQLSERVER|1433|0|D:\\Program Files\\Microsoft SQL Server\\MSSQL13.MSSQLSERVER\\MSSQL|" +
		"D:\\Program Files\\Microsoft SQL Server\\MSSQL13.MSSQLSERVER\\MSSQL\\Binn|" +
		"D:\\DATA|D:\\BACKUP|13.0.5026.0|Enterprise Evaluation Edition|13|13.0.5026.0|130|" +
		"D:\\Program Files\\Microsoft SQL Server\\Client SDK\\ODBC\\130\\Tools\\Binn\\sqlcmd.exe"
	entry, err := ParseInstanceRegistryLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "MSSQLSERVER" || entry.ListenPort != 1433 {
		t.Fatalf("name/port: %+v", entry)
	}
	if entry.ProductMajor != 13 || entry.ToolsRegKey != "130" {
		t.Fatalf("major/tools: %+v", entry)
	}
	if !strings.HasSuffix(strings.ToLower(entry.SqlcmdPath), "sqlcmd.exe") {
		t.Fatalf("sqlcmd: %q", entry.SqlcmdPath)
	}
}

func TestFindInstanceByPort(t *testing.T) {
	entries := []InstanceRegistryEntry{
		{Name: "MSSQLSERVER", ListenPort: 1433},
		{Name: "MSSQLSERVER2", ListenPort: 1435},
	}
	if got := FindInstanceByPort(entries, 1435); len(got) != 1 || got[0].Name != "MSSQLSERVER2" {
		t.Fatalf("by port: %+v", got)
	}
	if got := FindInstanceByPort(entries, 9999); len(got) != 0 {
		t.Fatalf("missing port: %+v", got)
	}
}

func TestProductMajorFromInternalID(t *testing.T) {
	if got := ProductMajorFromInternalID("MSSQL13.MSSQLSERVER2"); got != 13 {
		t.Fatalf("got %d", got)
	}
}

func TestToolsRegKeyFromMajor(t *testing.T) {
	if got := ToolsRegKeyFromMajor(13); got != "130" {
		t.Fatalf("got %q", got)
	}
}

func TestJoinSqlcmdPath(t *testing.T) {
	got := JoinSqlcmdPath(`D:\Tools\Binn\`)
	if !strings.HasSuffix(got, `\sqlcmd.exe`) {
		t.Fatalf("got %q", got)
	}
}

func TestEffectiveListenPort(t *testing.T) {
	if got := EffectiveListenPort(1433, 49152); got != 1433 {
		t.Fatalf("static: %d", got)
	}
	if got := EffectiveListenPort(0, 49152); got != 49152 {
		t.Fatalf("dynamic: %d", got)
	}
}

func TestRegistryEntryPerHost(t *testing.T) {
	results := map[string]interface{}{}
	primary := InstanceRegistryEntry{Name: "MSSQLSERVER", ListenPort: 1433}
	replica := InstanceRegistryEntry{Name: "SQL2", ListenPort: 1435}
	StoreRegistryEntry(&runner.StepContext{
		Results:  results,
		Executor: &hostExecutor{host: "10.10.10.185"},
	}, primary)
	StoreRegistryEntry(&runner.StepContext{
		Results:  results,
		Executor: &hostExecutor{host: "10.10.10.186"},
	}, replica)

	pCtx := &runner.StepContext{Results: results, Executor: &hostExecutor{host: "10.10.10.185"}}
	rCtx := &runner.StepContext{Results: results, Executor: &hostExecutor{host: "10.10.10.186"}}
	gotP, ok := RegistryEntryFromContext(pCtx)
	if !ok || gotP.Name != "MSSQLSERVER" || gotP.ListenPort != 1433 {
		t.Fatalf("primary: ok=%v %+v", ok, gotP)
	}
	gotR, ok := RegistryEntryFromContext(rCtx)
	if !ok || gotR.Name != "SQL2" || gotR.ListenPort != 1435 {
		t.Fatalf("replica: ok=%v %+v", ok, gotR)
	}
}

type hostExecutor struct {
	host string
}

func (h *hostExecutor) Execute(string, bool) (runner.ExecResult, error) { return nil, nil }
func (h *hostExecutor) Host() string                                    { return h.host }
func (h *hostExecutor) Close() error                                    { return nil }
func (h *hostExecutor) Upload(string, string, *ssh.UploadContext) error { return nil }
