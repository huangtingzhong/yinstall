package mssql

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

const (
	// DefaultHACertValidDays is default HA certificate validity (1 year).
	DefaultHACertValidDays = 365
)

// HACertValidDays returns cert validity in days from params.
func HACertValidDays(ctx *runner.StepContext) int {
	if ctx != nil {
		if d := ctx.GetParamInt("mirror_cert_valid_days", 0); d > 0 {
			return d
		}
		if d := ctx.GetParamInt("ha_cert_valid_days", 0); d > 0 {
			return d
		}
	}
	return DefaultHACertValidDays
}

// CreateHAMasterKeySQL creates database master key if missing.
func CreateHAMasterKeySQL() string {
	return CreateMirrorMasterKeySQL()
}

// CreateHACertSQL creates server certificate for HA endpoint.
func CreateHACertSQL(kind HAEndpointKind, hostKey string, validDays int) string {
	if validDays <= 0 {
		validDays = DefaultHACertValidDays
	}
	hostKey = strings.ReplaceAll(hostKey, "'", "''")
	certName := HACertName(kind, hostKey)
	subject := "yinstall ha"
	if kind == HAEndpointMirror {
		subject = "yinstall mirror"
	} else {
		subject = "yinstall hadr"
	}
	start := time.Now().UTC()
	expiry := start.AddDate(0, 0, validDays)
	startStr := start.Format("20060102")
	expiryStr := expiry.Format("20060102")
	return fmt.Sprintf(`
IF NOT EXISTS (SELECT 1 FROM sys.certificates WHERE name = N'%s')
BEGIN
  CREATE CERTIFICATE [%s]
    WITH SUBJECT = N'%s %s',
    START_DATE = '%s', EXPIRY_DATE = '%s';
END`, certName, certName, subject, hostKey, startStr, expiryStr)
}

// ExportHACertSQL returns BACKUP CERTIFICATE statement.
func ExportHACertSQL(kind HAEndpointKind, hostKey, filePath string) string {
	certName := HACertName(kind, hostKey)
	filePath = strings.ReplaceAll(filePath, "'", "''")
	return fmt.Sprintf("BACKUP CERTIFICATE [%s] TO FILE = N'%s';", certName, filePath)
}

// ImportHAPartnerCertSQL creates cert + login + grant for partner on endpoint.
func ImportHAPartnerCertSQL(kind HAEndpointKind, partnerKey, cerPath string) []string {
	login := HALoginName(kind, partnerKey)
	cerPath = strings.ReplaceAll(cerPath, "'", "''")
	partnerCert := HACertName(kind, partnerKey)
	epName := kind.endpointName()
	return []string{
		fmt.Sprintf(`
IF NOT EXISTS (SELECT 1 FROM sys.certificates WHERE name = N'%s')
BEGIN
  CREATE CERTIFICATE [%s] FROM FILE = N'%s';
END`, partnerCert, partnerCert, cerPath),
		fmt.Sprintf(`
IF NOT EXISTS (SELECT 1 FROM sys.server_principals WHERE name = N'%s')
BEGIN
  CREATE LOGIN [%s] FROM CERTIFICATE [%s];
END`, login, login, partnerCert),
		fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM sys.endpoints WHERE name = N'%s')
BEGIN
  GRANT CONNECT ON ENDPOINT::[%s] TO [%s];
END`, epName, epName, login),
	}
}

// CertDirMkdirPowerShell returns script to recreate cert export dir only (preserves HA work dir backups).
func CertDirMkdirPowerShell(workDir, certDir, engineService string) string {
	_ = workDir
	_ = engineService
	certDir = strings.ReplaceAll(certDir, "'", "''")
	return fmt.Sprintf(`$ErrorActionPreference='Stop'; $d='%s'; if (Test-Path -LiteralPath $d -PathType Leaf) { Remove-Item -LiteralPath $d -Force -ErrorAction SilentlyContinue }; if (Test-Path -LiteralPath $d) { Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction SilentlyContinue }; New-Item -ItemType Directory -Force -Path $d | Out-Null; if (-not (Test-Path -LiteralPath $d)) { throw 'cert export directory missing after mkdir' }`,
		certDir)
}

// BackupDirMkdirPowerShell creates backup dir with ACL for SQL service.
func BackupDirMkdirPowerShell(backupDir, instance string) string {
	return fmt.Sprintf(`$d='%s'; New-Item -ItemType Directory -Force -Path $d | Out-Null; icacls $d /grant '%s:(OI)(CI)F' 2>$null | Out-Null`,
		strings.ReplaceAll(backupDir, "'", "''"), SQLServiceAccountName(instance))
}
