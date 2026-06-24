package runner

import "fmt"

// AddToTotal increases the progress denominator (e.g. before embedded child steps).
func (p *StepProgress) AddToTotal(n int) {
	if p != nil && n > 0 {
		p.total += n
	}
}

// RunEmbeddedSteps runs child steps under a parent wrapper step.
// The parent step must have already claimed one progress slot; each non-optional child
// receives the next index and the total is expanded accordingly.
func RunEmbeddedSteps(ctx *StepContext, parentStepID string, steps []*Step) error {
	if ctx == nil {
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	extra := CountNonOptionalSteps(steps)
	if extra > 0 {
		extra--
	}
	if ctx.Progress != nil && extra > 0 {
		ctx.Progress.AddToTotal(extra)
	}
	for _, step := range steps {
		if step == nil {
			continue
		}
		ctx.CurrentStepID = step.ID
		if ctx.Logger != nil && parentStepID != "" {
			ctx.Logger.Info("%s running embedded %s", parentStepID, step.ID)
		}
		result := RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if result.Error != nil {
				return fmt.Errorf("embedded step %s failed: %w", step.ID, result.Error)
			}
			return fmt.Errorf("embedded step %s failed", step.ID)
		}
	}
	return nil
}
