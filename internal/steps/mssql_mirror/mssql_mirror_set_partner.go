package mssql_mirror

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepSetPartner() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Set Partner",
		Description: "Log backup/restore and configure mirroring partners",
		Tags:        []string{"mssql-ha", "mirror", "verify"},
		PreCheck: func(ctx *runner.StepContext) error {
			dbs, err := ensureMirrorTargetDBs(ctx)
			if err != nil {
				return err
			}
			if len(dbs) > 0 && allMirrorDBsMatch(ctx, dbs, func(db string) bool { return mirrorDBSynchronized(ctx, db) }) {
				return runner.NewStepSkippedError("M-013: mirroring already established")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			dbs, err := ensureMirrorTargetDBs(ctx)
			if err != nil {
				return err
			}
			if len(dbs) == 0 {
				return fmt.Errorf("M-013: no mirror target databases")
			}
			partner := commonmssql.MirrorPartnerHost(ctx)
			port := commonmssql.HAEndpointPortForHost(ctx, partner)
			addr := commonmssql.MirrorPartnerAddress(partner, port)
			if addr == "" {
				return fmt.Errorf("M-013: empty partner address")
			}

			switch ctx.GetParamString("mirror_013_phase", "") {
			case "log-backup":
				return m013LogBackup(ctx, dbs)
			case "log-restore-partner-secondary":
				return m013LogRestorePartnerSecondary(ctx, dbs, addr)
			case "partner-primary":
				return m013PartnerPrimary(ctx, dbs, addr)
			default:
				return fmt.Errorf("M-013: unknown mirror_013_phase %q (expected log-backup, log-restore-partner-secondary, or partner-primary)", ctx.GetParamString("mirror_013_phase", ""))
			}
		},
	}
}
