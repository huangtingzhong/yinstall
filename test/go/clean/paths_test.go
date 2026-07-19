package clean_test

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/steps/clean"
)

func TestPathMatchLiteralsForPS_instanceDataAndParent(t *testing.T) {
	t.Parallel()
	pats := clean.PathMatchLiteralsForPS("/data/yashan/cust_data_3788/db-1-1")
	joined := strings.Join(pats, "\n")
	if !strings.Contains(joined, "/data/yashan/cust_data_3788/") {
		t.Fatalf("expected parent prefix in %v", pats)
	}
	if strings.Contains(joined, "/data/yashan/") && !strings.Contains(joined, "/data/yashan/cust_") {
		// allow cust_ paths; forbid bare /data/yashan/ as a standalone pat
	}
	for _, pat := range pats {
		if pat == "/data/yashan/" {
			t.Fatalf("must not emit host-wide /data/yashan/ prefix; got %v", pats)
		}
	}
	if !strings.Contains(joined, "/data/yashan/cust_data_3788/db-1-1") {
		t.Fatalf("expected bare instance path in %v", pats)
	}
	cmd := "yasdb open -D /data/yashan/cust_data_3788/db-1-1"
	matched := false
	for _, pat := range pats {
		if strings.Contains(cmd, pat) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("no literal matched cmdline %q; pats=%v", cmd, pats)
	}
}

func TestPathMatchLiteralsForPS_dataRootDoesNotUseHostParent(t *testing.T) {
	t.Parallel()
	pats := clean.PathMatchLiteralsForPS("/data/yashan/cust_data_3788")
	for _, pat := range pats {
		if pat == "/data/yashan/" {
			t.Fatalf("data root must not add /data/yashan/; got %v", pats)
		}
	}
}

func TestPathMatchLiteralsForPS_doesNotBareAmbiguousHome(t *testing.T) {
	t.Parallel()
	pats := clean.PathMatchLiteralsForPS("/data/yashan/yasdb_home")
	for _, pat := range pats {
		if pat == "/data/yashan/yasdb_home" {
			t.Fatalf("bare yasdb_home must not be emitted (ambiguous vs yasdb_home_3988); got %v", pats)
		}
	}
	if !strings.Contains(strings.Join(pats, "\n"), "/data/yashan/yasdb_home/") {
		t.Fatalf("expected trailing-slash prefix; got %v", pats)
	}
}

func TestPathsCompatibleLiterals_customHomeVsVersioned(t *testing.T) {
	t.Parallel()
	if !clean.PathsCompatibleLiterals(
		"/data/yashan/cust_home_3788/23.5.2.101",
		"/data/yashan/cust_home_3788",
	) {
		t.Fatal("versioned home should be compatible with install root")
	}
	if clean.PathsCompatibleLiterals(
		"/data/yashan/cust_home_3788",
		"/data/yashan/yasdb_home",
	) {
		t.Fatal("unrelated homes must not be compatible")
	}
}
