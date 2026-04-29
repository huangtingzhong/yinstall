package db

import "github.com/yinstall/internal/runner"

// skipPrecheckDryRunWhenUpstreamDBArtifactMissing delegates to runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing.
func skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx *runner.StepContext, err error) error {
	return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
}
