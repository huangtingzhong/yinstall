package mssql

import (
	"strings"
	"testing"
)

func TestRestoreMirrorDBSQLWithMove(t *testing.T) {
	files := []MirrorRestoreFile{
		{LogicalName: "TestMirrorDB", Type: "D", PhysicalName: `C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\DATA\TestMirrorDB.mdf`},
		{LogicalName: "TestMirrorDB_log", Type: "L", PhysicalName: `C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\DATA\TestMirrorDB_log.ldf`},
	}
	sql := RestoreMirrorDBSQLWithMove("TestMirrorDB", `C:\bak\TestMirrorDB.bak`, `C:\SQLData\SQL2\Data`, `C:\SQLData\SQL2\Log`, files)
	for _, want := range []string{
		"RESTORE DATABASE [TestMirrorDB]",
		"MOVE N'TestMirrorDB' TO N'C:\\SQLData\\SQL2\\Data\\TestMirrorDB.mdf'",
		"MOVE N'TestMirrorDB_log' TO N'C:\\SQLData\\SQL2\\Log\\TestMirrorDB_log.ldf'",
		"WITH NORECOVERY, REPLACE",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in SQL: %s", want, sql)
		}
	}
}

func TestParseRestoreFileListPipe(t *testing.T) {
	stdout := "restore_file\n----------\nTestMirrorDB|D|C:\\primary\\TestMirrorDB.mdf\nTestMirrorDB_log|L|C:\\primary\\TestMirrorDB_log.ldf\n\n(2 rows affected)\n"
	files, err := ParseRestoreFileList(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].LogicalName != "TestMirrorDB" || files[1].Type != "L" {
		t.Fatalf("unexpected files: %+v", files)
	}
}

func TestParseRestoreFileListSqlcmdColumns(t *testing.T) {
	stdout := "LogicalName                      PhysicalName                                                                                         Type FileGroupName\n-------------------------------- ---------------------------------------------------------------------------------------------------- ---- ----------------\nTestMirrorDB                     C:\\Program Files\\Microsoft SQL Server\\MSSQL13.MSSQLSERVER\\MSSQL\\DATA\\TestMirrorDB.mdf                D    PRIMARY\nTestMirrorDB_log                 C:\\Program Files\\Microsoft SQL Server\\MSSQL13.MSSQLSERVER\\MSSQL\\DATA\\TestMirrorDB_log.ldf            L    NULL\n\n(2 rows affected)\n"
	files, err := ParseRestoreFileList(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("unexpected files: %+v", files)
	}
	if files[0].PhysicalName != `C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\DATA\TestMirrorDB.mdf` {
		t.Fatalf("physical: %q", files[0].PhysicalName)
	}
}
