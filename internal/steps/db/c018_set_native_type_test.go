package db

import (
	"strings"
	"testing"
)

func TestInsertUseNativeUnderEachGroupNodeConfig_singleNode(t *testing.T) {
	raw := `cluster = "yashandb"
[[group]]
  group_type = "db"
  [[group.node]]
    cpu_limit = 4
    [group.node.config]
      ARCH_CLEAN_IGNORE_MODE = "BACKUP"
      CGROUP_ROOT_DIR = "/sys/fs/cgroup"
    [group.node.mysql_config]
      x = 1
`
	lines := normalizeTomlLines(raw)
	lines = stripUseNativeLines(lines)
	out := insertUseNativeUnderEachGroupNodeConfig(lines, "true")
	if err := verifyUseNativePerNodeConfigLines(out); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(out, "\n")
	if !strings.Contains(got, "USE_NATIVE_TYPE = true") {
		t.Fatalf("missing USE_NATIVE_TYPE: %q", got)
	}
	if strings.Count(got, "USE_NATIVE_TYPE") != 1 {
		t.Fatalf("expected exactly one USE_NATIVE_TYPE line: %q", got)
	}
}

func TestInsertUseNativeUnderEachGroupNodeConfig_twoSections(t *testing.T) {
	raw := `[[group]]
  [[group.node]]
    [group.node.config]
      A = 1
  [[group.node]]
    [group.node.config]
      B = 2
`
	lines := normalizeTomlLines(raw)
	lines = stripUseNativeLines(lines)
	out := insertUseNativeUnderEachGroupNodeConfig(lines, "false")
	if err := verifyUseNativePerNodeConfigLines(out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.Join(out, "\n"), "USE_NATIVE_TYPE = false") != 2 {
		t.Fatalf("expected two inserts: %q", strings.Join(out, "\n"))
	}
}

func TestInsertUseNativeUnderEachGroupNodeConfig_headerOnlyEOF(t *testing.T) {
	raw := `  [[group.node]]
    [group.node.config]
`
	lines := normalizeTomlLines(raw)
	lines = stripUseNativeLines(lines)
	out := insertUseNativeUnderEachGroupNodeConfig(lines, "true")
	if err := verifyUseNativePerNodeConfigLines(out); err != nil {
		t.Fatal(err)
	}
}
