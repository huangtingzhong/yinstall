package standby_test

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/steps/standby"
)

func TestYFSDiskgroupFromPath(t *testing.T) {
	if g := standby.YFSDiskgroupFromPath("+DG0/dbfiles/system"); g != "DG0" {
		t.Fatalf("got %q", g)
	}
	if g := standby.YFSDiskgroupFromPath("+REDO/dbfiles/redo1"); g != "REDO" {
		t.Fatalf("got %q", g)
	}
	if g := standby.YFSDiskgroupFromPath("/data/yashan/x"); g != "" {
		t.Fatalf("fs path want empty, got %q", g)
	}
	if g := standby.YFSDiskgroupFromPath("+DG0"); g != "DG0" {
		t.Fatalf("bare dg got %q", g)
	}
}

func TestParsePrimaryYFSProbe_LabSameDG(t *testing.T) {
	stdout := `
NAME
--------------------------------
+DG0/dbfiles/system
+DG0/dbfiles/sysaux
+DG0/dbfiles/users

NAME
--------------------------------
+DG0/dbfiles/redo1
+DG0/dbfiles/redo2

NAME                                                 VALUE
---------------------------------------------------- ----------------------------------------
ARCHIVE_LOCAL_DEST                                   +DG0/arch_files

NAME
----
DG0
SYSTEM
`
	layout, err := standby.ParsePrimaryYFSProbe(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if layout.DataDG != "DG0" || layout.RedoDG != "DG0" {
		t.Fatalf("layout=%+v", layout)
	}
	if len(layout.DataDGs) != 1 || layout.DataDGs[0] != "DG0" {
		t.Fatalf("DataDGs=%v", layout.DataDGs)
	}
	if !strings.Contains(layout.ArchiveDest, "+DG0/arch_files") {
		t.Fatalf("arch=%q", layout.ArchiveDest)
	}
}

func TestParsePrimaryYFSProbe_MultiDataDG(t *testing.T) {
	stdout := `
+DG0/dbfiles/system
+DG0/dbfiles/sysaux
+DG0/dbfiles/users
+DG0/dbfiles/undo1
+DG1/dbfiles/tbs_dg1
+DG0/dbfiles/redo1
DG0
DG1
SYSTEM
`
	layout, err := standby.ParsePrimaryYFSProbe(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if layout.DataDG != "DG0" {
		t.Fatalf("majority DataDG=%q want DG0", layout.DataDG)
	}
	if len(layout.DataDGs) != 2 || layout.DataDGs[0] != "DG0" || layout.DataDGs[1] != "DG1" {
		t.Fatalf("DataDGs=%v", layout.DataDGs)
	}
}

func TestParsePrimaryYFSProbe_PrimaryRedoOnREDO(t *testing.T) {
	stdout := `
+DG0/dbfiles/system
+REDO/dbfiles/redo1
+REDO/dbfiles/redo2
ARCHIVE_LOCAL_DEST                                   +ARCH/arch_files
DG0
REDO
ARCH
SYSTEM
`
	layout, err := standby.ParsePrimaryYFSProbe(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if layout.DataDG != "DG0" || layout.RedoDG != "REDO" {
		t.Fatalf("layout=%+v", layout)
	}
	if layout.ArchiveDest != "+ARCH/arch_files" {
		t.Fatalf("arch=%q", layout.ArchiveDest)
	}
}

func TestDeriveStandbyCEYFSPatch(t *testing.T) {
	// 对齐主库：无 CONVERT（lab 默认）
	p := standby.DeriveStandbyCEYFSPatch(
		standby.PrimaryYFSLayout{DataDG: "DG0", RedoDG: "DG0", ArchiveDest: "+DG0/arch_files"},
		standby.StandbyYFSAvailability{},
	)
	if p.DataDiskgroup != "DG0" || p.RedoFileNameConvert != "" || p.ArchiveLocalDest != "+DG0/arch_files" {
		t.Fatalf("%+v", p)
	}

	// 生产样例：主 redo 在 REDO、备首轮无 REDO/ARCH → REDO_FILE_NAME_CONVERT 落到 DG0
	p2 := standby.DeriveStandbyCEYFSPatch(
		standby.PrimaryYFSLayout{DataDG: "DG0", RedoDG: "REDO", ArchiveDest: "+ARCH/arch_files"},
		standby.StandbyYFSAvailability{},
	)
	if p2.DataDiskgroup != "DG0" {
		t.Fatalf("%+v", p2)
	}
	if p2.RedoFileNameConvert != "'+REDO/dbfiles','+DG0/dbfiles'" || p2.ArchiveLocalDest != "+DG0/arch_files" {
		t.Fatalf("%+v", p2)
	}

	// 备有 ARCH：redo 落到 ARCH，ARCHIVE 落 ARCH
	p3 := standby.DeriveStandbyCEYFSPatch(
		standby.PrimaryYFSLayout{DataDG: "DG0", RedoDG: "REDO"},
		standby.StandbyYFSAvailability{HasARCH: true},
	)
	if p3.DataDiskgroup != "DG0" {
		t.Fatalf("%+v", p3)
	}
	if p3.RedoFileNameConvert != "'+REDO/dbfiles','+ARCH/dbfiles'" || p3.ArchiveLocalDest != "+ARCH/arch_files" {
		t.Fatalf("%+v", p3)
	}

	// 主库多 data 组：额外组映到众数 DG0（与真机验证一致）
	p4 := standby.DeriveStandbyCEYFSPatch(
		standby.PrimaryYFSLayout{DataDG: "DG0", DataDGs: []string{"DG0", "DG1"}, RedoDG: "DG0"},
		standby.StandbyYFSAvailability{},
	)
	if p4.DataDiskgroup != "DG0" || p4.DBFileNameConvert != "'+DG1/dbfiles','+DG0/dbfiles'" {
		t.Fatalf("%+v", p4)
	}
	if p4.RedoFileNameConvert != "" {
		t.Fatalf("unexpected redo convert: %+v", p4)
	}

	// 多个额外组：按 DataDGs 顺序（众数后字典序）拼多对
	p5 := standby.DeriveStandbyCEYFSPatch(
		standby.PrimaryYFSLayout{DataDG: "DG0", DataDGs: []string{"DG0", "DG1", "DG2"}, RedoDG: "DG0"},
		standby.StandbyYFSAvailability{},
	)
	wantDB := "'+DG1/dbfiles','+DG0/dbfiles','+DG2/dbfiles','+DG0/dbfiles'"
	if p5.DBFileNameConvert != wantDB {
		t.Fatalf("got %q want %q", p5.DBFileNameConvert, wantDB)
	}
}
