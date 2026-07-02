package file

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var yashanDBPackageVersionRE = regexp.MustCompile(`(?i)yashandb-(\d+)\.(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// ParseYashanDBPackageVersion extracts the numeric version from a yashandb-*.tar.gz path or basename.
// Example: yashandb-23.5.3.2-linux-aarch64.tar.gz -> [23, 5, 3, 2].
func ParseYashanDBPackageVersion(packagePath string) ([]int, error) {
	base := filepath.Base(strings.TrimSpace(packagePath))
	if base == "" || base == "." {
		return nil, fmt.Errorf("empty package path")
	}
	m := yashanDBPackageVersionRE.FindStringSubmatch(base)
	if m == nil {
		return nil, fmt.Errorf("cannot parse YashanDB version from package name %q (expected yashandb-M.m.p.r-...)", base)
	}
	out := make([]int, 0, 4)
	for i := 1; i <= 4; i++ {
		if m[i] == "" {
			break
		}
		v, err := strconv.Atoi(m[i])
		if err != nil {
			return nil, fmt.Errorf("invalid version segment in package name %q", base)
		}
		out = append(out, v)
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("invalid YashanDB version in package name %q", base)
	}
	return out, nil
}

// FormatYashanDBVersion renders a version slice for logs and errors.
func FormatYashanDBVersion(ver []int) string {
	if len(ver) == 0 {
		return "unknown"
	}
	parts := make([]string, len(ver))
	for i, v := range ver {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ".")
}

// VersionAtLeast reports whether ver is >= min (missing segments treated as 0).
func VersionAtLeast(ver []int, min ...int) bool {
	for i := 0; i < len(min); i++ {
		vi := 0
		if i < len(ver) {
			vi = ver[i]
		}
		if vi > min[i] {
			return true
		}
		if vi < min[i] {
			return false
		}
	}
	return true
}
