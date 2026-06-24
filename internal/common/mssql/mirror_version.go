package mssql

import (
	"fmt"
	"strconv"
	"strings"
)

// MirrorInstanceInfo holds SQL Server version metadata for mirroring partners.
type MirrorInstanceInfo struct {
	Host                string
	ProductVersion      string
	ProductLevel        string
	Edition             string
	EngineEdition       string
	ProductMajorVersion string
}

// MirrorInstanceInfoSQL returns a single-row pipe-delimited version query for sqlcmd.
func MirrorInstanceInfoSQL() string {
	return HAInstanceInfoSQL()
}

// MirrorInstanceInfoResultKey stores per-host mirror instance metadata in Results.
func MirrorInstanceInfoResultKey(hostKey string) string {
	return "mirror_instance_" + MirrorHostKey(hostKey)
}

// ParseMirrorInstanceInfo parses sqlcmd stdout for MirrorInstanceInfoSQL.
func ParseMirrorInstanceInfo(host, stdout string) (MirrorInstanceInfo, error) {
	ha, err := ParseHAInstanceInfo(host, stdout)
	if err != nil {
		return MirrorInstanceInfo{}, err
	}
	return haInfoToMirror(ha), nil
}

// MirrorInstanceInfoFromResults reads stored instance info for a host.
func MirrorInstanceInfoFromResults(results map[string]interface{}, host string) (MirrorInstanceInfo, bool) {
	if results == nil {
		return MirrorInstanceInfo{}, false
	}
	key := MirrorInstanceInfoResultKey(host)
	if v, ok := results[key].(MirrorInstanceInfo); ok && strings.TrimSpace(v.ProductVersion) != "" {
		if strings.TrimSpace(v.Host) == "" {
			v.Host = strings.TrimSpace(host)
		}
		return v, true
	}
	return MirrorInstanceInfo{}, false
}

// ProductVersion represents a SQL Server ProductVersion (major.minor.build.revision).
type ProductVersion struct {
	Major, Minor, Build, Revision int
}

// ParseProductVersion parses SERVERPROPERTY('ProductVersion') values.
func ParseProductVersion(raw string) (ProductVersion, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ".")
	if len(parts) < 3 {
		return ProductVersion{}, fmt.Errorf("invalid product version %q", raw)
	}
	parse := func(s string) (int, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, fmt.Errorf("empty version segment in %q", raw)
		}
		return strconv.Atoi(s)
	}
	major, err := parse(parts[0])
	if err != nil {
		return ProductVersion{}, err
	}
	minor, err := parse(parts[1])
	if err != nil {
		return ProductVersion{}, err
	}
	build, err := parse(parts[2])
	if err != nil {
		return ProductVersion{}, err
	}
	revision := 0
	if len(parts) > 3 {
		revision, err = parse(parts[3])
		if err != nil {
			return ProductVersion{}, err
		}
	}
	return ProductVersion{Major: major, Minor: minor, Build: build, Revision: revision}, nil
}

// Compare returns -1 if v < other, 0 if equal, 1 if v > other.
func (v ProductVersion) Compare(other ProductVersion) int {
	type pair struct{ a, b int }
	for _, p := range []pair{
		{v.Major, other.Major},
		{v.Minor, other.Minor},
		{v.Build, other.Build},
		{v.Revision, other.Revision},
	} {
		switch {
		case p.a < p.b:
			return -1
		case p.a > p.b:
			return 1
		}
	}
	return 0
}

// CompareMirrorPartners validates database mirroring version/edition requirements.
func CompareMirrorPartners(primaryHost string, infos []MirrorInstanceInfo) error {
	if len(infos) < 2 {
		return fmt.Errorf("mirror version check requires 2+ SQL Server instances")
	}
	primaryHost = strings.TrimSpace(primaryHost)
	var primary *MirrorInstanceInfo
	for i := range infos {
		if strings.EqualFold(infos[i].Host, primaryHost) {
			primary = &infos[i]
			break
		}
	}
	if primary == nil {
		return fmt.Errorf("primary host %s not found in mirror instance info", primaryHost)
	}
	for _, replica := range infos {
		if strings.EqualFold(replica.Host, primaryHost) {
			continue
		}
		if err := compareMirrorInstancePair(*primary, replica); err != nil {
			return err
		}
	}
	return nil
}

func compareMirrorInstancePair(primary, replica MirrorInstanceInfo) error {
	if primary.ProductMajorVersion != replica.ProductMajorVersion {
		return fmt.Errorf(
			"mirror partner %s ProductMajorVersion %s != primary %s (database mirroring requires same SQL Server major version)",
			replica.Host, replica.ProductMajorVersion, primary.ProductMajorVersion,
		)
	}
	if primary.EngineEdition != replica.EngineEdition {
		return fmt.Errorf(
			"mirror partner %s EngineEdition %s (%s) != primary %s (%s) (database mirroring requires same SQL Server edition)",
			replica.Host, replica.EngineEdition, replica.Edition, primary.EngineEdition, primary.Edition,
		)
	}
	primaryPV, err := ParseProductVersion(primary.ProductVersion)
	if err != nil {
		return fmt.Errorf("primary %s: %w", primary.Host, err)
	}
	replicaPV, err := ParseProductVersion(replica.ProductVersion)
	if err != nil {
		return fmt.Errorf("mirror partner %s: %w", replica.Host, err)
	}
	switch replicaPV.Compare(primaryPV) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf(
			"mirror partner %s ProductVersion %s is newer than primary %s (principal must be same or higher build for database mirroring)",
			replica.Host, replica.ProductVersion, primary.ProductVersion,
		)
	default:
		return fmt.Errorf(
			"mirror partner %s ProductVersion %s is older than primary %s (database mirroring requires matching SQL Server version/build)",
			replica.Host, replica.ProductVersion, primary.ProductVersion,
		)
	}
}
