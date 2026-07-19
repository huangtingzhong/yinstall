package mssql_ag

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepHadrCertEndpoint() *runner.Step {
	return &runner.Step{
		Name:        "HADR Cert and Endpoint",
		Description: "Create master key, certificate, and HADR endpoint",
		Tags:        []string{"mssql-ha", "ag", "cert"},
		PreCheck: func(ctx *runner.StepContext) error {
			ready, reason, err := haCertLocalReady(ctx, commonmssql.HAEndpointHADR)
			if err != nil {
				return err
			}
			if ready {
				return runner.NewStepSkippedError("A-007: " + reason)
			}
			// Even with -F/-f, forbid recreating the local cert on a node
			// that already participates in an AG. Dropping the cert would
			// break HADR endpoint auth for ALL existing partners.
			// Use "remove replica first, then force A-007, then re-add".
			if ctx.IsForceStep() || ctx.ForceAll {
				if !commonmssql.ForceHaCertsEnabled(ctx) {
					return fmt.Errorf("A-007: force-recreate local HADR cert/endpoint requires --mssql-force-ha-certs with -f A-007")
				}
				any, err := commonmssql.AnyAGDatabaseReplicaActive(ctx)
				if err == nil && any {
					return fmt.Errorf("A-007: cannot force-recreate local cert while AG databases are active on this instance; remove AG replicas from this node first, or recreate certs on a new node")
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if err := discoverHAWorkDir(ctx); err != nil {
				return err
			}
			hostKey := commonmssql.HAHostKey(ctx.Executor.Host())
			kind := commonmssql.HAEndpointHADR
			// Force-recreate local cert is ONLY allowed when no AG replica is
			// active (new node). The PreCheck already guards active nodes.
			if err := dropLocalCertEndpointIfForced(ctx, kind, ctx.CurrentStepID, hostKey); err != nil {
				return err
			}
			port := commonmssql.LocalHAEndpointPort(ctx)
			queries := []string{
				commonmssql.EnsureCertEndpointStartedSQL(kind),
				commonmssql.CreateHAMasterKeySQL(),
				commonmssql.CreateHACertSQL(kind, hostKey, commonmssql.HACertValidDays(ctx)),
				commonmssql.CreateCertEndpointSQL(kind, hostKey, port),
				commonmssql.EnsureCertEndpointStartedSQL(kind),
			}
			prefix := commonmssql.DropConflictingHAEndpointSQL(kind)
			queries = append(prefix, queries...)
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-007 HADR cert", queries); err != nil {
				return err
			}
			workDir := commonmssql.HAWorkDir(ctx)
			certDir := commonmssql.HACertDir(ctx)
			inst := commonmssql.ResolvedInstanceName(ctx)
			engine, _ := commonmssql.SqlEngineAndAgentServiceNames(inst)
			mkdir := fmt.Sprintf(`powershell -NoProfile -Command "%s"`, commonmssql.CertDirMkdirPowerShell(workDir, certDir, engine))
			ctx.LogScriptPreview("powershell", "A-007 mkdir certs", mkdir)
			if !ctx.DryRun {
				if _, err := ctx.ExecuteWithCheck(mkdir, false); err != nil {
					return err
				}
			}
			certFile := commonmssql.HACertFile(ctx, hostKey)
			export := commonmssql.ExportHACertSQL(kind, hostKey, certFile)
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-007 export cert", []string{export}); err != nil {
				return err
			}
			ctx.SetResult("ha_cert_file_"+hostKey, certFile)
			return nil
		},
		PostCheck: func(ctx *runner.StepContext) error {
			return commonmssql.VerifyLocalHAEndpointReady(ctx, ctx.CurrentStepID)
		},
	}
}
