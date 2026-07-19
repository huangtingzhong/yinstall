package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepMirrorCertEndpoint() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Cert and Endpoint",
		Description: "Create master key, certificate, and DATABASE_MIRRORING endpoint",
		Tags:        []string{"mssql-ha", "mirror", "cert"},
		PreCheck: func(ctx *runner.StepContext) error {
			ready, reason, err := mirrorInfraLocalReady(ctx)
			if err != nil {
				return err
			}
			if ready {
				return runner.NewStepSkippedError("M-008: " + reason)
			}
			if ctx.IsForceStep() && commonmssql.ShouldBypassHACertSkip(ctx) {
				if err := commonmssql.GuardForceRecreateMirrorInfrastructure(ctx, ctx.CurrentStepID); err != nil {
					return err
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if err := discoverMirrorWorkDir(ctx); err != nil {
				return err
			}
			hostKey := commonmssql.MirrorHostKey(ctx.Executor.Host())
			if err := dropLocalCertEndpointIfForced(ctx, commonmssql.HAEndpointMirror, ctx.CurrentStepID, hostKey); err != nil {
				return err
			}
			port := commonmssql.MirrorEndpointPort(ctx)
			mshLogPhase(ctx, "mirror-cert-start", hostKey)
			// Idempotent: create-if-missing then ensure endpoint started (covers stopped endpoint reuse).
			queries := []string{
				commonmssql.EnsureMirrorEndpointStartedSQL(),
				commonmssql.CreateMirrorMasterKeySQL(),
				commonmssql.CreateMirrorCertSQL(hostKey, commonmssql.MirrorCertValidDays(ctx)),
				commonmssql.CreateMirrorEndpointSQL(hostKey, port),
				commonmssql.EnsureMirrorEndpointStartedSQL(),
			}
			if err := commonmssql.RunSqlcmdQueries(ctx, "M-008 mirror cert", queries); err != nil {
				return err
			}
			certEntry, _ := commonmssql.EnsureInstanceResolved(ctx)
			sqlAccount := commonmssql.SQLServiceAccountName(certEntry.Name)
			mkdir := fmt.Sprintf(`powershell -NoProfile -Command "$d='%s'; if (Test-Path -LiteralPath $d -PathType Leaf) { Remove-Item -LiteralPath $d -Force }; New-Item -ItemType Directory -Force -Path $d | Out-Null; icacls $d /grant '%s:(OI)(CI)F' 2>$null | Out-Null"`,
				strings.ReplaceAll(commonmssql.MirrorCertDir(ctx), "'", "''"), sqlAccount)
			ctx.LogScriptPreview("powershell", "M-008 mkdir certs", mkdir)
			if !ctx.DryRun && !ctx.Precheck {
				if _, err := ctx.ExecuteWithCheck(mkdir, false); err != nil {
					return err
				}
			}
			certFile := commonmssql.MirrorCertFile(ctx, hostKey)
			rmCert := fmt.Sprintf(`powershell -NoProfile -Command "Remove-Item -LiteralPath '%s' -Force -ErrorAction SilentlyContinue"`,
				strings.ReplaceAll(certFile, "'", "''"))
			if !ctx.DryRun && !ctx.Precheck {
				ctx.Execute(rmCert, false)
			}
			export := commonmssql.ExportMirrorCertSQL(hostKey, certFile)
			if err := commonmssql.RunSqlcmdQueries(ctx, "M-008 export cert", []string{export}); err != nil {
				return err
			}
			ctx.SetResult("mirror_cert_file_"+hostKey, certFile)
			mshLogPhase(ctx, "mirror-cert-done", certFile)
			return nil
		},
		PostCheck: func(ctx *runner.StepContext) error {
			return commonmssql.VerifyLocalHAEndpointReady(ctx, ctx.CurrentStepID)
		},
	}
}
