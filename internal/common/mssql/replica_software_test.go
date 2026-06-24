package mssql

import "testing"

func TestSQLReleaseYearFromMajor(t *testing.T) {
	year, ok := SQLReleaseYearFromMajor(13)
	if !ok || year != 2016 {
		t.Fatalf("major 13 => year %d ok=%v, want 2016 true", year, ok)
	}
	major, ok := SQLMajorFromReleaseYear(2019)
	if !ok || major != 15 {
		t.Fatalf("year 2019 => major %d ok=%v, want 15 true", major, ok)
	}
}

func TestValidateSetupMediaMatchesPrimary(t *testing.T) {
	primary := MirrorInstanceInfo{
		ProductMajorVersion: "13",
		ProductVersion:      "13.0.6300.2",
		Edition:             "Enterprise",
	}
	if err := ValidateSetupMediaMatchesPrimary(`D:\soft\cn_sql_server_2016_enterprise_x64_dvd.iso`, primary); err != nil {
		t.Fatalf("2016 ISO should match major 13: %v", err)
	}
	if err := ValidateSetupMediaMatchesPrimary(`D:\soft\cn_sql_server_2019_enterprise_x64_dvd.iso`, primary); err == nil {
		t.Fatal("2019 ISO should not match primary major 13")
	}
}

func TestShouldSkipReplicaSoftwareInstall(t *testing.T) {
	primary := MirrorInstanceInfo{
		ProductVersion:      "13.0.6300.2",
		ProductMajorVersion: "13",
		Edition:             "Enterprise Edition (64-bit)",
		EngineEdition:       "3",
	}
	replicaPathOnly := MirrorInstanceInfo{ProductVersion: "13.0.6300.2", ProductMajorVersion: "13"}
	if !ShouldSkipReplicaSoftwareInstall(replicaPathOnly, primary) {
		t.Fatal("path-only replica with matching ProductVersion should skip install")
	}
	replicaMismatch := MirrorInstanceInfo{ProductVersion: "14.0.1000.0", ProductMajorVersion: "14"}
	if ShouldSkipReplicaSoftwareInstall(replicaMismatch, primary) {
		t.Fatal("mismatched version should not skip install")
	}
}

func TestHAStageParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"all", HAStageAll},
		{"a", HAStageAll},
		{"software", HAStageSoftware},
		{"s", HAStageSoftware},
		{"ha", HAStageHA},
		{"h", HAStageHA},
	} {
		got, err := ParseHAStage(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ParseHAStage(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if !HAIncludesSoftwareInstall(HAStageAll) || !HAIncludesHASetup(HAStageAll) {
		t.Fatal("all should include install and HA")
	}
	if HAIncludesHASetup(HAStageSoftware) {
		t.Fatal("software should not include HA")
	}
	if HAIncludesSoftwareInstall(HAStageHA) {
		t.Fatal("ha should not include install")
	}
}
