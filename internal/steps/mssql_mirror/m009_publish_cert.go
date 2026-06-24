package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepM009MirrorPublishCert() *runner.Step {
	return &runner.Step{
		ID:          "M-009",
		Name:        "Publish Mirror Certificate",
		Description: "Copy local mirror cert to partner admin share",
		Tags:        []string{"mssql-ha", "mirror", "cert"},
		PreCheck: func(ctx *runner.StepContext) error {
			ready, reason, err := mirrorCertPublishReady(ctx)
			if err != nil {
				return err
			}
			if ready {
				return runner.NewStepSkippedError("M-009: " + reason)
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
				return fmt.Errorf("M-009: cannot resolve mirror partner host")
			}
			selfCert := commonmssql.MirrorCertFile(ctx, selfKey)
			partnerShareCert := commonmssql.AdminShareMirrorCertPath(ctx, partnerKey, selfKey)
			user := commonmssql.HAAdminUser(ctx, partnerKey)
			pass := commonmssql.HAAdminPassword(ctx, partnerKey)

			return commonmssql.PublishCertToAdminShare(ctx, "M-009 publish cert", selfCert, partnerShareCert, partnerKey, user, pass)
		},
	}
}
