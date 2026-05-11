package os

import "testing"

func TestIsSafeUnixRmRfPath(t *testing.T) {
	cases := []struct {
		p    string
		safe bool
	}{
		{"/", false},
		{"/data", false},
		{"/data/yashan", true},
		{"/home/yashan/install", true},
		{"/opt/ymp", true},
		{"/usr/local/foo", true},
		{"/usr/bin", false},
		{"/etc/foo", false},
		{"/dev/sda1", false},
		{"relative/path", false},
		{"/data/yashan/../etc/passwd", false},
		{"/tmp/a/b", true},
	}
	for _, tc := range cases {
		got := IsSafeUnixRmRfPath(tc.p)
		if got != tc.safe {
			t.Errorf("IsSafeUnixRmRfPath(%q) = %v, want %v", tc.p, got, tc.safe)
		}
	}
}

func TestIsSafeUnixBlockDevicePath(t *testing.T) {
	cases := []struct {
		p    string
		safe bool
	}{
		{"/dev/sda", true},
		{"/dev/nvme0n1", true},
		{"/dev/mapper/mpatha", true},
		{"/dev/disk/by-id/wwn-0x123", true},
		{"/dev", false},
		{"/dev/../etc/passwd", false},
		{"/data/disk.img", false},
		{"/dev/sda x", false},
	}
	for _, tc := range cases {
		got := IsSafeUnixBlockDevicePath(tc.p)
		if got != tc.safe {
			t.Errorf("IsSafeUnixBlockDevicePath(%q) = %v, want %v", tc.p, got, tc.safe)
		}
	}
}
