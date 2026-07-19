package mssql_test

import (
	"testing"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func TestSummaryOKLabel(t *testing.T) {
	if commonmssql.SummaryOKLabel(true) != "OK" || commonmssql.SummaryOKLabel(false) != "FAIL" {
		t.Fatal("unexpected OK/FAIL labels")
	}
}

func TestSetupMediaLabelPrefersLocalISO(t *testing.T) {
	ctx := &runner.StepContext{
		Results: map[string]interface{}{
			"mssql_setup_local_path": "/Users/yihan/Downloads/mssql/cn_sql_server_2019_enterprise_x64_dvd_2bfe815a.iso",
		},
	}
	got := commonmssql.SetupMediaLabel(ctx)
	want := "cn_sql_server_2019_enterprise_x64_dvd_2bfe815a.iso"
	if got != want {
		t.Fatalf("SetupMediaLabel() = %q, want %q", got, want)
	}
}

func TestInstallSummaryProductLine(t *testing.T) {
	ctx := &runner.StepContext{
		Results: map[string]interface{}{
			"mssql_setup_product_major": 13,
		},
	}
	got := commonmssql.InstallSummaryProductLine(ctx, commonmssql.Layout{SetupProductMajor: 13})
	if got != "major=13 (SQL Server 2016)" {
		t.Fatalf("InstallSummaryProductLine() = %q", got)
	}
}

func TestEnrichLayoutProgramPathsFromRegistry(t *testing.T) {
	entry := commonmssql.InstanceRegistryEntry{
		SQLPath:      `C:\Program Files\Microsoft SQL Server\MSSQL15.MSSQLSERVER\MSSQL`,
		InternalID:   "MSSQL15.MSSQLSERVER",
		ProductMajor: 15,
	}
	layout := commonmssql.EnrichLayoutProgramPathsFromRegistry(commonmssql.Layout{}, entry)
	if layout.InstanceDir != `C:\Program Files\Microsoft SQL Server\MSSQL15.MSSQLSERVER` {
		t.Fatalf("instance_dir: %q", layout.InstanceDir)
	}
	if layout.SharedDir != `C:\Program Files\Microsoft SQL Server\150` {
		t.Fatalf("shared_dir: %q", layout.SharedDir)
	}
}
