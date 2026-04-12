package standby

import "testing"

func TestDefaultPrimaryStageDir(t *testing.T) {
	t.Parallel()
	if got := DefaultPrimaryStageDir("yashan", 3988); got != "/home/yashan/install_3988" {
		t.Fatalf("got %q", got)
	}
	if got := DefaultPrimaryStageDir("", 1688); got != "/home/yashan/install_1688" {
		t.Fatalf("empty user: got %q", got)
	}
}

func TestDefaultExpansionPaths(t *testing.T) {
	t.Parallel()
	if got := DefaultExpansionInstallPath("yashan", 3988); got != "/data/yashan/yasdb_home_3988" {
		t.Fatalf("install: got %q", got)
	}
	if got := DefaultExpansionDataPath("yashan", 3988); got != "/data/yashan/yasdb_data_3988" {
		t.Fatalf("data: got %q", got)
	}
	if got := DefaultExpansionLogPath("yashan", 3988); got != "/data/yashan/log_3988" {
		t.Fatalf("log: got %q", got)
	}
}
