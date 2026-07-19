package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// stepPlanReplicaInstall resolves replica setup media matching primary version.
func stepPlanReplicaInstall() *runner.Step {
	return &runner.Step{
		Name:        "Plan Replica SQL Install",
		Description: "Match replica setup media to primary SQL version or skip when replica already matches",
		Tags:        []string{"mssql-ha", "replica", "install-plan"},
		PreCheck: func(ctx *runner.StepContext) error {
			if commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("M-003 runs on replica only")
			}
			stage := haStage(ctx)
			if !commonmssql.HAIncludesSoftwareInstall(stage) {
				return runner.NewStepSkippedError(fmt.Sprintf("ha stage %q skips replica install", stage))
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			primary, ok := commonmssql.PrimaryInstanceInfoFromResults(ctx.Results)
			if !ok {
				return fmt.Errorf("primary instance info missing; run M-002 first")
			}
			mshLogPhase(ctx, "plan", "M-003 plan replica install")

			host := ctx.Executor.Host()
			if replicaInfo, found, err := commonmssql.ProbeReplicaInstalledSoftware(ctx); err != nil {
				return err
			} else if found {
				if commonmssql.ShouldSkipReplicaSoftwareInstall(replicaInfo, primary) {
					ctx.SetResult("replica_install_skipped", true)
					ctx.Logger.Info("Replica SQL already matches primary at install paths (ProductVersion=%s); skipping install", replicaInfo.ProductVersion)
					commonmssql.StorePrimaryInstanceInfo(ctx.Results, primary)
					ctx.Results[commonmssql.MirrorInstanceInfoResultKey(host)] = replicaInfo
					return nil
				}
				ctx.Logger.Info("Replica SQL exists but version mismatch: replica=%s primary=%s",
					replicaInfo.ProductVersion, primary.ProductVersion)
			}

			if pkg := strings.TrimSpace(ctx.GetParamString("mssql_setup_package", "")); pkg != "" {
				if err := commonmssql.ValidateSetupMediaMatchesPrimary(pkg, primary); err != nil {
					return err
				}
			}

			loc, err := commonmssql.LocateSetupMediaMatchingPrimary(ctx, primary)
			if err != nil {
				return err
			}
			commonmssql.ApplySetupMediaToContext(ctx, loc)
			commonmssql.LogLocatedReplicaSetupMedia(ctx, loc)
			ctx.SetResult("replica_install_skipped", false)
			return nil
		},
	}
}

func haStage(ctx *runner.StepContext) string {
	if s := strings.TrimSpace(ctx.GetParamString("mssql_ha_stage", "")); s != "" {
		return s
	}
	return commonmssql.DefaultHAStage()
}

func mustMajor(info commonmssql.MirrorInstanceInfo) int {
	major, _ := parseMajor(info.ProductMajorVersion)
	return major
}

func parseMajor(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty major")
	}
	var n int
	_, err := fmt.Sscanf(raw, "%d", &n)
	return n, err
}
