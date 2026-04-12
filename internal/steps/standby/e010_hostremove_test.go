package standby

import "testing"

func TestYasbootHostRemoveOutputInRemoving(t *testing.T) {
	if !yasbootHostRemoveOutputInRemoving("node [1-2] in removing hosts, you cannot remove it") {
		t.Fatal("expected in-removing")
	}
	if yasbootHostRemoveOutputInRemoving("host removed ok") {
		t.Fatal("unexpected")
	}
}
