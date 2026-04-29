package runner

import "strings"

// SkipPrecheckDryRunWhenUpstreamArtifactMissing returns nil during --precheck or --dry-run when err
// indicates files or binaries that earlier steps create (extract, gen config, deploy, etc.).
// Normal apply still fails if artifacts are missing. Matching is substring-based and intentionally narrow.
func SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx *StepContext, err error) error {
	if err == nil || ctx == nil {
		return err
	}
	if !ctx.Precheck && !ctx.DryRun {
		return err
	}
	msg := err.Error()
	if isUpstreamArtifactMissingMessage(msg) {
		ctx.Logger.Info("Precheck/dry-run: %s (skipped: artifact comes from an earlier step when apply runs)", msg)
		return nil
	}
	return err
}

func isUpstreamArtifactMissingMessage(msg string) bool {
	substrings := []string{
		"yasboot not found at ",
		"hosts.toml not found at ",
		"cluster config not found at ",
		"hosts_add.toml not found",
		"_add.toml not found, run E-011",
		"deploy config not found: ",
		"deploy config file not found: ",
		"ycm-init not found in any of:",
		"ymp.sh not found at ",
		"yashan_monit.sh not found or not executable",
		"primary env file not found: ",
		"specified primary environment file ",
		"primary environment file not found (tried:",
		"sysctl config file not found",
	}
	for _, s := range substrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
