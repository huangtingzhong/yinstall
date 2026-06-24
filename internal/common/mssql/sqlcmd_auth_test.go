package mssql

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestSqlcmdAuthArgsUsesProbedMode(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_sa_password": "secret",
		},
		Results: map[string]interface{}{},
	}
	ctx.Results[sqlcmdAuthResultKey(ctx)] = SqlcmdAuthIntegrated
	got := SqlcmdAuthArgs(ctx)
	if got != "-E" {
		t.Fatalf("integrated should ignore sa password, got %q", got)
	}

	ctx.Results[sqlcmdAuthResultKey(ctx)] = SqlcmdAuthSQL
	got = SqlcmdAuthArgs(ctx)
	if !strings.Contains(got, `-U sa`) || !strings.Contains(got, "secret") {
		t.Fatalf("sql mode should use sa password, got %q", got)
	}
}

func TestFormatSqlcmdRemoteProgramFilesPath(t *testing.T) {
	got := FormatSqlcmdRemote(
		`D:\Program Files\Microsoft SQL Server\Client SDK\ODBC\130\Tools\Binn\sqlcmd.exe`,
		"localhost,1433", "-E", `-Q 'SELECT 1' -b`,
	)
	if !strings.HasPrefix(got, "powershell -NoProfile -Command ") {
		t.Fatalf("expected powershell wrapper, got %q", got)
	}
	if !strings.Contains(got, `Program Files`) {
		t.Fatalf("path lost: %q", got)
	}
	if strings.Contains(got, `'D:\Program'`) {
		t.Fatalf("path truncated: %q", got)
	}
}

func TestSqlcmdAuthArgsDefaultIntegrated(t *testing.T) {
	ctx := &runner.StepContext{
		Params:  map[string]interface{}{"mssql_sa_password": "x"},
		Results: map[string]interface{}{},
	}
	if got := SqlcmdAuthArgs(ctx); got != "-E" {
		t.Fatalf("before probe default -E, got %q", got)
	}
}

func TestDisplaySqlcmdAuth(t *testing.T) {
	ctx := &runner.StepContext{Results: map[string]interface{}{}}
	ctx.Results[sqlcmdAuthResultKey(ctx)] = SqlcmdAuthIntegrated
	if got := DisplaySqlcmdAuth(ctx); !strings.Contains(got, "Windows") {
		t.Fatalf("got %q", got)
	}
	ctx.Results[sqlcmdAuthResultKey(ctx)] = SqlcmdAuthSQL
	if got := DisplaySqlcmdAuth(ctx); !strings.Contains(got, "SQL Server") {
		t.Fatalf("got %q", got)
	}
}

func TestSqlcmdConnectionExample(t *testing.T) {
	ctx := &runner.StepContext{
		Params:  map[string]interface{}{},
		Results: map[string]interface{}{},
	}
	ctx.Results[sqlcmdAuthResultKey(ctx)] = SqlcmdAuthIntegrated
	if got := SqlcmdConnectionExample(ctx, "10.0.0.1,1433"); !strings.Contains(got, "-E") {
		t.Fatalf("got %q", got)
	}
	ctx.Results[sqlcmdAuthResultKey(ctx)] = SqlcmdAuthSQL
	if got := SqlcmdConnectionExample(ctx, "10.0.0.1,1433"); !strings.Contains(got, "-U sa") {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSAPassword(t *testing.T) {
	if got := ResolveSAPassword("", false); got != "" {
		t.Fatalf("HA/remove empty explicit = %q, want empty", got)
	}
	if got := ResolveSAPassword("", true); got != DefaultSAPassword {
		t.Fatalf("install empty explicit = %q, want default", got)
	}
	if got := ResolveSAPassword(" custom ", true); got != "custom" {
		t.Fatalf("explicit password = %q, want custom", got)
	}
}
