package mssql

import (
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestRestoreTargetDirsFromContextExplicitRestore(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_restore_data_dir": `D:\Restore\Data`,
			"mssql_restore_log_dir":  `D:\Restore\Log`,
		},
	}
	data, log, err := RestoreTargetDirsFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if data != `D:\Restore\Data` || log != `D:\Restore\Log` {
		t.Fatalf("got data=%q log=%q", data, log)
	}
}

func TestRestoreTargetDirsFromContextExplicitDataOnly(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"replica_mssql_restore_data_dir": `E:\SQL\Data`,
		},
	}
	data, log, err := RestoreTargetDirsFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if data != `E:\SQL\Data` || log != `E:\SQL\Data` {
		t.Fatalf("got data=%q log=%q", data, log)
	}
}

func TestRestoreTargetDirsFromContextCLIPathFlags(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_data_root": `C:\SQLData`,
			"mssql_instance":  DefaultInstance,
		},
	}
	data, log, err := RestoreTargetDirsFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantData := `C:\SQLData\MSSQLSERVER\Data`
	wantLog := `C:\SQLData\MSSQLSERVER\Log`
	if data != wantData || log != wantLog {
		t.Fatalf("got data=%q log=%q want data=%q log=%q", data, log, wantData, wantLog)
	}
}

func TestRestoreTargetDirsFromContextRegistry(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{},
		Results: map[string]interface{}{
			"mssql_registry_entry": InstanceRegistryEntry{
				Name:      "MSSQLSERVER",
				DataRoot:  `C:\SQLData\MSSQLSERVER`,
				BackupDir: `C:\SQLData\MSSQLSERVER\Backup`,
			},
		},
	}
	data, log, err := RestoreTargetDirsFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if data != `C:\SQLData\MSSQLSERVER\Data` || log != `C:\SQLData\MSSQLSERVER\Log` {
		t.Fatalf("got data=%q log=%q", data, log)
	}
}

func TestLayoutFromRegistryEntryYinstallLayout(t *testing.T) {
	entry := InstanceRegistryEntry{
		Name:       "SQL2",
		ListenPort: 1435,
		DataRoot:   `C:\SQLData\SQL2`,
		BackupDir:  `C:\SQLData\SQL2\Backup`,
	}
	layout := LayoutFromRegistryEntry(entry)
	if layout.UseSQLDefaults {
		t.Fatal("expected custom layout")
	}
	if layout.Base != `C:\SQLData\SQL2` {
		t.Fatalf("base: %q", layout.Base)
	}
	if layout.DataDir != `C:\SQLData\SQL2\Data` {
		t.Fatalf("data: %q", layout.DataDir)
	}
	if layout.LogDir != `C:\SQLData\SQL2\Log` {
		t.Fatalf("log: %q", layout.LogDir)
	}
	if layout.BackupDir != `C:\SQLData\SQL2\Backup` {
		t.Fatalf("backup: %q", layout.BackupDir)
	}
}

func TestLayoutFromRegistryEntryNestedSQLDataRoot185(t *testing.T) {
	// 185 field layout: SQLDataRoot nested under instance segment.
	entry := InstanceRegistryEntry{
		Name:       "SQL2",
		ListenPort: 1435,
		DataRoot:   `C:\SQLData\SQL2\MSSQL13.SQL2\MSSQL`,
		BackupDir:  `C:\SQLData\SQL2\Backup`,
	}
	layout := LayoutFromRegistryEntry(entry)
	if layout.Base != `C:\SQLData\SQL2` {
		t.Fatalf("base: %q", layout.Base)
	}
	if layout.DataDir != `C:\SQLData\SQL2\Data` {
		t.Fatalf("data: %q", layout.DataDir)
	}
	if layout.LogDir != `C:\SQLData\SQL2\Log` {
		t.Fatalf("log: %q", layout.LogDir)
	}
	if layout.BackupDir != `C:\SQLData\SQL2\Backup` {
		t.Fatalf("backup: %q", layout.BackupDir)
	}
}

func TestLayoutFromRegistryEntryFlatDataRoot(t *testing.T) {
	entry := InstanceRegistryEntry{
		Name:      "MSSQLSERVER",
		DataRoot:  `D:\DATA`,
		BackupDir: `D:\BACKUP`,
	}
	layout := LayoutFromRegistryEntry(entry)
	if layout.DataDir != `D:\DATA` {
		t.Fatalf("data: %q", layout.DataDir)
	}
	if layout.BackupDir != `D:\BACKUP` {
		t.Fatalf("backup: %q", layout.BackupDir)
	}
}

func TestLayoutFromRegistryEntrySQLPathOnly(t *testing.T) {
	entry := InstanceRegistryEntry{
		Name:    "MSSQLSERVER",
		SQLPath: `D:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL`,
	}
	layout := LayoutFromRegistryEntry(entry)
	if layout.DataDir != `D:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\DATA` {
		t.Fatalf("data: %q", layout.DataDir)
	}
	if layout.Base != `D:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER` {
		t.Fatalf("base: %q", layout.Base)
	}
	if layout.BackupDir != `D:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\Backup` {
		t.Fatalf("backup: %q", layout.BackupDir)
	}
}

func TestLayoutFromRegistryEntrySQLDataRootEqualsSQLPath(t *testing.T) {
	sqlPath := `C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL`
	entry := InstanceRegistryEntry{
		Name:     "MSSQLSERVER",
		SQLPath:  sqlPath,
		DataRoot: sqlPath,
	}
	layout := LayoutFromRegistryEntry(entry)
	wantData := joinWinPath(sqlPath, "DATA")
	if layout.DataDir != wantData {
		t.Fatalf("data: %q want %q", layout.DataDir, wantData)
	}
}

func TestUserDatabaseDirFromRegistry(t *testing.T) {
	sqlPath := `C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL`
	if got := userDatabaseDirFromRegistry(sqlPath, sqlPath); got != joinWinPath(sqlPath, "DATA") {
		t.Fatalf("got %q", got)
	}
	if got := userDatabaseDirFromRegistry(`D:\DATA`, ""); got != `D:\DATA` {
		t.Fatalf("got %q", got)
	}
}
