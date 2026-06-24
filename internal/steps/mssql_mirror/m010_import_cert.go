package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepM010MirrorImportCert() *runner.Step {
	return &runner.Step{
		ID:          "M-010",
		Name:        "Import Partner Mirror Certificate",
		Description: "Fetch partner cert from admin share and grant endpoint access",
		Tags:        []string{"mssql-ha", "mirror", "cert"},
		PreCheck: func(ctx *runner.StepContext) error {
			partnerKey := commonmssql.MirrorHostKey(commonmssql.MirrorPartnerHost(ctx))
			ready, reason, err := mirrorPartnerTrustReady(ctx, partnerKey)
			if err != nil {
				return err
			}
			if ready {
				return runner.NewStepSkippedError("M-010: " + reason)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if err := discoverMirrorWorkDir(ctx); err != nil {
				return err
			}
			selfKey := commonmssql.MirrorHostKey(ctx.Executor.Host())
			partnerKey := commonmssql.MirrorHostKey(commonmssql.MirrorPartnerHost(ctx))
			if partnerKey == "" || strings.EqualFold(selfKey, partnerKey) {
				return fmt.Errorf("M-010: cannot resolve mirror partner host")
			}
			mshLogPhase(ctx, "mirror-import-start", partnerKey)

			partnerCertLocal := commonmssql.MirrorCertFileForHost(ctx, selfKey, partnerKey)
			partnerCertRemote := commonmssql.AdminShareUNC(partnerKey) + strings.TrimPrefix(commonmssql.MirrorCertFileForHost(ctx, partnerKey, partnerKey), `C:`)
			user := commonmssql.HAAdminUser(ctx, partnerKey)
			pass := commonmssql.HAAdminPassword(ctx, partnerKey)
			entry, _ := commonmssql.EnsureInstanceResolved(ctx)

			sqlAccount := commonmssql.SQLServiceAccountName(entry.Name)
			if err := commonmssql.ImportCertFromPartner(ctx, "M-010 import cert", partnerCertLocal, partnerCertRemote, partnerKey, user, pass, sqlAccount); err != nil {
				return err
			}

			if err := ensurePartnerCertImported(ctx, commonmssql.HAEndpointMirror, "M-010", partnerKey, partnerCertLocal); err != nil {
				return err
			}
			mshLogPhase(ctx, "mirror-import-done", partnerKey)
			return nil
		},
	}
}
