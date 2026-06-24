package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// NormalizeCertThumbprint normalizes thumbprint strings for comparison.
func NormalizeCertThumbprint(s string) string {
	// Strip sqlcmd metadata: separator lines, "(0 rows affected)", "(0 行受影响)".
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) {
			continue
		}
		line = strings.ReplaceAll(line, " ", "")
		line = strings.TrimPrefix(strings.ToUpper(line), "0X")
		if len(line) >= 32 { // SHA-1 in hex is 40 chars
			return line
		}
	}
	return ""
}

// QueryCertThumbprintSQL returns sqlcmd query for certificate thumbprint (style 2 hex, uppercase).
func QueryCertThumbprintSQL(certName string) string {
	certName = strings.ReplaceAll(certName, "'", "''")
	return fmt.Sprintf(`SELECT UPPER(CONVERT(varchar(128), thumbprint, 2)) FROM sys.certificates WHERE name = N'%s';`, certName)
}

// PartnerLoginExistsSQL checks certificate login exists.
func PartnerLoginExistsSQL(loginName string) string {
	loginName = strings.ReplaceAll(loginName, "'", "''")
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.server_principals WHERE name = N'%s'
) THEN N'1' ELSE N'0' END;`, loginName)
}

// DropHAPartnerTrustSQL drops partner login and imported certificate (safe when not mirroring/AG active).
func DropHAPartnerTrustSQL(kind HAEndpointKind, partnerKey string) []string {
	login := HALoginName(kind, partnerKey)
	cert := HACertName(kind, partnerKey)
	epName := kind.endpointName()
	loginEsc := strings.ReplaceAll(login, "'", "''")
	certEsc := strings.ReplaceAll(cert, "'", "''")
	epEsc := strings.ReplaceAll(epName, "'", "''")
	return []string{
		fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.server_principals WHERE name = N'%s')
BEGIN
  IF EXISTS (SELECT 1 FROM sys.endpoints WHERE name = N'%s')
    REVOKE CONNECT ON ENDPOINT::[%s] FROM [%s];
  IF EXISTS (SELECT 1 FROM sys.server_principals WHERE name = N'%s')
    DROP LOGIN [%s];
END`, loginEsc, epEsc, epName, login, loginEsc, login),
		fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.certificates WHERE name = N'%s')
  DROP CERTIFICATE [%s];`, certEsc, cert),
	}
}

// DropLocalCertEndpointSQL stops and drops the endpoint for kind only, then drops local host certificate.
// Does not drop the other HA endpoint type (Mirror vs HADR) on the same instance.
func DropLocalCertEndpointSQL(kind HAEndpointKind, hostKey string) []string {
	epName := kind.endpointName()
	certName := HACertName(kind, hostKey)
	epEsc := strings.ReplaceAll(epName, "'", "''")
	certEsc := strings.ReplaceAll(certName, "'", "''")
	return []string{
		fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.endpoints WHERE name = N'%s')
BEGIN
  ALTER ENDPOINT [%s] STATE = STOPPED;
  DROP ENDPOINT [%s];
END`, epEsc, epName, epName),
		fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.certificates WHERE name = N'%s')
  DROP CERTIFICATE [%s];`, certEsc, certName),
	}
}

// DropConflictingHAEndpointSQL drops the opposite HA endpoint (Mirror vs HADR) to free
// the TCP listener binding when switching modes on the same instance (e.g. mirror remove
// leaves Mirror_endpoint on 5022, blocking CREATE Hadr_endpoint).
func DropConflictingHAEndpointSQL(kind HAEndpointKind) []string {
	var other HAEndpointKind
	switch kind {
	case HAEndpointHADR:
		other = HAEndpointMirror
	case HAEndpointMirror:
		other = HAEndpointHADR
	default:
		return nil
	}
	epName := other.endpointName()
	epEsc := strings.ReplaceAll(epName, "'", "''")
	return []string{fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.endpoints WHERE name = N'%s')
BEGIN
  ALTER ENDPOINT [%s] STATE = STOPPED;
  DROP ENDPOINT [%s];
END`, epEsc, epName, epName)}
}

// AnyAGDatabaseReplicaActive reports whether any AG database replica state exists on the current instance.
func AnyAGDatabaseReplicaActive(ctx *runner.StepContext) (bool, error) {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return false, nil
	}
	stdout, err := QuerySqlcmdScalar(ctx, "any AG db replica", HAAnyAGDatabaseReplicaSQL())
	if err != nil {
		return false, err
	}
	return ParseSqlcmdBoolScalar(stdout), nil
}

// AnyDatabaseMirroringActive reports whether any user database has active mirroring state.
func AnyDatabaseMirroringActive(ctx *runner.StepContext) (bool, error) {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return false, nil
	}
	stdout, err := QuerySqlcmdScalar(ctx, "any db mirroring", MirrorAnyDatabaseMirroringSQL())
	if err != nil {
		return false, err
	}
	return ParseSqlcmdBoolScalar(stdout), nil
}

// GuardForceRecreateMirrorInfrastructure blocks force-recreate of mirror cert/endpoint while HA is in use.
func GuardForceRecreateMirrorInfrastructure(ctx *runner.StepContext, stepID string) error {
	if ag, err := AnyAGDatabaseReplicaActive(ctx); err != nil {
		return err
	} else if ag {
		return fmt.Errorf("%s: cannot force-recreate mirror cert/endpoint while AG databases are active on this instance; use a separate SQL instance or remove AG first", stepID)
	}
	if mir, err := AnyDatabaseMirroringActive(ctx); err != nil {
		return err
	} else if mir {
		return fmt.Errorf("%s: cannot force-recreate mirror cert/endpoint while database mirroring is active on this instance; run mirror remove first", stepID)
	}
	return nil
}

// CertThumbprintFromCerFilePowerShell returns a script that prints .cer file thumbprint.
func CertThumbprintFromCerFilePowerShell(cerPath string) string {
	return fmt.Sprintf(
		`$ErrorActionPreference='Stop'; $c=New-Object System.Security.Cryptography.X509Certificates.X509Certificate2('%s'); $c.Thumbprint`,
		psEsc(cerPath),
	)
}

// HAAnyAGDatabaseReplicaSQL reports whether any AG database replica state exists.
func HAAnyAGDatabaseReplicaSQL() string {
	return `SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.dm_hadr_database_replica_states
) THEN N'1' ELSE N'0' END;`
}
