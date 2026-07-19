package db_test

import (
	"testing"

	"github.com/yinstall/internal/steps/db"
)

func TestParseYACDiskGroupPathOnly(t *testing.T) {
	dg, err := db.ParseYACDiskGroup("/dev/yfs/sys1,/dev/yfs/sys2")
	if err != nil {
		t.Fatal(err)
	}
	if dg == nil || dg.Name != "" {
		t.Fatalf("dg=%v", dg)
	}
	if got := db.FormatDiskList(dg); got != "/dev/yfs/sys1,/dev/yfs/sys2" {
		t.Fatalf("FormatDiskList=%q", got)
	}
}

func TestParseYACDiskGroupLegacyRole(t *testing.T) {
	dg, err := db.ParseYACDiskGroup("systemdg:/dev/yfs/sys1,/dev/yfs/sys2")
	if err != nil {
		t.Fatal(err)
	}
	if dg == nil || dg.Name != "systemdg" {
		t.Fatalf("dg=%v", dg)
	}
	if got := db.FormatDiskList(dg); got != "/dev/yfs/sys1,/dev/yfs/sys2" {
		t.Fatalf("FormatDiskList=%q", got)
	}
}

func TestParseYACDiskGroupEmpty(t *testing.T) {
	dg, err := db.ParseYACDiskGroup("")
	if err != nil || dg != nil {
		t.Fatalf("empty: dg=%v err=%v", dg, err)
	}
}

func TestParseYACDiskGroupInvalid(t *testing.T) {
	if _, err := db.ParseYACDiskGroup("role:"); err == nil {
		t.Fatal("expected error for empty disks after role")
	}
	if _, err := db.ParseYACDiskGroup(","); err == nil {
		t.Fatal("expected error for empty disk list")
	}
}

func TestMapYACDiskGroupParamPathOnly(t *testing.T) {
	got := db.MapYACDiskGroupParam("systemdg:/dev/a,/dev/b", func(disk string, _ int) string {
		return disk + "x"
	})
	if got != "/dev/ax,/dev/bx" {
		t.Fatalf("got %q", got)
	}
}
