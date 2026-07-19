package clean

import "github.com/yinstall/internal/runner"

// GetAllSteps returns all clean steps.
func GetAllSteps() []*runner.Step {
	out := GetDBCleanSteps()
	extra := runner.BuildSteps(runner.StepSpec{
		Entries: []runner.StepEntry{
			{FixedID: "CLEAN-YCM", New: StepCleanYCM},
			{FixedID: "CLEAN-YMP", New: StepCleanYMP},
		},
	})
	return append(out, extra...)
}

// GetDBCleanSteps returns detailed DB cleanup steps (CLEAN-DB-001..)。
// 序号由 registry 顺序赋予：001 查 YAC → 002 摘备 → 003 停进程 → …
func GetDBCleanSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "CLEAN-DB",
		Entries: []runner.StepEntry{
			{New: StepCleanDB001QueryYACDisks},
			{New: StepCleanDBDetachStandby},
			{New: StepCleanDB002StopProcesses},
			{New: StepCleanDB003RemoveDirectories},
			{New: StepCleanDB004RemoveConfig},
			{New: StepCleanDB005CleanYACDisks},
			{New: StepCleanDB006FinalCheck},
		},
	})
}

// GetStepByID returns a step by its ID（YCM/YMP/MySQL 固定 ID，或 registry 动态步）。
func GetStepByID(id string) *runner.Step {
	if id == "CLEAN-MYSQL" {
		return StepCleanMySQL()
	}
	for _, step := range GetAllSteps() {
		if step.ID == id {
			return step
		}
	}
	for _, step := range GetMysqlCleanSteps() {
		if step.ID == id {
			return step
		}
	}
	for _, step := range GetMssqlCleanSteps() {
		if step.ID == id {
			return step
		}
	}
	return nil
}
