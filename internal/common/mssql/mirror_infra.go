package mssql

import (
	"fmt"
	"strings"
)

// MirrorEndpointReadySQL checks Mirror_endpoint exists and is started (state=3).
func MirrorEndpointReadySQL() string {
	return `SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.endpoints WHERE name = N'Mirror_endpoint' AND state = 3
) THEN N'1' ELSE N'0' END;`
}

// MirrorLocalCertReadySQL checks the host mirror certificate exists in master.
func MirrorLocalCertReadySQL(hostKey string) string {
	cert := strings.ReplaceAll(MirrorCertName(hostKey), "'", "''")
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.certificates WHERE name = N'%s'
) THEN N'1' ELSE N'0' END;`, cert)
}

// MirrorPartnerTrustReadySQL checks partner certificate and login exist locally.
func MirrorPartnerTrustReadySQL(partnerKey string) string {
	cert := strings.ReplaceAll(MirrorCertName(partnerKey), "'", "''")
	login := strings.ReplaceAll(MirrorLoginName(partnerKey), "'", "''")
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.certificates WHERE name = N'%s'
) AND EXISTS (
  SELECT 1 FROM sys.server_principals WHERE name = N'%s'
) THEN N'1' ELSE N'0' END;`, cert, login)
}

// MirrorAnyDatabaseMirroringSQL reports whether any user database has mirroring configured.
func MirrorAnyDatabaseMirroringSQL() string {
	return `SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.database_mirroring WHERE mirroring_state IS NOT NULL
) THEN N'1' ELSE N'0' END;`
}

// ParseSqlcmdBoolScalar parses sqlcmd output for 1/0 boolean scalar.
func ParseSqlcmdBoolScalar(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if IsSqlcmdMetaLine(line) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "1" {
			return true
		}
		if line == "1" {
			return true
		}
	}
	return false
}

// MirrorCertName returns yinstall mirror certificate name for a host key.
func MirrorCertName(hostKey string) string {
	return mirrorCertName(hostKey)
}

// MirrorLoginName returns certificate login name for a partner host key.
func MirrorLoginName(partnerKey string) string {
	return mirrorLoginName(partnerKey)
}
