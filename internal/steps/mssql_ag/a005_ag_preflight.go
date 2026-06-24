package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepA005HAPreflight() *runner.Step {
	return &runner.Step{
		ID:          "A-005",
		Name:        "HA Preflight",
		Description: "Validate multi-node targets, versions, and WSFC for AG",
		Tags:        []string{"mssql-ha", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			hosts := ctx.HostsToRun()
			if len(hosts) < 2 {
				return runner.NewStepSkippedError("HA preflight requires 2+ targets")
			}
			res, _ := ctx.Execute(`powershell -NoProfile -Command "`+commonmssql.OSBuildCheckPowerShell()+`"`, false)
			if res != nil {
				build, err := commonmssql.ParseOSBuildNumber(res.GetStdout())
				if err != nil {
					return fmt.Errorf("A-005: %w", err)
				}
				if err := commonmssql.ValidateOSBuild(build, commonmssql.MinWS2016Build); err != nil {
					return err
				}
			}
			res, _ = ctx.Execute(`powershell -NoProfile -Command "`+commonmssql.WSFCClusterNamePowerShell()+`"`, false)
			if res == nil || strings.TrimSpace(res.GetStdout()) == "" {
				return fmt.Errorf("A-005: no WSFC cluster detected; configure WSFC externally before mssql ha")
			}
			cluster := strings.TrimSpace(res.GetStdout())
			ctx.SetResult("wsfc_cluster", cluster)
			mshLogPhase(ctx, "preflight-wsfc", cluster)
			stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-005 instance version", commonmssql.HAInstanceInfoSQL())
			if err != nil && !ctx.DryRun {
				return err
			}
			if stdout != "" {
				host := ctx.Executor.Host()
				info, err := commonmssql.ParseHAInstanceInfo(host, stdout)
				if err != nil {
					return fmt.Errorf("A-005: %w", err)
				}
				if err := commonmssql.ValidateHAMajorVersion(info, commonmssql.MinSQLMajorVersionAG); err != nil {
					return err
				}
				if commonmssql.NormalizeHAMode(ctx.GetParamString("mssql_ha_mode", commonmssql.HAModeMirror)) == commonmssql.HAModeAG {
					if err := commonmssql.ValidateAGEdition(info); err != nil {
						return fmt.Errorf("A-005: %w", err)
					}
				}
				ctx.SetResult(commonmssql.HAInstanceInfoResultKey(host), info)
				ctx.SetResult(commonmssql.MirrorInstanceInfoResultKey(host), commonmssql.MirrorInstanceInfo{
					Host: info.Host, ProductVersion: info.ProductVersion, ProductLevel: info.ProductLevel,
					Edition: info.Edition, EngineEdition: info.EngineEdition, ProductMajorVersion: info.ProductMajorVersion,
				})
				if repOut, err := commonmssql.QuerySqlcmdScalar(ctx, "A-005 replica server name", commonmssql.HAReplicaServerNameSQL()); err == nil && repOut != "" {
					if repName, err := commonmssql.ParseHAReplicaServerName(repOut); err == nil {
						ctx.SetResult(commonmssql.HAReplicaServerNameResultKey(host), repName)
					}
				}
			}
			mirrorAny, err := commonmssql.QuerySqlcmdScalar(ctx, "A-005 mirroring check", commonmssql.AnyDatabaseMirroringSQL())
			if err == nil && commonmssql.ParseSqlcmdBoolScalar(mirrorAny) {
				return fmt.Errorf("A-005: database mirroring active on instance; run mssql ha remove before AG")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			var infos []commonmssql.HAInstanceInfo
			primary := commonmssql.ResolvePrimaryHost(ctx)
			for _, th := range ctx.HostsToRun() {
				h := th.Host
				if v, ok := ctx.Results[commonmssql.HAInstanceInfoResultKey(h)].(commonmssql.HAInstanceInfo); ok {
					infos = append(infos, v)
				}
			}
			if len(infos) >= 2 {
				if err := commonmssql.CompareHAPartners(primary, infos); err != nil {
					return err
				}
			}
			mshLogPhase(ctx, "plan", "A-005 HA preflight ok")
			return nil
		},
	}
}
