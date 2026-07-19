package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// stepExchangeCerts exchanges HADR certificates between the current
// host and EVERY other host in the topology that doesn't already have trust.
//
// This is safe for existing AGs: pairs with valid trust are skipped (PreCheck
// passes for those partners, Action only touches new/outdated pairs).
// Adding a new replica exchanges certs between the new node ↔ all existing
// nodes, but does NOT touch existing certs between already-trusted hosts.
func stepExchangeCerts() *runner.Step {
	return &runner.Step{
		Name:        "Exchange HADR Certificates",
		Description: "Publish and import partner HADR certificates (all untrusted peers)",
		Tags:        []string{"mssql-ha", "ag", "cert"},
		PreCheck: func(ctx *runner.StepContext) error {
			// Check if ANY partner still needs trust. If all are ready, skip.
			allReady := true
			for _, pk := range haPeerHosts(ctx) {
				ready, _, _ := haPartnerTrustReady(ctx, commonmssql.HAEndpointHADR, pk)
				if !ready {
					allReady = false
					break
				}
			}
			if allReady {
				return runner.NewStepSkippedError("A-008: all partner trusts established")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if err := discoverHAWorkDir(ctx); err != nil {
				return err
			}
			selfKey := commonmssql.HAHostKey(ctx.Executor.Host())
			kind := commonmssql.HAEndpointHADR
			selfCert := commonmssql.HACertFile(ctx, selfKey)

			// Exchange certs with ALL other hosts that don't yet have trust.
			for _, partnerKey := range haPeerHosts(ctx) {
				partnerKey = strings.TrimSpace(partnerKey)
				if partnerKey == "" || strings.EqualFold(selfKey, partnerKey) {
					continue
				}

				ready, _, err := haPartnerTrustReady(ctx, kind, partnerKey)
				if err != nil {
					return err
				}
				if ready {
					ctx.Logger.Info("A-008: skip cert exchange with %s (trust already established)", partnerKey)
					continue
				}

				ctx.Logger.Info("A-008: exchanging cert with %s", partnerKey)
				partnerShareCert := commonmssql.AdminShareHACertPath(ctx, partnerKey, selfKey)
				user := commonmssql.HAAdminUser(ctx, partnerKey)
				pass := commonmssql.HAAdminPassword(ctx, partnerKey)
				if err := commonmssql.PublishCertToAdminShare(ctx, "A-008 publish cert to "+partnerKey, selfCert, partnerShareCert, partnerKey, user, pass); err != nil {
					return fmt.Errorf("A-008 publish cert to %s: %w", partnerKey, err)
				}

				partnerCertLocal := commonmssql.HACertFileForHost(ctx, selfKey, partnerKey)
				partnerCertRemote := commonmssql.AdminShareUNC(partnerKey) + strings.TrimPrefix(commonmssql.HACertFileForHost(ctx, partnerKey, partnerKey), `C:`)
				user = commonmssql.HAAdminUser(ctx, partnerKey)
				pass = commonmssql.HAAdminPassword(ctx, partnerKey)
				entry, _ := commonmssql.EnsureInstanceResolved(ctx)
				sqlAccount := commonmssql.SQLServiceAccountName(entry.Name)
				if err := commonmssql.ImportCertFromPartner(ctx, "A-008 import cert from "+partnerKey, partnerCertLocal, partnerCertRemote, partnerKey, user, pass, sqlAccount); err != nil {
					return fmt.Errorf("A-008 import cert from %s: %w", partnerKey, err)
				}
				if err := ensurePartnerCertImported(ctx, kind, ctx.CurrentStepID, partnerKey, partnerCertLocal); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// haPeerHosts returns all other hosts in the topology (primary + all replicas
// excluding self), as IPs/keys for cert exchange.
func haPeerHosts(ctx *runner.StepContext) []string {
	self := ""
	if ctx != nil && ctx.Executor != nil {
		self = strings.TrimSpace(ctx.Executor.Host())
	}
	var peers []string
	for _, h := range commonmssql.HATopologyHosts(ctx) {
		h = strings.TrimSpace(h)
		if h == "" || (self != "" && strings.EqualFold(h, self)) {
			continue
		}
		peers = append(peers, commonmssql.HAHostKey(h))
	}
	return peers
}
