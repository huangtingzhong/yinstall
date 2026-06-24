package mssql

import (
	"fmt"
	"strings"
)

// AGReplicaStatus is one AG replica row for terminal summary.
type AGReplicaStatus struct {
	ServerName string
	Role       string
	Connected  string
	SyncHealth string
}

// AGDatabaseStatus is one AG database sync row.
type AGDatabaseStatus struct {
	Name       string
	SyncState  string
	SyncHealth string
}

// AGListenerStatus is one AG listener row.
type AGListenerStatus struct {
	DNSName string
	Port    string
	IP      string
}

// WSFCResourceLine is one WSFC cluster/group/resource row.
type WSFCResourceLine struct {
	Kind       string // CLUSTER, GROUP, RESOURCE
	OwnerGroup string
	Name       string
	Type       string
	State      string
}

// MirrorDatabaseStatus is one database mirroring row.
type MirrorDatabaseStatus struct {
	Name        string
	DBState     string
	State       string
	Role        string
	Partner     string
	SafetyLevel string
}

// MirrorEndpointStatus is mirror TCP endpoint row.
type MirrorEndpointStatus struct {
	Name  string
	Port  string
	State string
}

// AGStatusReplicaSQL returns pipe-delimited replica status rows.
func AGStatusReplicaSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`
SELECT CONCAT(
  r.replica_server_name, N'|',
  rs.role_desc, N'|',
  rs.connected_state_desc, N'|',
  rs.synchronization_health_desc
)
FROM sys.availability_replicas r
INNER JOIN sys.dm_hadr_availability_replica_states rs ON r.replica_id = rs.replica_id
INNER JOIN sys.availability_groups g ON g.group_id = r.group_id
WHERE g.name = N'%s'
ORDER BY r.replica_server_name;`, agName)
}

// AGStatusDatabaseSQL returns pipe-delimited AG database sync rows (primary replica view).
func AGStatusDatabaseSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`
SELECT CONCAT(
  d.name, N'|',
  drs.synchronization_state_desc, N'|',
  drs.synchronization_health_desc
)
FROM sys.dm_hadr_database_replica_states drs
INNER JOIN sys.databases d ON d.database_id = drs.database_id
INNER JOIN sys.availability_groups g ON g.group_id = drs.group_id
INNER JOIN sys.dm_hadr_availability_replica_states rs ON rs.replica_id = drs.replica_id AND rs.role_desc = N'PRIMARY'
WHERE g.name = N'%s'
ORDER BY d.name;`, agName)
}

// AGStatusListenerSQL returns pipe-delimited AG listener rows.
func AGStatusListenerSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`
SELECT CONCAT(
  l.dns_name, N'|',
  CAST(l.port AS NVARCHAR(10)), N'|',
  ISNULL(lip.ip_address, N'')
)
FROM sys.availability_groups g
INNER JOIN sys.availability_group_listeners l ON l.group_id = g.group_id
LEFT JOIN sys.availability_group_listener_ip_addresses lip ON lip.listener_id = l.listener_id
WHERE g.name = N'%s'
ORDER BY l.dns_name;`, agName)
}

// MirrorMirroredDatabaseStatusSQL returns pipe-delimited status for mirrored databases only.
func MirrorMirroredDatabaseStatusSQL() string {
	return `
SELECT CONCAT(
  CAST(d.name AS NVARCHAR(128)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(d.state_desc AS NVARCHAR(64)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(ISNULL(CAST(m.mirroring_state AS NVARCHAR(10)), N'') AS NVARCHAR(10)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(ISNULL(m.mirroring_role_desc, N'') AS NVARCHAR(64)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(ISNULL(m.mirroring_partner_name, N'') AS NVARCHAR(256)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(ISNULL(m.mirroring_safety_level_desc, N'') AS NVARCHAR(64)) COLLATE Latin1_General_CI_AS
)
FROM sys.databases d
INNER JOIN sys.database_mirroring m ON m.database_id = d.database_id
WHERE m.mirroring_state IS NOT NULL
ORDER BY d.name;`
}

// MirrorAllDatabaseStatusSQL is an alias kept for callers; returns mirrored databases only.
func MirrorAllDatabaseStatusSQL() string {
	return MirrorMirroredDatabaseStatusSQL()
}

// MirrorEndpointStatusSQL returns mirror endpoint name, port, and state.
func MirrorEndpointStatusSQL() string {
	return `
SELECT CONCAT(
  CAST(e.name AS NVARCHAR(128)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(CAST(t.port AS NVARCHAR(10)) AS NVARCHAR(10)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(CASE e.state WHEN 0 THEN N'STOPPED' WHEN 1 THEN N'STARTED' ELSE CAST(e.state AS NVARCHAR(10)) END AS NVARCHAR(32)) COLLATE Latin1_General_CI_AS
)
FROM sys.endpoints e
INNER JOIN sys.tcp_endpoints t ON e.endpoint_id = t.endpoint_id
WHERE e.name = N'Mirror_endpoint';`
}

// WSFCClusterStatusPS returns cluster, AG group, and AG resources as pipe-delimited lines.
func WSFCClusterStatusPS(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`$ag='%s'
$c = Get-Cluster -ErrorAction SilentlyContinue
if ($c) { "CLUSTER||$($c.Name)|Up" }
$g = Get-ClusterGroup -Name $ag -ErrorAction SilentlyContinue
if ($g) { "GROUP||$($g.Name)|$($g.State)" }
Get-ClusterResource -ErrorAction SilentlyContinue | Where-Object { $_.OwnerGroup -and $_.OwnerGroup.Name -eq $ag } | ForEach-Object {
  "RESOURCE|$ag|$($_.Name)|$($_.ResourceType)|$($_.State)"
}
exit 0`, agName)
}

// MirrorStateLabel maps mirroring_state numeric code to label.
func MirrorStateLabel(state string) string {
	switch strings.TrimSpace(state) {
	case "0":
		return "SUSPENDED"
	case "1":
		return "SUSPENDED (PENDING)"
	case "2":
		return "DISCONNECTED"
	case "3":
		return "SYNCHRONIZING"
	case "4":
		return "SYNCHRONIZED"
	case "":
		return "NOT MIRRORED"
	default:
		return state
	}
}

func parsePipeRows(stdout string, minCols int, parse func([]string)) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < minCols {
			continue
		}
		parse(parts)
	}
}

// ParseAGReplicaStatus parses AGStatusReplicaSQL output.
func ParseAGReplicaStatus(stdout string) []AGReplicaStatus {
	var out []AGReplicaStatus
	parsePipeRows(stdout, 4, func(p []string) {
		out = append(out, AGReplicaStatus{
			ServerName: strings.TrimSpace(p[0]),
			Role:       strings.TrimSpace(p[1]),
			Connected:  strings.TrimSpace(p[2]),
			SyncHealth: strings.TrimSpace(p[3]),
		})
	})
	return out
}

// ParseAGDatabaseStatus parses AGStatusDatabaseSQL output.
func ParseAGDatabaseStatus(stdout string) []AGDatabaseStatus {
	var out []AGDatabaseStatus
	parsePipeRows(stdout, 3, func(p []string) {
		out = append(out, AGDatabaseStatus{
			Name:       strings.TrimSpace(p[0]),
			SyncState:  strings.TrimSpace(p[1]),
			SyncHealth: strings.TrimSpace(p[2]),
		})
	})
	return out
}

// ParseAGListenerStatus parses AGStatusListenerSQL output.
func ParseAGListenerStatus(stdout string) []AGListenerStatus {
	var out []AGListenerStatus
	parsePipeRows(stdout, 3, func(p []string) {
		out = append(out, AGListenerStatus{
			DNSName: strings.TrimSpace(p[0]),
			Port:    strings.TrimSpace(p[1]),
			IP:      strings.TrimSpace(p[2]),
		})
	})
	return out
}

// ParseMirrorDatabaseStatus parses MirrorMirroredDatabaseStatusSQL output.
func ParseMirrorDatabaseStatus(stdout string) []MirrorDatabaseStatus {
	var out []MirrorDatabaseStatus
	parsePipeRows(stdout, 6, func(p []string) {
		out = append(out, MirrorDatabaseStatus{
			Name:        strings.TrimSpace(p[0]),
			DBState:     strings.TrimSpace(p[1]),
			State:       strings.TrimSpace(p[2]),
			Role:        strings.TrimSpace(p[3]),
			Partner:     strings.TrimSpace(p[4]),
			SafetyLevel: strings.TrimSpace(p[5]),
		})
	})
	return out
}

// ParseMirrorEndpointStatus parses MirrorEndpointStatusSQL output.
func ParseMirrorEndpointStatus(stdout string) []MirrorEndpointStatus {
	var out []MirrorEndpointStatus
	parsePipeRows(stdout, 3, func(p []string) {
		out = append(out, MirrorEndpointStatus{
			Name:  strings.TrimSpace(p[0]),
			Port:  strings.TrimSpace(p[1]),
			State: strings.TrimSpace(p[2]),
		})
	})
	return out
}

// ParseWSFCResourceLines parses WSFCClusterStatusPS output.
func ParseWSFCResourceLines(stdout string) []WSFCResourceLine {
	var out []WSFCResourceLine
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		kind := strings.TrimSpace(parts[0])
		switch kind {
		case "CLUSTER":
			if len(parts) < 4 {
				continue
			}
			out = append(out, WSFCResourceLine{
				Kind:  kind,
				Name:  strings.TrimSpace(parts[2]),
				State: strings.TrimSpace(parts[3]),
			})
		case "GROUP":
			if len(parts) < 4 {
				continue
			}
			out = append(out, WSFCResourceLine{
				Kind:  kind,
				Name:  strings.TrimSpace(parts[2]),
				State: strings.TrimSpace(parts[3]),
			})
		case "RESOURCE":
			if len(parts) < 5 {
				continue
			}
			out = append(out, WSFCResourceLine{
				Kind:       kind,
				OwnerGroup: strings.TrimSpace(parts[1]),
				Name:       strings.TrimSpace(parts[2]),
				Type:       strings.TrimSpace(parts[3]),
				State:      strings.TrimSpace(parts[4]),
			})
		}
	}
	return out
}
