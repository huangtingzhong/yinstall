package cli_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/yinstall/internal/cli"
)

func TestDefaultLocalSoftwareDirs_includesCwdAndHome(t *testing.T) {
	t.Parallel()
	dirs := cli.DefaultLocalSoftwareDirs()
	wantHas := []string{"./software", "./pkg", "."}
	home, err := os.UserHomeDir()
	if err == nil {
		wantHas = append(wantHas, home)
	}
	for _, w := range wantHas {
		found := false
		for _, d := range dirs {
			if d == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("DefaultLocalSoftwareDirs missing %q; got %v", w, dirs)
		}
	}
	// ./software should appear before "." and home
	idxSoft, idxDot, idxHome := -1, -1, -1
	for i, d := range dirs {
		switch d {
		case "./software":
			idxSoft = i
		case ".":
			idxDot = i
		case home:
			idxHome = i
		}
	}
	if idxSoft < 0 || idxDot < 0 || idxSoft > idxDot {
		t.Fatalf("expected ./software before ., got %v", dirs)
	}
	if home != "" && idxHome >= 0 && idxDot > idxHome {
		t.Fatalf("expected . before $HOME, got %v", dirs)
	}
	_ = runtime.GOOS
}
