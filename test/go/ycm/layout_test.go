package ycm_test

import (
	"testing"

	ycmsteps "github.com/yinstall/internal/steps/ycm"
)

func TestYCMCleanDeletePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		installDir, ycmHome           string
		homeExplicit, installExplicit bool
		want                          string
	}{
		{"/opt", "/opt/ycm", false, false, "/opt/ycm"},
		{"/opt/ycm_9070", "/opt/ycm_9070/ycm", false, false, "/opt/ycm_9070"},
		{"/data/foo", "/data/foo/ycm", false, true, "/data/foo"},
		{"/custom", "/custom/ycm", true, false, "/custom/ycm"},
	}
	for _, tc := range cases {
		got := ycmsteps.YCMCleanDeletePath(tc.installDir, tc.ycmHome, tc.homeExplicit, tc.installExplicit)
		if got != tc.want {
			t.Errorf("YCMCleanDeletePath(%q, %q, home=%v, install=%v) = %q, want %q",
				tc.installDir, tc.ycmHome, tc.homeExplicit, tc.installExplicit, got, tc.want)
		}
	}
}
