package mysql

import (
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

// GetAllSteps returns ordered MySQL installation steps (M-001 .. M-018).
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepM001PlatformDetect(),
		StepM002WriteHosts(),
		StepM003PortCheck(),
		StepM004ResolvePackage(),
		StepM005CreateDirs(),
		StepM006SetOwnership(),
		StepM007InstallVCRedist(),
		StepM008ExtractPackage(),
		StepM009WriteEnvFile(),
		StepM010GenerateMyCnf(),
		StepM011InitializeDatadir(),
		StepM012PrepareLogFiles(),
		StepM013ConfigureAutostart(),
		StepM014StartMysqld(),
		StepM015SetRootPassword(),
		StepM016RemoteRoot(),
		StepM017VerifyInstallation(),
		StepM018CustomSQL(),
	}
}

// GetStepByID finds a step by its ID.
func GetStepByID(id string) *runner.Step {
	for _, s := range GetAllSteps() {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// GetInstanceSteps returns instance-stage M-* steps, optionally excluding IDs (e.g. M-010 for standby cnf copy).
func GetInstanceSteps(excludeIDs ...string) []*runner.Step {
	ex := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		ex[id] = true
	}
	var out []*runner.Step
	for _, s := range GetAllSteps() {
		if s == nil || ex[s.ID] || s.ID == "M-001" {
			continue
		}
		if commonmysql.StepMatchesInstallStage(s, commonmysql.StageInstance) {
			out = append(out, s)
		}
	}
	return out
}

// GetSoftwareSteps returns software-stage M-* steps, optionally excluding IDs.
func GetSoftwareSteps(excludeIDs ...string) []*runner.Step {
	ex := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		ex[id] = true
	}
	var out []*runner.Step
	for _, s := range GetAllSteps() {
		if s == nil || ex[s.ID] || s.ID == "M-001" {
			continue
		}
		if commonmysql.StepMatchesInstallStage(s, commonmysql.StageSoftware) {
			out = append(out, s)
		}
	}
	return out
}
