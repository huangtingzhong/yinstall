package commonos_test

import (
	"testing"

	commonos "github.com/yinstall/internal/common/os"
)

func TestNormalizeManagedHostsEntries(t *testing.T) {
	got := commonos.NormalizeManagedHostsEntries([]string{
		"  10.10.10.1   host1  alias ",
		"",
		"10.10.10.2\thost2",
	})
	want := []string{"10.10.10.1  host1", "10.10.10.2  host2"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestManagedHostsEntriesEqual(t *testing.T) {
	a := []string{"10.10.10.1   host1", "10.10.10.2  host2"}
	b := []string{"10.10.10.1  host1", "10.10.10.2  host2"}
	if !commonos.ManagedHostsEntriesEqual(a, b) {
		t.Fatal("expected equal after normalize")
	}
	c := []string{"10.10.10.1  host1"}
	if commonos.ManagedHostsEntriesEqual(a, c) {
		t.Fatal("expected not equal")
	}
}

func TestTextContentEqual(t *testing.T) {
	if !commonos.TextContentEqual("a\r\nb\n", "a\nb") {
		t.Fatal("expected equal after newline normalize")
	}
	if commonos.TextContentEqual("a", "b") {
		t.Fatal("expected not equal")
	}
}
