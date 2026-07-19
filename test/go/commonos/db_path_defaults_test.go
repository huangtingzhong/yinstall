package commonos_test

import (
	"testing"

	commonos "github.com/yinstall/internal/common/os"
)

func TestConventionStageDir_defaultAndNonDefaultPort(t *testing.T) {
	t.Parallel()
	if got := commonos.ConventionStageDir("yashan", 1688); got != "/home/yashan/install" {
		t.Fatalf("1688: got %q", got)
	}
	if got := commonos.ConventionStageDir("yashan", 2688); got != "/home/yashan/install_2688" {
		t.Fatalf("2688: got %q", got)
	}
	if got := commonos.ConventionStageDir("tpcc", 3988); got != "/home/tpcc/install_3988" {
		t.Fatalf("user+port: got %q", got)
	}
	if got := commonos.ConventionStageDir("", commonos.DefaultDBBeginPort); got != "/home/yashan/install" {
		t.Fatalf("empty user: got %q", got)
	}
}

func TestResolveConventionStageDir_explicitWins(t *testing.T) {
	t.Parallel()
	explicit := "/data/custom/stage"
	if got := commonos.ResolveConventionStageDir(explicit, "yashan", 2688); got != explicit {
		t.Fatalf("explicit: got %q", got)
	}
	if got := commonos.ResolveConventionStageDir("  ", "yashan", 2688); got != "/home/yashan/install_2688" {
		t.Fatalf("blank falls back: got %q", got)
	}
	if got := commonos.ResolveConventionStageDir("", "yashan", 1688); got != "/home/yashan/install" {
		t.Fatalf("empty+1688: got %q", got)
	}
}

func TestDefaultDBClusterName_defaultAndNonDefaultPort(t *testing.T) {
	t.Parallel()
	if got := commonos.DefaultDBClusterName(1688); got != "yashandb" {
		t.Fatalf("1688: got %q", got)
	}
	if got := commonos.DefaultDBClusterName(commonos.DefaultDBBeginPort); got != "yashandb" {
		t.Fatalf("DefaultDBBeginPort: got %q", got)
	}
	if got := commonos.DefaultDBClusterName(2688); got != "yashandb_2688" {
		t.Fatalf("2688: got %q", got)
	}
	if got := commonos.DefaultDBClusterName(0); got != "yashandb" {
		t.Fatalf("port<=0 treats as default: got %q", got)
	}
}

func TestResolveDBClusterName_explicitWins(t *testing.T) {
	t.Parallel()
	if got := commonos.ResolveDBClusterName("mycluster", 2688); got != "mycluster" {
		t.Fatalf("explicit: got %q", got)
	}
	if got := commonos.ResolveDBClusterName("  ", 2688); got != "yashandb_2688" {
		t.Fatalf("blank falls back: got %q", got)
	}
	if got := commonos.ResolveDBClusterName("", 1688); got != "yashandb" {
		t.Fatalf("empty+1688: got %q", got)
	}
}

func TestIsConventionStageDir(t *testing.T) {
	t.Parallel()
	if !commonos.IsConventionStageDir("/home/yashan/install", "yashan", 1688) {
		t.Fatal("want convention match for 1688")
	}
	if commonos.IsConventionStageDir("/home/yashan/install", "yashan", 2688) {
		t.Fatal("1688 path must not match port 2688 convention")
	}
	if !commonos.IsConventionStageDir("/home/yashan/install_2688", "yashan", 2688) {
		t.Fatal("want convention match for 2688")
	}
}
