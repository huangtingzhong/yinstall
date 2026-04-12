package standby

import (
	"strings"
	"testing"
)

func TestExplainYasbootHostAddFailure(t *testing.T) {
	h := ExplainYasbootHostAddFailure("stderr: yasdb path: /data/yashan/yasdb_home/23.4.4.104 should be empty")
	if h == "" || len(h) < 20 {
		t.Fatal(h)
	}
}

func TestExplainYasbootNodeAddFailure(t *testing.T) {
	h := ExplainYasbootNodeAddFailure("node:1-2, get host0002 host info failed")
	if h == "" || len(h) < 20 {
		t.Fatal(h)
	}
	h2 := ExplainYasbootNodeAddFailure("connection is shut down")
	if !strings.Contains(h2, "RPC") && !strings.Contains(h2, "agent") {
		t.Fatalf("expected agent/RPC hint, got %q", h2)
	}
}
