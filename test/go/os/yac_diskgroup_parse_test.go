package os_test

import (
	"testing"

	ossteps "github.com/yinstall/internal/steps/os"
)

func TestParseDiskGroupConfigPathOnly(t *testing.T) {
	dg, err := ossteps.ParseDiskGroupConfig("/dev/a,/dev/b")
	if err != nil {
		t.Fatal(err)
	}
	if dg == nil || dg.Name != "" || len(dg.Disks) != 2 {
		t.Fatalf("dg=%v", dg)
	}
}

func TestParseDiskGroupConfigLegacy(t *testing.T) {
	dg, err := ossteps.ParseDiskGroupConfig("datadg:/dev/a,/dev/b")
	if err != nil {
		t.Fatal(err)
	}
	if dg == nil || dg.Name != "datadg" {
		t.Fatalf("dg=%v", dg)
	}
}

func TestDiskGroupConfigsDistinct(t *testing.T) {
	a, _ := ossteps.ParseDiskGroupConfig("/dev/a")
	b, _ := ossteps.ParseDiskGroupConfig("/dev/b")
	same, _ := ossteps.ParseDiskGroupConfig("/dev/a")
	if !ossteps.DiskGroupConfigsDistinct(a, b) {
		t.Fatal("expected distinct paths")
	}
	if ossteps.DiskGroupConfigsDistinct(a, same) {
		t.Fatal("expected same paths")
	}
}
