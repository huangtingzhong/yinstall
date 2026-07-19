package win_os

import (
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func winStepEntries() []runner.StepEntry {
	return []runner.StepEntry{
		{New: stepPlatformDetect},
		{New: stepTimezoneW32time},
		{New: stepWindowsFirewall},
		{New: stepRemoteManagement},
		{New: stepPrepareDataVolume},
		{New: stepPagefileLpim},
		{New: stepServiceAccountPrep},
		{New: stepPrerequisites},
		{New: stepAntivirusExclusions},
		{New: stepPowerPlan},
		{New: stepSpnVerifyRegister},
		{New: stepBaselineVerify},
	}
}

// GetAllSteps returns all Windows OS steps in execution order (W-001..W-NNN).
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix:  "W",
		Entries: winStepEntries(),
	})
}

func isPostInstanceStep(s *runner.Step) bool {
	if s == nil {
		return false
	}
	for _, t := range s.Tags {
		if t == "post-instance" {
			return true
		}
	}
	return s.Name == "SPN Verify Register" || s.Name == "OS Baseline Verify"
}

// GetPreInstanceSteps returns W-* steps to run before SQL instance setup.
func GetPreInstanceSteps(profile commonwin.Profile) []*runner.Step {
	all := GetAllSteps()
	var pre []*runner.Step
	for _, s := range all {
		if !isPostInstanceStep(s) {
			pre = append(pre, s)
		}
	}
	return commonwin.FilterSteps(pre, profile)
}

// GetPostInstanceSteps returns W-* steps after SQL instance setup.
func GetPostInstanceSteps(profile commonwin.Profile) []*runner.Step {
	all := GetAllSteps()
	var post []*runner.Step
	for _, s := range all {
		if isPostInstanceStep(s) {
			post = append(post, s)
		}
	}
	return commonwin.FilterSteps(post, profile)
}

// GetStepsForProfile returns filtered steps for a product profile.
func GetStepsForProfile(profile commonwin.Profile) []*runner.Step {
	return GetAllSteps()
}
