package mssql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// AGReplicaSpec describes one AG replica for CREATE AVAILABILITY GROUP.
type AGReplicaSpec struct {
	ServerName        string // FOR REPLICA ON (@@SERVERNAME / dbatools SqlInstance)
	EndpointHost      string // ENDPOINT_URL host (IP or DNS)
	EndpointPort      int
	AvailabilityMode  string // SYNCHRONOUS_COMMIT or ASYNCHRONOUS_COMMIT
	FailoverMode      string // AUTOMATIC or MANUAL
	SeedingMode       string // AUTOMATIC or MANUAL
	SecondaryRoleMode string // NO or ALL
}

// DefaultAGReplicaSpec returns synchronous automatic failover replica defaults.
func DefaultAGReplicaSpec(serverName, endpointHost string, port int, isPrimary bool) AGReplicaSpec {
	spec := AGReplicaSpec{
		ServerName:        strings.TrimSpace(serverName),
		EndpointHost:      strings.TrimSpace(endpointHost),
		EndpointPort:      port,
		AvailabilityMode:  "SYNCHRONOUS_COMMIT",
		FailoverMode:      "MANUAL",
		SeedingMode:       "MANUAL",
		SecondaryRoleMode: "NO",
	}
	if isPrimary {
		spec.FailoverMode = "AUTOMATIC"
		spec.SecondaryRoleMode = "ALL"
	}
	return spec
}

// AGReplicaSpecForHost builds replica spec with per-host SQL/HADR endpoint ports.
func AGReplicaSpecForHost(ctx *runner.StepContext, host string, isPrimary bool) AGReplicaSpec {
	return DefaultAGReplicaSpec(
		HAReplicaServerName(ctx, host),
		host,
		HAEndpointPortForHost(ctx, host),
		isPrimary,
	)
}

// formatAGReplicaClause renders one "N'name' WITH (...)" clause shared by
// CREATE AVAILABILITY GROUP ... FOR REPLICA ON and ALTER ... ADD REPLICA ON.
func formatAGReplicaClause(r AGReplicaSpec, sqlMajor int) string {
	serverName := strings.TrimSpace(r.ServerName)
	endpointHost := strings.TrimSpace(r.EndpointHost)
	if endpointHost == "" {
		endpointHost = serverName
	}
	port := r.EndpointPort
	if port <= 0 {
		port = 5022
	}
	url := fmt.Sprintf("TCP://%s:%d", endpointHost, port)
	url = strings.ReplaceAll(url, "'", "''")
	serverName = strings.ReplaceAll(serverName, "'", "''")
	failover := strings.TrimSpace(r.FailoverMode)
	if failover == "" {
		failover = "MANUAL"
	}
	avail := strings.TrimSpace(r.AvailabilityMode)
	if avail == "" {
		avail = "SYNCHRONOUS_COMMIT"
	}
	secRole := strings.TrimSpace(r.SecondaryRoleMode)
	if secRole == "" {
		secRole = "NO"
	}
	seedClause := ""
	if sqlMajor >= 14 {
		seed := strings.TrimSpace(r.SeedingMode)
		if seed == "" {
			seed = "MANUAL"
		}
		seedClause = fmt.Sprintf(",\n    SEEDING_MODE = %s", seed)
	}
	return fmt.Sprintf(`
  N'%s' WITH (
    ENDPOINT_URL = N'%s',
    AVAILABILITY_MODE = %s,
    FAILOVER_MODE = %s%s,
    SECONDARY_ROLE(ALLOW_CONNECTIONS = %s)
  )`, serverName, url, avail, failover, seedClause, secRole)
}

// CreateAvailabilityGroupSQL builds CREATE AVAILABILITY GROUP with FOR REPLICA ON.
// sqlMajor: ProductMajorVersion; omit CLUSTER_TYPE / replica SEEDING_MODE on SQL 2016.
func CreateAvailabilityGroupSQL(agName string, replicas []AGReplicaSpec, sqlMajor int) string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	var parts []string
	for _, r := range replicas {
		parts = append(parts, formatAGReplicaClause(r, sqlMajor))
	}
	clusterClause := ""
	if sqlMajor >= 14 {
		clusterClause = "WITH (CLUSTER_TYPE = WSFC)\n"
	}
	return fmt.Sprintf(`CREATE AVAILABILITY GROUP [%s]
%sFOR REPLICA ON %s;`, agName, clusterClause, strings.Join(parts, ","))
}

// AlterAvailabilityGroupAddReplicaSQL builds one ALTER AVAILABILITY GROUP ... ADD
// REPLICA ON statement per replica, for adding new secondaries to an existing AG.
func AlterAvailabilityGroupAddReplicaSQL(agName string, replicas []AGReplicaSpec, sqlMajor int) []string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	var stmts []string
	for _, r := range replicas {
		stmts = append(stmts, fmt.Sprintf("ALTER AVAILABILITY GROUP [%s]\nADD REPLICA ON %s;", agName, formatAGReplicaClause(r, sqlMajor)))
	}
	return stmts
}

// RemoveReplicaFromAGSQL builds ALTER AVAILABILITY GROUP ... REMOVE REPLICA
// for a single secondary. Call on the primary; SQL Server will propagate.
// serverName is replica_server_name (typically the Windows computer name).
func RemoveReplicaFromAGSQL(agName, serverName string) string {
	agSQL := strings.ReplaceAll(agName, "'", "''")
	agBracket := strings.ReplaceAll(agName, "]", "]]")
	serverSQL := strings.ReplaceAll(serverName, "'", "''")
	return fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.availability_replicas r INNER JOIN sys.availability_groups g ON g.group_id = r.group_id WHERE g.name = N'%s' AND r.replica_server_name = N'%s')
BEGIN
  ALTER AVAILABILITY GROUP [%s] REMOVE REPLICA ON N'%s';
END`, agSQL, serverSQL, agBracket, serverSQL)
}

// AGReplicaServerNamesSQL lists replica_server_name values for a named AG (primary-side query).
func AGReplicaServerNamesSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`SELECT r.replica_server_name FROM sys.availability_replicas r INNER JOIN sys.availability_groups g ON g.group_id = r.group_id WHERE g.name = N'%s';`, agName)
}

// ParseAGReplicaServerNames extracts replica server names (one per line) from sqlcmd stdout.
func ParseAGReplicaServerNames(stdout string) []string {
	var names []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) || strings.EqualFold(line, "replica_server_name") {
			continue
		}
		names = append(names, line)
	}
	return names
}

// LocalReplicaJoinedAGSQL returns 1 when the local instance is already a member
// of the named AG (is_local=1 in dm_hadr_availability_replica_states), else 0.
func LocalReplicaJoinedAGSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (SELECT 1 FROM sys.dm_hadr_availability_replica_states rs INNER JOIN sys.availability_groups g ON g.group_id = rs.group_id WHERE g.name = N'%s' AND rs.is_local = 1) THEN 1 ELSE 0 END;`, agName)
}

// JoinAvailabilityGroupSQL builds secondary join statements.
func JoinAvailabilityGroupSQL(agName string, sqlMajor int) []string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	join := fmt.Sprintf("ALTER AVAILABILITY GROUP [%s] JOIN;", agName)
	if sqlMajor >= 14 {
		join = fmt.Sprintf("ALTER AVAILABILITY GROUP [%s] JOIN WITH (CLUSTER_TYPE = WSFC);", agName)
	}
	return []string{
		join,
		fmt.Sprintf("ALTER AVAILABILITY GROUP [%s] GRANT CREATE ANY DATABASE;", agName),
	}
}

// CreateListenerSQL builds ADD LISTENER with static IP.
func CreateListenerSQL(agName, listener string, ip string, port int) string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	listener = strings.ReplaceAll(listener, "'", "''")
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "0.0.0.0"
	}
	ip = strings.ReplaceAll(ip, "'", "''")
	return fmt.Sprintf("ALTER AVAILABILITY GROUP [%s] ADD LISTENER N'%s' (WITH IP ((N'%s', N'255.255.255.0')), PORT=%d);",
		agName, listener, ip, port)
}

// AGListenerEnabled reports whether MSH-005 should create an AG listener resource.
func AGListenerEnabled(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	return strings.TrimSpace(ctx.GetParamString("mssql_ag_listener_ip", "")) != ""
}

// ResolveListenerIP returns --mssql-ag-listener-ip when listener is enabled.
func ResolveListenerIP(ctx *runner.StepContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("ResolveListenerIP: nil context")
	}
	ip := strings.TrimSpace(ctx.GetParamString("mssql_ag_listener_ip", ""))
	if ip == "" {
		return "", fmt.Errorf("AG listener not configured")
	}
	return ip, nil
}

// AddDatabaseToAGSQL adds database to AG on primary (manual seeding path).
func AddDatabaseToAGSQL(agName, dbName string) string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	return fmt.Sprintf("ALTER AVAILABILITY GROUP [%s] ADD DATABASE [%s];", agName, dbName)
}

// JoinDatabaseToAGSQL joins a restored (NORECOVERY) database to AG on secondary.
// Must run AFTER AddDatabaseToAGSQL on primary and AFTER backup-restore on secondary.
// Without this, secondary database stays in RESTORING state and never participates
// in AG synchronization (sys.dm_hadr_database_replica_states shows 0 local rows).
func JoinDatabaseToAGSQL(agName, dbName string) string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	return fmt.Sprintf("ALTER DATABASE [%s] SET HADR AVAILABILITY GROUP = [%s];", dbName, agName)
}

// AddDatabaseToAGAutomaticSQL adds database with automatic seeding (SQL 2016 SP1+ / 2017+).
func AddDatabaseToAGAutomaticSQL(agName, dbName string, sqlMajor int) string {
	agName = strings.ReplaceAll(agName, "]", "]]")
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	if sqlMajor < 13 {
		return AddDatabaseToAGSQL(agName, dbName)
	}
	return fmt.Sprintf("ALTER AVAILABILITY GROUP [%s] ADD DATABASE [%s] WITH (SEEDING_MODE = AUTOMATIC);", agName, dbName)
}

// SQLMajorFromContext reads ProductMajorVersion from HA instance info in Results.
func SQLMajorFromContext(ctx *runner.StepContext) int {
	if ctx == nil || ctx.Executor == nil {
		return 14
	}
	host := HAHostKey(ctx.Executor.Host())
	if info, ok := HAInstanceInfoFromResults(ctx.Results, host); ok {
		if pv, err := ParseProductVersion(info.ProductVersion); err == nil && pv.Major > 0 {
			return pv.Major
		}
		if major, err := strconv.Atoi(strings.TrimSpace(info.ProductMajorVersion)); err == nil && major > 0 {
			return major
		}
	}
	return 14
}

// VerifyAvailabilityGroupDetailedSQL returns replica sync state.
func VerifyAvailabilityGroupDetailedSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`
SELECT r.replica_server_name, rs.role_desc, rs.connected_state_desc, rs.synchronization_health_desc
FROM sys.availability_replicas r
INNER JOIN sys.dm_hadr_availability_replica_states rs ON r.replica_id = rs.replica_id
INNER JOIN sys.availability_groups g ON g.group_id = r.group_id
WHERE g.name = N'%s';`, agName)
}

// AGDatabaseListSQL lists databases in the named availability group (primary).
func AGDatabaseListSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`SELECT d.name
FROM sys.availability_databases_cluster adc
INNER JOIN sys.databases d ON adc.group_database_id = d.group_database_id
INNER JOIN sys.availability_groups g ON g.group_id = adc.group_id
WHERE g.name = N'%s'
ORDER BY d.name;`, agName)
}

// VerifyAGDatabaseSyncSQL returns database replica sync states.
func VerifyAGDatabaseSyncSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`
SELECT d.name, drs.synchronization_state_desc, drs.synchronization_health_desc
FROM sys.dm_hadr_database_replica_states drs
INNER JOIN sys.databases d ON d.database_id = drs.database_id
INNER JOIN sys.availability_groups g ON g.group_id = drs.group_id
WHERE g.name = N'%s';`, agName)
}

// AGDBNamesParam returns explicit --mssql-ag-db values.
func AGDBNamesParam(ctx *runner.StepContext) []string {
	if ctx == nil {
		return nil
	}
	return ParseMirrorDBNames(ctx.GetParamString("mssql_ag_db", ""))
}

// AGSeedingMode returns manual or automatic.
func AGSeedingMode(ctx *runner.StepContext) string {
	mode := strings.ToLower(strings.TrimSpace(ctx.GetParamString("mssql_ag_seeding_mode", "manual")))
	if mode == "automatic" || mode == "auto" {
		return "automatic"
	}
	return "manual"
}

// AnyDatabaseMirroringSQL reports active database mirroring on instance.
func AnyDatabaseMirroringSQL() string {
	return MirrorAnyDatabaseMirroringSQL()
}
