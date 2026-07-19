package os

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestNormalizeArch(t *testing.T) {
	tests := map[string]string{
		"aarch64": "aarch64",
		"arm64":   "aarch64",
		"x86_64":  "x86_64",
		"amd64":   "x86_64",
		"x64":     "x86_64",
		"":        "x86_64",
		"ppc64le": "ppc64le",
	}
	for in, want := range tests {
		if got := NormalizeArch(in); got != want {
			t.Fatalf("NormalizeArch(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsAutoISODevice(t *testing.T) {
	for _, d := range []string{"", "auto", "AUTO", " Auto "} {
		if !IsAutoISODevice(d) {
			t.Fatalf("%q should be auto", d)
		}
	}
	for _, d := range []string{"/dev/sr0", "/dev/cdrom", "OracleLinux.iso"} {
		if IsAutoISODevice(d) {
			t.Fatalf("%q should not be auto", d)
		}
	}
}

func TestDefaultBlockDevices(t *testing.T) {
	devs := DefaultBlockDevices()
	if len(devs) != 2 || devs[0] != "/dev/cdrom" || devs[1] != "/dev/sr0" {
		t.Fatalf("unexpected block devices: %v", devs)
	}
}

func TestISOProfileFromOSInfo(t *testing.T) {
	tests := []struct {
		name string
		in   *runner.OSInfo
		want ISOProfile
	}{
		{
			name: "nil defaults",
			in:   nil,
			want: ISOProfile{Family: "rhel8", Arch: "x86_64", MinorVer: -1},
		},
		{
			name: "OL 8.8 aarch64",
			in: &runner.OSInfo{
				ID: "ol", VersionID: "8.8", Arch: "aarch64", IsRHEL8: true,
			},
			want: ISOProfile{Family: "rhel8", DistroID: "ol", MajorVer: 8, MinorVer: 8, Arch: "aarch64"},
		},
		{
			name: "RHEL 7.9 x86_64",
			in: &runner.OSInfo{
				ID: "rhel", VersionID: "7.9", Arch: "x86_64", IsRHEL7: true,
			},
			want: ISOProfile{Family: "rhel7", DistroID: "rhel", MajorVer: 7, MinorVer: 9, Arch: "x86_64"},
		},
		{
			name: "CentOS 8",
			in: &runner.OSInfo{
				ID: "centos", VersionID: "8", Arch: "x86_64", IsRHEL8: true,
			},
			want: ISOProfile{Family: "rhel8", DistroID: "centos", MajorVer: 8, MinorVer: -1, Arch: "x86_64"},
		},
		{
			name: "Kylin V10",
			in: &runner.OSInfo{
				ID: "kylin", VersionID: "V10", Arch: "aarch64", IsKylin: true, IsRHEL8: true,
			},
			want: ISOProfile{Family: "rhel8", DistroID: "kylin", MajorVer: 10, MinorVer: -1, Arch: "aarch64"},
		},
		{
			name: "Rocky 9",
			in: &runner.OSInfo{
				ID: "rocky", VersionID: "9.0", Arch: "x86_64", IsRHEL8: true,
			},
			want: ISOProfile{Family: "rhel8", DistroID: "rocky", MajorVer: 9, MinorVer: 0, Arch: "x86_64"},
		},
		{
			name: "unknown distro major 7 fallback",
			in: &runner.OSInfo{
				ID: "custom", VersionID: "7.6", Arch: "x86_64",
			},
			want: ISOProfile{Family: "rhel7", DistroID: "custom", MajorVer: 7, MinorVer: 6, Arch: "x86_64"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ISOProfileFromOSInfo(tc.in)
			if got.Family != tc.want.Family || got.DistroID != tc.want.DistroID ||
				got.MajorVer != tc.want.MajorVer || got.MinorVer != tc.want.MinorVer || got.Arch != tc.want.Arch {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestScoreISOFilename(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", DistroID: "ol", MajorVer: 8, MinorVer: 8, Arch: "aarch64"}

	tests := []struct {
		name    string
		iso     string
		minWant int
		maxWant int
	}{
		{
			name:    "best match OL8 U8 aarch64 dvd",
			iso:     "OracleLinux-R8-U8-aarch64-dvd.iso",
			minWant: 150,
			maxWant: 300,
		},
		{
			name:    "alt naming aarch64",
			iso:     "Oracle-Linux-Release-8-U8-aarch64-dvd.iso",
			minWant: 150,
			maxWant: 300,
		},
		{
			name:    "arch conflict x86",
			iso:     "OracleLinux-R8-U8-x86_64-dvd.iso",
			minWant: -1000,
			maxWant: -1000,
		},
		{
			name:    "major conflict centos7",
			iso:     "CentOS-7-x86_64-DVD.iso",
			minWant: -1000,
			maxWant: -1000,
		},
		{
			name:    "boot iso penalty",
			iso:     "OracleLinux-R8-U8-aarch64-boot.iso",
			minWant: 40,
			maxWant: 150,
		},
		{
			name:    "non iso extension",
			iso:     "OracleLinux-R8-U8-aarch64-dvd.img",
			minWant: -10000,
			maxWant: -10000,
		},
		{
			name:    "windows artifact",
			iso:     "windows-server.iso",
			minWant: -10000,
			maxWant: -10000,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := ScoreISOFilename(tc.iso, profile)
			if score < tc.minWant || score > tc.maxWant {
				t.Fatalf("ScoreISOFilename(%q)=%d want [%d,%d]", tc.iso, score, tc.minWant, tc.maxWant)
			}
		})
	}
}

func TestScoreISOFilename_kylinProfile(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", DistroID: "kylin", MajorVer: 10, MinorVer: -1, Arch: "aarch64"}
	score := ScoreISOFilename("Kylin-Server-V10-SP3-aarch64-dvd.iso", profile)
	if score < 100 {
		t.Fatalf("kylin iso should score high for kylin profile, got %d", score)
	}
}

func TestScoreISOFilename_x86Profile(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", DistroID: "rhel", MajorVer: 8, MinorVer: -1, Arch: "x86_64"}
	aarch := ScoreISOFilename("OracleLinux-R8-U8-aarch64-dvd.iso", profile)
	x86 := ScoreISOFilename("OracleLinux-R8-U8-x86_64-dvd.iso", profile)
	if aarch >= 0 {
		t.Fatalf("aarch64 iso should be rejected for x86 profile, score=%d", aarch)
	}
	if x86 <= 0 {
		t.Fatalf("x86 iso should score positive, score=%d", x86)
	}
}

func TestSelectBestISOFilename(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", DistroID: "ol", MajorVer: 8, MinorVer: 8, Arch: "aarch64"}
	names := []string{
		"CentOS-7-x86_64-DVD.iso",
		"Oracle-Linux-Release-8-U8-x86_64-dvd.iso",
		"OracleLinux-R8-U8-aarch64-dvd.iso",
		"Oracle-Linux-Release-8-U8-aarch64-dvd.iso",
		"random.iso",
	}
	got, score, err := SelectBestISOFilename(names, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got != "OracleLinux-R8-U8-aarch64-dvd.iso" && got != "Oracle-Linux-Release-8-U8-aarch64-dvd.iso" {
		t.Fatalf("got %q score=%d", got, score)
	}
	if score < 150 {
		t.Fatalf("expected high score, got %d", score)
	}
}

func TestSelectBestISOFilename_noMatch(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", DistroID: "ol", MajorVer: 8, MinorVer: 8, Arch: "aarch64"}
	_, _, err := SelectBestISOFilename([]string{
		"CentOS-7-x86_64-DVD.iso",
		"OracleLinux-R8-U8-x86_64-dvd.iso",
	}, profile)
	if err == nil {
		t.Fatal("expected error when no candidate matches")
	}
}

func TestSelectBestISOFilename_tieBreakAlphabetical(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", DistroID: "ol", MajorVer: 8, MinorVer: -1, Arch: "x86_64"}
	got, _, err := SelectBestISOFilename([]string{
		"OracleLinux-R8-U8-x86_64-dvd.iso",
		"Oracle-Linux-Release-8-U8-x86_64-dvd.iso",
	}, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Oracle-Linux-Release-8-U8-x86_64-dvd.iso" {
		t.Fatalf("tie-break should pick alphabetically first high scorer, got %q", got)
	}
}

func TestISOMetadataMatchesProfile(t *testing.T) {
	profile := ISOProfile{Family: "rhel8", MajorVer: 8, MinorVer: 8, Arch: "aarch64"}

	okMeta := ISOMetadata{Major: 8, Minor: 8, Arch: "aarch64", Family: "rhel8", Version: "8.8"}
	if !ISOMetadataMatchesProfile(okMeta, profile) {
		t.Fatal("expected match")
	}

	cases := []ISOMetadata{
		{Major: 7, Minor: 8, Arch: "aarch64", Family: "rhel7"},
		{Major: 8, Minor: 7, Arch: "aarch64", Family: "rhel8"},
		{Major: 8, Minor: 8, Arch: "x86_64", Family: "rhel8"},
		{Major: 8, Minor: 8, Arch: "aarch64", Family: "rhel7"},
	}
	for i, meta := range cases {
		if ISOMetadataMatchesProfile(meta, profile) {
			t.Fatalf("case %d should not match: %+v", i, meta)
		}
	}

	// major-only ISO on 8.8 OS is acceptable when minor unknown on ISO side
	majorOnly := ISOMetadata{Major: 8, Minor: -1, Arch: "aarch64", Family: "rhel8"}
	if !ISOMetadataMatchesProfile(majorOnly, profile) {
		t.Fatal("major-only ISO should match when minor not specified in metadata")
	}
}

func TestParseISOMetadataFromTreeinfo(t *testing.T) {
	content := `[general]
version = 8.8
arch = aarch64
`
	meta := ParseISOMetadataFromTreeinfo(content)
	if meta.Major != 8 || meta.Minor != 8 || meta.Arch != "aarch64" || meta.Family != "rhel8" || meta.Source != "treeinfo" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestParseISOMetadataFromDiscinfo(t *testing.T) {
	content := "Oracle Linux Server release 8.8 (aarch64)\n"
	meta := ParseISOMetadataFromDiscinfo(content)
	if meta.Major != 8 || meta.Minor != 8 || meta.Arch != "aarch64" || meta.Family != "rhel8" {
		t.Fatalf("unexpected meta: %+v", meta)
	}

	rhel7 := ParseISOMetadataFromDiscinfo("Red Hat Enterprise Linux Server release 7.9 (x86_64)\n")
	if rhel7.Major != 7 || rhel7.Minor != 9 || rhel7.Arch != "x86_64" || rhel7.Family != "rhel7" {
		t.Fatalf("unexpected rhel7 meta: %+v", rhel7)
	}
}

func TestParseISOMetadataFromMediaRepo(t *testing.T) {
	content := `# media.repo
[InstallMedia]
name=Oracle Linux
version=8.8
`
	meta := ParseISOMetadataFromMediaRepo(content)
	if meta.Major != 8 || meta.Minor != 8 || meta.Family != "rhel8" || meta.Source != "media.repo" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestMergeISOMetadata(t *testing.T) {
	a := ISOMetadata{Major: 8, Minor: -1, Source: "treeinfo"}
	b := ISOMetadata{Minor: 8, Arch: "aarch64", Source: "discinfo"}
	merged := MergeISOMetadata(a, b)
	if merged.Major != 8 || merged.Minor != 8 || merged.Arch != "aarch64" || merged.Source != "treeinfo" {
		t.Fatalf("unexpected merged: %+v", merged)
	}
}

func TestDistroISOKeywords(t *testing.T) {
	tests := map[string]string{
		"ol":        "oraclelinux",
		"rhel":      "rhel",
		"centos":    "centos",
		"rocky":     "rocky",
		"almalinux": "alma",
		"kylin":     "kylin",
		"uos":       "uos",
	}
	for id, wantSub := range tests {
		kws := distroISOKeywords(id)
		found := false
		for _, kw := range kws {
			if strings.Contains(kw, wantSub) || kw == wantSub {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("distro %q keywords %v should contain %q", id, kws, wantSub)
		}
	}
}

func TestExtractMajorCandidates(t *testing.T) {
	name := "OracleLinux-R8-U8-aarch64-dvd.iso"
	cands := extractMajorCandidates(strings.ToLower(strings.TrimSuffix(name, ".iso")))
	has8 := false
	for _, c := range cands {
		if c == 8 {
			has8 = true
		}
	}
	if !has8 {
		t.Fatalf("expected major 8 in candidates, got %v", cands)
	}
}
