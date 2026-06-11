package mysql

import (
	"fmt"
	"strings"
)

// DefaultReplicationUser is the default MySQL replication account name.
const DefaultReplicationUser = "rep"

// mysqldumpPrivileges are extra grants beyond replication for remote dump on primary.
const mysqldumpPrivileges = "SELECT, SHOW VIEW, TRIGGER, EVENT, RELOAD, LOCK TABLES, PROCESS"

// replicationPrivileges are required for replica and clone connections.
const replicationPrivileges = "REPLICATION SLAVE, REPLICATION CLIENT, BACKUP_ADMIN"

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// ChannelName builds default replication channel name.
func ChannelName(primaryHost string, primaryPort int, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	host := strings.TrimSpace(primaryHost)
	host = strings.ReplaceAll(host, ":", "_")
	return fmt.Sprintf("channel_%s_%d", host, primaryPort)
}

// ClonePluginSQL returns INSTALL PLUGIN for clone.
func ClonePluginSQL(platform string) string {
	suffix := "mysql_clone.so"
	if platform == PlatformWindows {
		suffix = "mysql_clone.dll"
	}
	return fmt.Sprintf("INSTALL PLUGIN clone SONAME '%s'", suffix)
}

// SemiSyncPluginSQL returns install statements for semi-sync (8.0.26+ naming).
func SemiSyncPluginSQL(platform, role string) string {
	ext := ".so"
	if platform == PlatformWindows {
		ext = ".dll"
	}
	switch role {
	case "source", "primary":
		return fmt.Sprintf("INSTALL PLUGIN rpl_semi_sync_source SONAME 'semisync_source%s'", ext)
	case "replica", "replica_side":
		return fmt.Sprintf("INSTALL PLUGIN rpl_semi_sync_replica SONAME 'semisync_replica%s'", ext)
	default:
		return ""
	}
}

// ReplicationOpts holds CHANGE REPLICATION SOURCE / MASTER options.
type ReplicationOpts struct {
	PrimaryHost  string
	PrimaryPort  int
	RepUser      string
	RepPassword  string
	Channel      string
	Version      string
	GetPublicKey bool
	UseSSL       bool
	SSLCA        string
	SSLCert      string
	SSLKey       string
}

// BuildChangeReplicationSource returns replication setup SQL.
func BuildChangeReplicationSource(opts ReplicationOpts) string {
	esc := escapeSQLString
	pw := esc(opts.RepPassword)
	user := esc(opts.RepUser)
	host := esc(opts.PrimaryHost)
	ch := strings.TrimSpace(opts.Channel)
	channelClause := ""
	if ch != "" {
		channelClause = fmt.Sprintf(" FOR CHANNEL '%s'", esc(ch))
	}
	if UsesReplicationSourceSyntax(opts.Version) {
		sql := fmt.Sprintf(
			"CHANGE REPLICATION SOURCE TO SOURCE_HOST='%s', SOURCE_PORT=%d, SOURCE_USER='%s', SOURCE_PASSWORD='%s', SOURCE_AUTO_POSITION=1, SOURCE_CONNECT_RETRY=10, SOURCE_HEARTBEAT_PERIOD=5",
			host, opts.PrimaryPort, user, pw)
		if opts.GetPublicKey {
			sql += ", GET_SOURCE_PUBLIC_KEY=1"
		}
		if opts.UseSSL {
			sql += fmt.Sprintf(", SOURCE_SSL=1, SOURCE_SSL_CA='%s', SOURCE_SSL_CERT='%s', SOURCE_SSL_KEY='%s'",
				esc(opts.SSLCA), esc(opts.SSLCert), esc(opts.SSLKey))
		}
		return sql + channelClause
	}
	sql := fmt.Sprintf(
		"CHANGE MASTER TO MASTER_HOST='%s', MASTER_PORT=%d, MASTER_USER='%s', MASTER_PASSWORD='%s', MASTER_AUTO_POSITION=1, GET_MASTER_PUBLIC_KEY=1",
		host, opts.PrimaryPort, user, pw)
	return sql + channelClause
}

// BuildStartReplica returns START REPLICA or START SLAVE.
func BuildStartReplica(version, channel string) string {
	ch := strings.TrimSpace(channel)
	if UsesReplicationSourceSyntax(version) {
		if ch != "" {
			return fmt.Sprintf("START REPLICA FOR CHANNEL '%s'", escapeSQLString(ch))
		}
		return "START REPLICA"
	}
	if ch != "" {
		return fmt.Sprintf("START SLAVE FOR CHANNEL '%s'", escapeSQLString(ch))
	}
	return "START SLAVE"
}

// BuildStopReplica returns STOP REPLICA or STOP SLAVE.
func BuildStopReplica(version, channel string) string {
	ch := strings.TrimSpace(channel)
	if UsesReplicationSourceSyntax(version) {
		if ch != "" {
			return fmt.Sprintf("STOP REPLICA FOR CHANNEL '%s'", escapeSQLString(ch))
		}
		return "STOP REPLICA"
	}
	if ch != "" {
		return fmt.Sprintf("STOP SLAVE FOR CHANNEL '%s'", escapeSQLString(ch))
	}
	return "STOP SLAVE"
}

// BuildResetReplicaAll returns RESET REPLICA ALL or RESET SLAVE ALL.
func BuildResetReplicaAll(version, channel string) string {
	ch := strings.TrimSpace(channel)
	if UsesReplicationSourceSyntax(version) {
		if ch != "" {
			return fmt.Sprintf("RESET REPLICA ALL FOR CHANNEL '%s'", escapeSQLString(ch))
		}
		return "RESET REPLICA ALL"
	}
	if ch != "" {
		return fmt.Sprintf("RESET SLAVE ALL FOR CHANNEL '%s'", escapeSQLString(ch))
	}
	return "RESET SLAVE ALL"
}

// BuildReplicationFilter returns CHANGE REPLICATION FILTER statement (8.0+).
func BuildReplicationFilter(doDBs, ignoreDBs []string) string {
	var parts []string
	if len(doDBs) > 0 {
		quoted := make([]string, len(doDBs))
		for i, d := range doDBs {
			quoted[i] = "'" + escapeSQLString(d) + "'"
		}
		parts = append(parts, "REPLICATE_DO_DB = ("+strings.Join(quoted, ", ")+")")
	}
	if len(ignoreDBs) > 0 {
		quoted := make([]string, len(ignoreDBs))
		for i, d := range ignoreDBs {
			quoted[i] = "'" + escapeSQLString(d) + "'"
		}
		parts = append(parts, "REPLICATE_IGNORE_DB = ("+strings.Join(quoted, ", ")+")")
	}
	if len(parts) == 0 {
		return ""
	}
	return "CHANGE REPLICATION FILTER " + strings.Join(parts, ", ")
}

// BuildCreateReplicationUser returns CREATE USER + GRANT for replication.
func BuildCreateReplicationUser(user, password string, useSSL bool) string {
	esc := escapeSQLString
	u := esc(user)
	p := esc(password)
	require := ""
	if useSSL {
		require = " REQUIRE SSL"
	}
	return fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'%s; "+
		"GRANT %s ON *.* TO '%s'@'%%'; FLUSH PRIVILEGES",
		u, p, require, replicationPrivileges, u)
}

// BuildGrantReplicationPrivileges grants replication privileges to an existing account.
// Prefer over ALTER USER when the account already exists: GRANT replicates safely to running replicas.
func BuildGrantReplicationPrivileges(user string, hosts []string) string {
	esc := escapeSQLString
	u := esc(user)
	if len(hosts) == 0 {
		hosts = []string{"%"}
	}
	var stmts []string
	for _, h := range hosts {
		host := strings.TrimSpace(h)
		if host == "" {
			host = "%"
		}
		ht := esc(host)
		stmts = append(stmts, fmt.Sprintf("GRANT %s ON *.* TO '%s'@'%s'", replicationPrivileges, u, ht))
	}
	stmts = append(stmts, "FLUSH PRIVILEGES")
	return strings.Join(stmts, "; ") + ";"
}

// BuildGrantDumpPrivileges grants mysqldump privileges to an existing account (e.g. rep@'%').
func BuildGrantDumpPrivileges(user string, hosts []string) string {
	esc := escapeSQLString
	u := esc(user)
	if len(hosts) == 0 {
		hosts = []string{"%"}
	}
	var stmts []string
	for _, h := range hosts {
		host := strings.TrimSpace(h)
		if host == "" {
			host = "%"
		}
		ht := esc(host)
		stmts = append(stmts, fmt.Sprintf("GRANT %s ON *.* TO '%s'@'%s'", mysqldumpPrivileges, u, ht))
	}
	stmts = append(stmts, "FLUSH PRIVILEGES")
	return strings.Join(stmts, "; ") + ";"
}

// BuildEnsureDumpUser returns CREATE/GRANT for a dedicated remote mysqldump account.
func BuildEnsureDumpUser(user, password string, hosts []string) string {
	esc := escapeSQLString
	u := esc(user)
	p := esc(password)
	if len(hosts) == 0 {
		hosts = []string{"%"}
	}
	var stmts []string
	for _, h := range hosts {
		host := strings.TrimSpace(h)
		if host == "" {
			host = "%"
		}
		ht := esc(host)
		stmts = append(stmts, fmt.Sprintf(
			"CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'; "+
				"GRANT %s, REPLICATION CLIENT ON *.* TO '%s'@'%s'",
			u, ht, p, mysqldumpPrivileges, u, ht))
	}
	stmts = append(stmts, "FLUSH PRIVILEGES")
	return strings.Join(stmts, "; ") + ";"
}
