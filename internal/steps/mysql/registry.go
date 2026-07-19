package mysql

import (
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

// GetAllSteps returns ordered MySQL installation steps (M-001 .. M-NNN).
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "M",
		Entries: []runner.StepEntry{
			{New: stepPlatformDetect},
			{New: stepWriteHosts},
			{New: stepPortCheck},
			{New: stepResolvePackage},
			{New: stepCreateDirs},
			{New: stepSetOwnership},
			{New: stepInstallVcredist},
			{New: stepExtractPackage},
			{New: stepWriteEnvFile},
			{New: stepGenerateMycnf},
			{New: stepInitializeDatadir},
			{New: stepPrepareLogFiles},
			{New: stepSelinuxContext},
			{New: stepConfigureAutostart},
			{New: stepStartMysqld},
			{New: stepSetRootPassword},
			{New: stepRemoteRoot},
			{New: stepVerifyInstallation},
			{New: stepCustomSql},
		},
	})
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

func filterSteps(excludeIDs map[string]bool, match func(*runner.Step) bool) []*runner.Step {
	var out []*runner.Step
	for _, s := range GetAllSteps() {
		if s == nil || excludeIDs[s.ID] || s.ID == "M-001" {
			continue
		}
		if match(s) {
			out = append(out, s)
		}
	}
	return out
}

// GetInstanceSteps returns instance-stage M-* steps, optionally excluding IDs.
func GetInstanceSteps(excludeIDs ...string) []*runner.Step {
	ex := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		ex[id] = true
	}
	return filterSteps(ex, func(s *runner.Step) bool {
		return commonmysql.StepMatchesInstallStage(s, commonmysql.StageInstance)
	})
}

// GetSoftwareSteps returns software-stage M-* steps, optionally excluding IDs.
func GetSoftwareSteps(excludeIDs ...string) []*runner.Step {
	ex := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		ex[id] = true
	}
	return filterSteps(ex, func(s *runner.Step) bool {
		return commonmysql.StepMatchesInstallStage(s, commonmysql.StageSoftware)
	})
}

// FirstStepID returns the ID of the first step in the registry (M-001).
func FirstStepID() string {
	steps := GetAllSteps()
	if len(steps) == 0 {
		return "M-001"
	}
	return steps[0].ID
}

// StepIDGenerateMyCnf returns the current ID for the generate-mycnf step (registry order).
func StepIDGenerateMyCnf() string {
	steps := GetAllSteps()
	for _, s := range steps {
		if s != nil && s.Name == "Generate my.cnf" {
			return s.ID
		}
	}
	return "M-010"
}
