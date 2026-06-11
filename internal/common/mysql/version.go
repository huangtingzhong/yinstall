package mysql

import (
	"fmt"
	"strconv"
	"strings"
)

// Version holds parsed MySQL semver components.
type Version struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// ParseMySQLVersion parses strings like 8.0.46-log or 5.7.44.
func ParseMySQLVersion(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Version{}, fmt.Errorf("empty mysql version")
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return Version{}, fmt.Errorf("invalid mysql version %q", raw)
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid mysql version major %q", raw)
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid mysql version minor %q", raw)
	}
	patch := 0
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return Version{Major: maj, Minor: min, Patch: patch, Raw: strings.TrimSpace(raw)}, nil
}

// CompareMySQLVersion returns -1 if a<b, 0 if equal, 1 if a>b.
func CompareMySQLVersion(a, b string) (int, error) {
	va, err := ParseMySQLVersion(a)
	if err != nil {
		return 0, err
	}
	vb, err := ParseMySQLVersion(b)
	if err != nil {
		return 0, err
	}
	if va.Major != vb.Major {
		if va.Major < vb.Major {
			return -1, nil
		}
		return 1, nil
	}
	if va.Minor != vb.Minor {
		if va.Minor < vb.Minor {
			return -1, nil
		}
		return 1, nil
	}
	if va.Patch < vb.Patch {
		return -1, nil
	}
	if va.Patch > vb.Patch {
		return 1, nil
	}
	return 0, nil
}

// ReplicaVersionOK reports replica >= primary.
func ReplicaVersionOK(replica, primary string) (bool, error) {
	cmp, err := CompareMySQLVersion(replica, primary)
	if err != nil {
		return false, err
	}
	return cmp >= 0, nil
}

// UsesReplicationSourceSyntax is true for MySQL 8.0.26+.
func UsesReplicationSourceSyntax(version string) bool {
	v, err := ParseMySQLVersion(version)
	if err != nil {
		return true
	}
	if v.Major > 8 {
		return true
	}
	if v.Major < 8 {
		return false
	}
	if v.Minor > 0 {
		return true
	}
	return v.Patch >= 26
}
