package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepCompareVersions() *runner.Step {
	return &runner.Step{
		Name:        "Compare Mirror Instance Versions",
		Description: "Verify primary and mirror SQL Server edition and build match mirroring requirements",
		Tags:        []string{"mssql-ha", "mirror", "preflight", "version"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("M-006 runs on primary only")
			}
			return compareMirrorPartnerVersions(ctx)
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun {
				mshLogPhase(ctx, "version-compare", "dry-run skip")
				return nil
			}
			return compareMirrorPartnerVersions(ctx)
		},
	}
}
