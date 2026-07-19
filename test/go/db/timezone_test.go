package db_test

import (
	"testing"

	commonos "github.com/yinstall/internal/common/os"
)

func TestResolveOSTimezone(t *testing.T) {
	if got := commonos.ResolveOSTimezone(""); got != "Asia/Shanghai" {
		t.Fatalf("empty: got %q", got)
	}
	if got := commonos.ResolveOSTimezone("  Europe/Berlin  "); got != "Europe/Berlin" {
		t.Fatalf("trim: got %q", got)
	}
}

func TestIsYashanTimeZoneOffset(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"+08:00", true},
		{"-05:30", true},
		{"+15:59", true},
		{"+16:00", false},
		{"Asia/Shanghai", false},
	}
	for _, c := range cases {
		if got := commonos.IsYashanTimeZoneOffset(c.in); got != c.want {
			t.Fatalf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestIANAToYashanTimeZone(t *testing.T) {
	got, err := commonos.IANAToYashanTimeZone("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got != "+08:00" {
		t.Fatalf("Asia/Shanghai: got %q", got)
	}
}

func TestParseDBTimeZoneInput(t *testing.T) {
	got, err := commonos.ParseDBTimeZoneInput("+09:00")
	if err != nil || got != "+09:00" {
		t.Fatalf("offset: got %q err=%v", got, err)
	}
	got, err = commonos.ParseDBTimeZoneInput("Asia/Tokyo")
	if err != nil || got != "+09:00" {
		t.Fatalf("iana: got %q err=%v", got, err)
	}
}
