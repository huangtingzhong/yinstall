package file

import (
	"regexp"
	"testing"
)

func TestParseMysqlPackageGlibc(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		base   string
		want   int
		tagged bool
	}{
		{"glibc228", "mysql-8.0.44-linux-glibc2.28-x86_64.tar.xz", 2028, true},
		{"glibc217", "mysql-8.0.44-linux-glibc2.17-x86_64.tar.gz", 2017, true},
		{"el8", "mysql-8.0.44-el8-linux-glibc2.28-x86_64.tar.xz", 2028, true},
		{"legacy", "mysql-8.0.44-linux-x86_64.tar.gz", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, tagged := parseMysqlPackageGlibc(tc.base)
			if tagged != tc.tagged || got != tc.want {
				t.Fatalf("parseMysqlPackageGlibc(%q) = (%d, %v), want (%d, %v)", tc.base, got, tagged, tc.want, tc.tagged)
			}
		})
	}
}

func TestParseGlibcProbeOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"glibc 2.28\n", 2028, true},
		{"ldd (GNU libc) 2.17\nCopyright ...", 2017, true},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseGlibcProbeOutput(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseGlibcProbeOutput(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSelectBestMysqlLinuxBinaryFromHost(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`mysql-(\d+\.\d+\.\d+)-(?:el\d+-)?linux(?:-glibc[\d.]+)?-(?:x86_64|x86-64)\.(?:tar\.gz|tar\.xz)`)
	files := []string{
		"/soft/mysql-8.0.44-linux-glibc2.28-x86_64.tar.xz",
		"/soft/mysql-8.0.44-linux-glibc2.17-x86_64.tar.gz",
		"/soft/mysql-8.0.43-linux-glibc2.28-x86_64.tar.xz",
	}

	selected, err := selectBestMysqlLinuxBinaryFromHost(files, re, 2017, true)
	if err != nil || selected != files[1] {
		t.Fatalf("host 2.17: got %q err=%v want %q", selected, err, files[1])
	}

	selected, err = selectBestMysqlLinuxBinaryFromHost(files, re, 2028, true)
	if err != nil || selected != files[0] {
		t.Fatalf("host 2.28: got %q err=%v want %q", selected, err, files[0])
	}

	_, err = selectBestMysqlLinuxBinaryFromHost([]string{files[0]}, re, 2017, true)
	if err == nil {
		t.Fatal("expected incompatible error")
	}

	legacy := []string{"/soft/mysql-8.0.44-linux-x86_64.tar.gz", files[1]}
	selected, err = selectBestMysqlLinuxBinaryFromHost(legacy, re, 2017, true)
	if err != nil || selected != files[1] {
		t.Fatalf("prefer tagged over legacy: got %q err=%v want %q", selected, err, files[1])
	}
}
