package mssql

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// ForceHaCertsEnabled reports whether --mssql-force-ha-certs was set (mirror/ag).
func ForceHaCertsEnabled(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	return ctx.GetParamBool("mssql_force_ha_certs", false)
}

// ShouldDropLocalCertEndpoint allows drop/recreate of local cert+endpoint only when
// both the step is forced (-f/-F) and --mssql-force-ha-certs is set.
func ShouldDropLocalCertEndpoint(ctx *runner.StepContext) bool {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return false
	}
	return ForceHaCertsEnabled(ctx) && ctx.IsForceStep()
}

// ShouldDropPartnerTrust allows drop/recreate of partner cert trust under the same gate.
func ShouldDropPartnerTrust(ctx *runner.StepContext) bool {
	return ShouldDropLocalCertEndpoint(ctx)
}

// ShouldBypassHACertSkip allows forced steps to bypass cert skip when HA is active.
func ShouldBypassHACertSkip(ctx *runner.StepContext) bool {
	return ShouldDropLocalCertEndpoint(ctx)
}

// ForceHaCertsRequiredError is returned when cert recreation needs explicit opt-in.
func ForceHaCertsRequiredError(stepID string) error {
	return fmt.Errorf(
		"%s: partner cert trust mismatch; add --mssql-force-ha-certs with -f %s to recreate certificates (mirror and ag use the same flag)",
		stepID, stepID,
	)
}
