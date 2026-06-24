package win_os

import (
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func allStepConstructors() []func() *runner.Step {
	return []func() *runner.Step{
		StepW001WindowsPlatformDetect,
		StepW003TimezoneW32Time,
		StepW004WindowsFirewall,
		StepW005RemoteManagement,
		StepW006PrepareDataVolume,
		StepW007PagefileLPIM,
		StepW008ServiceAccountPrep,
		StepW009OSPrerequisites,
		StepW010AntivirusExclusions,
		StepW013PowerPlan,
		StepW014SPNVerifyRegister,
		StepW012OSBaselineVerify,
	}
}

// GetAllSteps returns all Windows OS steps in execution order.
func GetAllSteps() []*runner.Step {
	out := make([]*runner.Step, 0, len(allStepConstructors()))
	for _, fn := range allStepConstructors() {
		out = append(out, fn())
	}
	return out
}

var preInstanceIDs = map[string]bool{
	"W-001": true, "W-003": true, "W-004": true, "W-005": true,
	"W-006": true, "W-007": true, "W-008": true, "W-009": true, "W-010": true,
	"W-013": true,
}

var postInstanceIDs = map[string]bool{
	"W-014": true, "W-012": true,
}

func filterByIDs(steps []*runner.Step, ids map[string]bool) []*runner.Step {
	var out []*runner.Step
	for _, s := range steps {
		if ids[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// GetPreInstanceSteps returns W-* steps to run before SQL instance setup.
func GetPreInstanceSteps(profile commonwin.Profile) []*runner.Step {
	steps := filterByIDs(GetAllSteps(), preInstanceIDs)
	return commonwin.FilterSteps(steps, profile)
}

// GetPostInstanceSteps returns W-* steps after SQL instance setup.
func GetPostInstanceSteps(profile commonwin.Profile) []*runner.Step {
	steps := filterByIDs(GetAllSteps(), postInstanceIDs)
	return commonwin.FilterSteps(steps, profile)
}

// GetStepsForProfile returns filtered steps for a product profile.
func GetStepsForProfile(profile commonwin.Profile) []*runner.Step {
	return GetAllSteps()
}
