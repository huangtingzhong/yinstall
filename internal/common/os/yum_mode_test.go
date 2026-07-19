package os

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestNormalizeYumMode(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"none", ""},
		{"online", ""},
		{"local", YumModeLocal},
		{"local-iso", YumModeLocal},
		{"LOCAL", YumModeLocal},
		{" Local-ISO ", YumModeLocal},
		{"10.10.10.20", YumModeHTTP},
		{"10.10.10.20:8080", YumModeHTTP},
		{"http://10.10.10.20/ol", YumModeHTTP},
		{"https://mirror.example/pub", YumModeHTTP},
	}
	for _, tc := range tests {
		if got := NormalizeYumMode(tc.in); got != tc.want {
			t.Errorf("NormalizeYumMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseYumMode_httpEndpoints(t *testing.T) {
	kind, ep, err := ParseYumMode("10.10.10.20:8080")
	if err != nil {
		t.Fatal(err)
	}
	if kind != YumModeHTTP || ep == nil {
		t.Fatalf("got kind=%q ep=%v", kind, ep)
	}
	if ep.Scheme != "http" || ep.Host != "10.10.10.20" || ep.Port != "8080" {
		t.Fatalf("endpoint=%+v", ep)
	}
	if ep.PathRoot != defaultYumHTTPPathRoot {
		t.Fatalf("PathRoot=%q", ep.PathRoot)
	}
	if got := ep.RootURL(); got != "http://10.10.10.20:8080" {
		t.Fatalf("RootURL=%q", got)
	}

	kind, ep, err = ParseYumMode("http://10.10.10.20/oraclelinux")
	if err != nil || kind != YumModeHTTP {
		t.Fatalf("err=%v kind=%q", err, kind)
	}
	if ep.PathRoot != "/oraclelinux" {
		t.Fatalf("PathRoot=%q", ep.PathRoot)
	}
	if got := ep.RootURL(); got != "http://10.10.10.20/oraclelinux" {
		t.Fatalf("RootURL=%q", got)
	}
}

func TestParseYumMode_invalid(t *testing.T) {
	if _, _, err := ParseYumMode("not a mode!!"); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := ParseYumMode("http"); err == nil {
		t.Fatal("expected error for bare http")
	}
	if err := ValidateYumMode("@@@"); err == nil {
		t.Fatal("expected ValidateYumMode error")
	}
}

func TestIsLocalYumMode(t *testing.T) {
	if IsLocalYumMode("") {
		t.Fatal("empty mode should not be local")
	}
	if IsLocalYumMode("none") {
		t.Fatal("none should not be local")
	}
	if !IsLocalYumMode("local") {
		t.Fatal("local should be local mode")
	}
	if !IsLocalYumMode("local-iso") {
		t.Fatal("local-iso alias should be local mode")
	}
	if IsLocalYumMode("10.10.10.20") {
		t.Fatal("IP should not be local")
	}
}

func TestIsHTTPYumMode(t *testing.T) {
	if !IsHTTPYumMode("10.10.10.20") {
		t.Fatal("IP should be http mode")
	}
	if IsHTTPYumMode("local") {
		t.Fatal("local should not be http")
	}
}

func TestBuildYinstallHTTPRepoContent_el8(t *testing.T) {
	ep := &YumHTTPEndpoint{Scheme: "http", Host: "10.10.10.20", PathRoot: defaultYumHTTPPathRoot}
	osInfo := &runner.OSInfo{ID: "ol", VersionID: "8.8", Arch: "aarch64", IsRHEL8: true, PkgManager: "dnf"}
	content, err := BuildYinstallHTTPRepoContent(osInfo, ep)
	if err != nil {
		t.Fatal(err)
	}
	wantBase := "baseurl=http://10.10.10.20/BaseOS/"
	wantApp := "baseurl=http://10.10.10.20/AppStream/"
	if !strings.Contains(content, "["+yumHTTPRepoBaseOS+"]") || !strings.Contains(content, wantBase) {
		t.Fatalf("missing baseos:\n%s", content)
	}
	if !strings.Contains(content, "["+yumHTTPRepoAppStream+"]") || !strings.Contains(content, wantApp) {
		t.Fatalf("missing appstream:\n%s", content)
	}
}

func TestBuildYinstallHTTPRepoContent_el7(t *testing.T) {
	ep := &YumHTTPEndpoint{Scheme: "http", Host: "10.10.10.20", Port: "8080", PathRoot: defaultYumHTTPPathRoot}
	osInfo := &runner.OSInfo{ID: "ol", VersionID: "7.9", Arch: "x86_64", IsRHEL7: true, PkgManager: "yum"}
	content, err := BuildYinstallHTTPRepoContent(osInfo, ep)
	if err != nil {
		t.Fatal(err)
	}
	want := "baseurl=http://10.10.10.20:8080/"
	if !strings.Contains(content, "["+yumHTTPRepoSingle+"]") || !strings.Contains(content, want) {
		t.Fatalf("content:\n%s", content)
	}
}

func TestIsRepoClassInstallError(t *testing.T) {
	if IsRepoClassInstallError("", 1) {
		t.Fatal("empty stderr with non-zero exit is not repo-class by default")
	}
	cases := []string{
		"Error: Failed to download metadata for repo 'yashan-local'",
		"Cannot download repomd.xml",
		"Error: Unable to find a match: lz4",
		"No package lz4 available",
	}
	for _, stderr := range cases {
		if !IsRepoClassInstallError(stderr, 1) {
			t.Errorf("expected repo-class error for %q", stderr)
		}
	}
	if IsRepoClassInstallError("permission denied", 1) {
		t.Fatal("permission denied should not be repo-class")
	}
	if IsRepoClassInstallError("repomd.xml", 0) {
		t.Fatal("exit 0 should never be repo-class")
	}
}

func TestBuildInstallCmd_localMode(t *testing.T) {
	cmd := BuildInstallCmd("dnf", YumModeLocal, "lz4", true)
	if !strings.Contains(cmd, "--enablerepo=local-baseos") {
		t.Fatalf("RHEL8 local cmd missing baseos repo: %s", cmd)
	}
	cmd7 := BuildInstallCmd("yum", "local-iso", "lz4", false)
	if !strings.Contains(cmd7, "--enablerepo=local") {
		t.Fatalf("RHEL7 local cmd missing local repo: %s", cmd7)
	}
	cmdSys := BuildInstallCmd("dnf", "", "lz4", true)
	if strings.Contains(cmdSys, "disablerepo") {
		t.Fatalf("system mode should not disable repos: %s", cmdSys)
	}
}

func TestBuildInstallCmd_httpMode(t *testing.T) {
	cmd := BuildInstallCmd("dnf", YumModeHTTP, "lz4", true)
	if !strings.Contains(cmd, "--enablerepo="+yumHTTPRepoBaseOS) || !strings.Contains(cmd, yumHTTPRepoAppStream) {
		t.Fatalf("http el8 cmd: %s", cmd)
	}
	cmd7 := BuildInstallCmd("yum", "10.10.10.20", "lz4", false)
	if !strings.Contains(cmd7, "--enablerepo="+yumHTTPRepoSingle) {
		t.Fatalf("http el7 cmd: %s", cmd7)
	}
}
