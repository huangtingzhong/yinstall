package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func certThumbprintFromSQL(ctx *runner.StepContext, label, certName string) (string, error) {
	out, err := commonmssql.QuerySqlcmdScalar(ctx, label, commonmssql.QueryCertThumbprintSQL(certName))
	if err != nil {
		return "", err
	}
	return commonmssql.NormalizeCertThumbprint(out), nil
}

func certThumbprintFromCerFile(ctx *runner.StepContext, label, cerPath string) (string, error) {
	if ctx.DryRun || ctx.Precheck {
		return "", nil
	}
	script := commonmssql.CertThumbprintFromCerFilePowerShell(cerPath)
	out, err := commonmssql.RunHAPowerShellScalar(ctx, label+" thumbprint", script)
	if err != nil {
		return "", fmt.Errorf("read cer thumbprint %s: %w", cerPath, err)
	}
	return commonmssql.NormalizeCertThumbprint(out), nil
}

func certThumbprintFromCerUNC(ctx *runner.StepContext, label, cerUNC, partnerKey string) (string, error) {
	if ctx.DryRun || ctx.Precheck {
		return "", nil
	}
	user := commonmssql.HAAdminUser(ctx, partnerKey)
	pass := commonmssql.HAAdminPassword(ctx, partnerKey)
	partnerUNC := commonmssql.AdminShareUNC(partnerKey)
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	script := fmt.Sprintf(`
$ErrorActionPreference='Stop'
$partnerUNC='%s'
$user='%s'
$pass='%s'
$f='%s'
if ($pass) { net use $partnerUNC /user:$user $pass 2>$null | Out-Null }
if (-not (Test-Path -LiteralPath $f)) { exit 2 }
$c=New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($f)
Write-Output $c.Thumbprint
`, esc(partnerUNC), esc(user), esc(pass), esc(cerUNC))
	out, err := commonmssql.RunHAPowerShellScalar(ctx, label+" UNC thumbprint", script)
	if err != nil {
		return "", fmt.Errorf("read UNC cer thumbprint %s: %w", cerUNC, err)
	}
	return commonmssql.NormalizeCertThumbprint(out), nil
}

func partnerLoginExists(ctx *runner.StepContext, loginName string) (bool, error) {
	out, err := commonmssql.QuerySqlcmdScalar(ctx, "partner login exists", commonmssql.PartnerLoginExistsSQL(loginName))
	if err != nil {
		return false, err
	}
	return commonmssql.ParseSqlcmdBoolScalar(out), nil
}

func partnerCertTrustMatchesShare(ctx *runner.StepContext, kind commonmssql.HAEndpointKind, partnerKey, stepID string) (bool, error) {
	certName := commonmssql.HACertName(kind, partnerKey)
	loginName := commonmssql.HALoginName(kind, partnerKey)
	dbThumb, err := certThumbprintFromSQL(ctx, stepID+" db thumbprint", certName)
	if err != nil {
		return false, err
	}
	if dbThumb == "" {
		return false, nil
	}
	hasLogin, err := partnerLoginExists(ctx, loginName)
	if err != nil || !hasLogin {
		return false, err
	}
	remoteCer := partnerCertRemoteUNCForKind(ctx, kind, partnerKey)
	fileThumb, err := certThumbprintFromCerUNC(ctx, stepID+" share thumbprint", remoteCer, partnerKey)
	if err != nil || fileThumb == "" {
		return false, err
	}
	return dbThumb == fileThumb, nil
}

func partnerCertRemoteUNCForKind(ctx *runner.StepContext, kind commonmssql.HAEndpointKind, partnerKey string) string {
	cerFile := commonmssql.MirrorCertFileForHost(ctx, partnerKey, partnerKey)
	if kind == commonmssql.HAEndpointHADR {
		cerFile = commonmssql.HACertFileForHost(ctx, partnerKey, partnerKey)
	}
	return commonmssql.AdminShareUNC(partnerKey) + strings.TrimPrefix(cerFile, `C:`)
}

func haLocalCertMatchesExport(ctx *runner.StepContext, kind commonmssql.HAEndpointKind, hostKey string) (bool, error) {
	if ctx.DryRun || ctx.Precheck {
		return false, nil
	}
	var certName, certFile string
	switch kind {
	case commonmssql.HAEndpointMirror:
		certName = commonmssql.MirrorCertName(hostKey)
		certFile = commonmssql.MirrorCertFile(ctx, hostKey)
	default:
		certName = commonmssql.HACertName(kind, hostKey)
		certFile = commonmssql.HACertFile(ctx, hostKey)
	}
	dbThumb, err := certThumbprintFromSQL(ctx, "local cert thumbprint", certName)
	if err != nil || dbThumb == "" {
		return false, err
	}
	fileThumb, err := certThumbprintFromCerFile(ctx, "export cert thumbprint", certFile)
	if err != nil || fileThumb == "" {
		return false, err
	}
	return dbThumb == fileThumb, nil
}

func haTrustProtected(ctx *runner.StepContext, kind commonmssql.HAEndpointKind) (bool, error) {
	if kind == commonmssql.HAEndpointMirror {
		return mirrorAnyDatabaseMirroring(ctx)
	}
	return commonmssql.AnyAGDatabaseReplicaActive(ctx)
}

func ensurePartnerCertImported(ctx *runner.StepContext, kind commonmssql.HAEndpointKind, stepID, partnerKey, cerLocal string) error {
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	fileThumb, err := certThumbprintFromCerFile(ctx, stepID, cerLocal)
	if err != nil {
		return err
	}
	if fileThumb == "" {
		return fmt.Errorf("%s: empty thumbprint from %s", stepID, cerLocal)
	}
	certName := commonmssql.HACertName(kind, partnerKey)
	dbThumb, err := certThumbprintFromSQL(ctx, stepID+" db thumbprint", certName)
	if err != nil {
		return err
	}
	loginName := commonmssql.HALoginName(kind, partnerKey)
	hasLogin, err := partnerLoginExists(ctx, loginName)
	if err != nil {
		return err
	}
	if dbThumb != "" && dbThumb == fileThumb && hasLogin {
		ctx.Logger.Info("%s: partner cert %s thumbprint matches; ensure GRANT only", stepID, certName)
	} else {
		mismatch := dbThumb != "" && fileThumb != "" && dbThumb != fileThumb
		if mismatch {
			protected, err := haTrustProtected(ctx, kind)
			if err != nil {
				return err
			}
			if protected && !ctx.IsForceStep() {
				return commonmssql.ForceHaCertsRequiredError(stepID)
			}
			if !commonmssql.ShouldDropPartnerTrust(ctx) {
				return commonmssql.ForceHaCertsRequiredError(stepID)
			}
			ctx.Logger.Info("%s: partner cert %s thumbprint mismatch (db=%s file=%s); recreating trust", stepID, certName, dbThumb, fileThumb)
			for _, q := range commonmssql.DropHAPartnerTrustSQL(kind, partnerKey) {
				if err := commonmssql.RunSqlcmdQueries(ctx, stepID+" drop partner trust", []string{q}); err != nil {
					return err
				}
			}
		}
	}
	for _, q := range commonmssql.ImportHAPartnerCertSQL(kind, partnerKey, cerLocal) {
		if err := commonmssql.RunSqlcmdQueries(ctx, stepID+" import partner cert", []string{q}); err != nil {
			return err
		}
	}
	return nil
}

func dropLocalCertEndpointIfForced(ctx *runner.StepContext, kind commonmssql.HAEndpointKind, stepID, hostKey string) error {
	if ctx.DryRun || ctx.Precheck || !commonmssql.ShouldDropLocalCertEndpoint(ctx) {
		return nil
	}
	if kind == commonmssql.HAEndpointMirror {
		if err := commonmssql.GuardForceRecreateMirrorInfrastructure(ctx, stepID); err != nil {
			return err
		}
	}
	ctx.Logger.Info("%s: force rebuild local cert/endpoint for %s", stepID, hostKey)
	for _, q := range commonmssql.DropLocalCertEndpointSQL(kind, hostKey) {
		if err := commonmssql.RunSqlcmdQueries(ctx, stepID+" drop local cert endpoint", []string{q}); err != nil {
			return err
		}
	}
	return nil
}

func selfCertMatchesOnPartnerShare(ctx *runner.StepContext, selfKey, partnerKey string) (bool, error) {
	if ctx.DryRun || ctx.Precheck {
		return false, nil
	}
	certName := commonmssql.MirrorCertName(selfKey)
	dbThumb, err := certThumbprintFromSQL(ctx, "local cert thumbprint", certName)
	if err != nil || dbThumb == "" {
		return false, err
	}
	sharePath := commonmssql.AdminShareMirrorCertPath(ctx, partnerKey, selfKey)
	fileThumb, err := certThumbprintFromCerUNC(ctx, "published cert thumbprint", sharePath, partnerKey)
	if err != nil || fileThumb == "" {
		return false, err
	}
	return dbThumb == fileThumb, nil
}
