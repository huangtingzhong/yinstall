package mssql

import (
	"fmt"
	"net"
	"strings"

	"github.com/yinstall/internal/runner"
)

// HAInstanceInfo holds SQL Server version metadata for HA partners.
type HAInstanceInfo struct {
	Host                string
	ProductVersion      string
	ProductLevel        string
	Edition             string
	EngineEdition       string
	ProductMajorVersion string
}

// HAInstanceInfoSQL returns a single-row pipe-delimited version query for sqlcmd.
func HAInstanceInfoSQL() string {
	return `SELECT CONCAT(
  RTRIM(CAST(SERVERPROPERTY('ProductVersion') AS NVARCHAR(128))), N'|',
  RTRIM(CAST(SERVERPROPERTY('ProductLevel') AS NVARCHAR(128))), N'|',
  RTRIM(CAST(SERVERPROPERTY('Edition') AS NVARCHAR(128))), N'|',
  CAST(SERVERPROPERTY('EngineEdition') AS NVARCHAR(10)), N'|',
  CAST(SERVERPROPERTY('ProductMajorVersion') AS NVARCHAR(10))
) AS ha_instance_info;`
}

// HAInstanceInfoResultKey stores per-host instance metadata in Results.
func HAInstanceInfoResultKey(hostKey string) string {
	return "ha_instance_" + HAHostKey(hostKey)
}

// ParseHAInstanceInfo parses sqlcmd stdout for HAInstanceInfoSQL.
func ParseHAInstanceInfo(host, stdout string) (HAInstanceInfo, error) {
	host = strings.TrimSpace(host)
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if IsSqlcmdMetaLine(line) || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		info := HAInstanceInfo{
			Host:                host,
			ProductVersion:      strings.TrimSpace(parts[0]),
			ProductLevel:        strings.TrimSpace(parts[1]),
			Edition:             strings.TrimSpace(parts[2]),
			EngineEdition:       strings.TrimSpace(parts[3]),
			ProductMajorVersion: strings.TrimSpace(parts[4]),
		}
		if info.ProductVersion == "" || info.ProductMajorVersion == "" {
			return HAInstanceInfo{}, fmt.Errorf("empty product version in sqlcmd output")
		}
		return info, nil
	}
	return HAInstanceInfo{}, fmt.Errorf("cannot parse HA instance info from sqlcmd output")
}

// HAInstanceInfoFromResults reads stored instance info for a host.
func HAInstanceInfoFromResults(results map[string]interface{}, host string) (HAInstanceInfo, bool) {
	if results == nil {
		return HAInstanceInfo{}, false
	}
	key := HAInstanceInfoResultKey(host)
	if v, ok := results[key].(HAInstanceInfo); ok && strings.TrimSpace(v.ProductVersion) != "" {
		if strings.TrimSpace(v.Host) == "" {
			v.Host = strings.TrimSpace(host)
		}
		return v, true
	}
	// Mirror legacy key.
	mkey := MirrorInstanceInfoResultKey(host)
	if v, ok := results[mkey].(MirrorInstanceInfo); ok && strings.TrimSpace(v.ProductVersion) != "" {
		return mirrorInfoToHA(v), true
	}
	return HAInstanceInfo{}, false
}

func mirrorInfoToHA(m MirrorInstanceInfo) HAInstanceInfo {
	return HAInstanceInfo{
		Host:                m.Host,
		ProductVersion:      m.ProductVersion,
		ProductLevel:        m.ProductLevel,
		Edition:             m.Edition,
		EngineEdition:       m.EngineEdition,
		ProductMajorVersion: m.ProductMajorVersion,
	}
}

func haInfoToMirror(h HAInstanceInfo) MirrorInstanceInfo {
	return MirrorInstanceInfo{
		Host:                h.Host,
		ProductVersion:      h.ProductVersion,
		ProductLevel:        h.ProductLevel,
		Edition:             h.Edition,
		EngineEdition:       h.EngineEdition,
		ProductMajorVersion: h.ProductMajorVersion,
	}
}

// HAReplicaServerNameSQL returns @@SERVERNAME for AG replica identity (dbatools SqlInstance name).
func HAReplicaServerNameSQL() string {
	return `SELECT @@SERVERNAME AS replica_server_name;`
}

// HAReplicaServerNameResultKey stores per-host AG replica server name in Results.
func HAReplicaServerNameResultKey(hostKey string) string {
	return "ha_replica_server_" + HAHostKey(hostKey)
}

// ParseHAReplicaServerName extracts @@SERVERNAME from sqlcmd output.
func ParseHAReplicaServerName(stdout string) (string, error) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) || strings.EqualFold(line, "replica_server_name") {
			continue
		}
		if strings.Contains(line, "|") {
			continue
		}
		return line, nil
	}
	return "", fmt.Errorf("cannot parse @@SERVERNAME from sqlcmd output")
}

// HAReplicaServerNameFromResults reads stored replica server name for a host.
func HAReplicaServerNameFromResults(results map[string]interface{}, host string) (string, bool) {
	if results == nil {
		return "", false
	}
	key := HAReplicaServerNameResultKey(host)
	if v, ok := results[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), true
	}
	return "", false
}

// HAReplicaServerName resolves AG replica server name (@@SERVERNAME) for host param/IP.
// Returns empty when only an IP is known and A-005 has not cached @@SERVERNAME.
func HAReplicaServerName(ctx *runner.StepContext, host string) string {
	if ctx != nil {
		if name, ok := HAReplicaServerNameFromResults(ctx.Results, host); ok && strings.TrimSpace(name) != "" {
			return name
		}
	}
	host = strings.TrimSpace(host)
	if host == "" && ctx != nil && ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	if host == "" {
		return ""
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	return host
}

// CompareHAPartners validates HA partner version/edition requirements (mirror + AG).
func CompareHAPartners(primaryHost string, infos []HAInstanceInfo) error {
	mirrorInfos := make([]MirrorInstanceInfo, len(infos))
	for i, info := range infos {
		mirrorInfos[i] = haInfoToMirror(info)
	}
	return CompareMirrorPartners(primaryHost, mirrorInfos)
}

// MinSQLMajorVersionAG is minimum ProductMajorVersion for Always On (SQL 2016+).
const MinSQLMajorVersionAG = 13

// ValidateAGEdition ensures SQL Server edition supports Always On Availability Groups.
func ValidateAGEdition(info HAInstanceInfo) error {
	switch strings.TrimSpace(info.EngineEdition) {
	case "3", "8": // Enterprise, Developer
		return nil
	default:
		return fmt.Errorf("host %s: Always On requires SQL Server Enterprise (EngineEdition=%s, %s)",
			info.Host, info.EngineEdition, info.Edition)
	}
}

// ValidateHAMajorVersion checks SQL major version meets minimum.
func ValidateHAMajorVersion(info HAInstanceInfo, minMajor int) error {
	major := strings.TrimSpace(info.ProductMajorVersion)
	if major == "" {
		return fmt.Errorf("host %s: empty ProductMajorVersion", info.Host)
	}
	pv, err := ParseProductVersion(info.ProductVersion)
	if err != nil {
		return fmt.Errorf("host %s: %w", info.Host, err)
	}
	if pv.Major < minMajor {
		return fmt.Errorf("host %s: SQL Server major version %d < required %d", info.Host, pv.Major, minMajor)
	}
	return nil
}
