package mssql

import (
	"fmt"
	"strings"
)

// HAEndpointKind identifies mirror vs HADR endpoint configuration.
type HAEndpointKind string

const (
	HAEndpointMirror HAEndpointKind = "mirror"
	HAEndpointHADR   HAEndpointKind = "hadr"
)

func (k HAEndpointKind) endpointName() string {
	switch k {
	case HAEndpointHADR:
		return "Hadr_endpoint"
	default:
		return "Mirror_endpoint"
	}
}

// HACertPrefix returns certificate name prefix for host key.
func HACertPrefix(kind HAEndpointKind) string {
	switch kind {
	case HAEndpointHADR:
		return "YinstallHADR_"
	default:
		return "YinstallMirror_"
	}
}

// HACertName returns server certificate name for host key and endpoint kind.
func HACertName(kind HAEndpointKind, hostKey string) string {
	key := sanitizeHostKeyForCert(hostKey)
	return HACertPrefix(kind) + key
}

// HALoginName returns certificate login name for partner host key.
func HALoginName(kind HAEndpointKind, partnerKey string) string {
	prefix := "MirrorLogin_"
	if kind == HAEndpointHADR {
		prefix = "HADRLogin_"
	}
	return prefix + sanitizeHostKeyForCert(partnerKey)
}

func sanitizeHostKeyForCert(hostKey string) string {
	key := strings.ReplaceAll(hostKey, ".", "_")
	return strings.ReplaceAll(key, ":", "_")
}

// CreateCertEndpointSQL creates DATABASE_MIRRORING endpoint with certificate auth.
func CreateCertEndpointSQL(kind HAEndpointKind, hostKey string, port int) string {
	epName := kind.endpointName()
	certName := HACertName(kind, hostKey)
	return fmt.Sprintf(`
IF NOT EXISTS (SELECT 1 FROM sys.endpoints WHERE name = N'%s')
BEGIN
  CREATE ENDPOINT [%s] AS TCP (LISTENER_PORT = %d)
    FOR DATA_MIRRORING (
      ROLE = ALL,
      AUTHENTICATION = CERTIFICATE [%s],
      ENCRYPTION = REQUIRED ALGORITHM AES
    );
  ALTER ENDPOINT [%s] STATE = STARTED;
END`, epName, epName, port, certName, epName)
}

// EnsureCertEndpointStartedSQL starts endpoint when it exists but is not started.
func EnsureCertEndpointStartedSQL(kind HAEndpointKind) string {
	epName := kind.endpointName()
	return fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM sys.endpoints WHERE name = N'%s' AND state <> 3)
BEGIN
  ALTER ENDPOINT [%s] STATE = STARTED;
END`, epName, epName)
}

// EndpointReadySQL checks endpoint exists and is started (state=3).
func EndpointReadySQL(kind HAEndpointKind) string {
	epName := kind.endpointName()
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.endpoints WHERE name = N'%s' AND state = 3
) THEN N'1' ELSE N'0' END;`, epName)
}

// LocalCertReadySQL checks host certificate exists in master.
func LocalCertReadySQL(kind HAEndpointKind, hostKey string) string {
	cert := HACertName(kind, hostKey)
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.certificates WHERE name = N'%s'
) THEN N'1' ELSE N'0' END;`, cert)
}

// PartnerTrustReadySQL checks partner certificate and login exist locally.
func PartnerTrustReadySQL(kind HAEndpointKind, partnerKey string) string {
	cert := HACertName(kind, partnerKey)
	login := HALoginName(kind, partnerKey)
	epName := kind.endpointName()
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.certificates WHERE name = N'%s'
) AND EXISTS (
  SELECT 1 FROM sys.server_principals WHERE name = N'%s'
) AND EXISTS (
  SELECT 1 FROM sys.endpoints WHERE name = N'%s'
) THEN N'1' ELSE N'0' END;`, cert, login, epName)
}
