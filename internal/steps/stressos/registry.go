// registry.go - stressos 步骤注册表
package stressos

import (
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
)

// GetAllSteps 返回按执行顺序排列的全部 stressos 步骤（S-001..S-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "S",
		Entries: []runner.StepEntry{
			{New: stepCheckConnectivity},
			{New: stepInitArchiveDir},
			{New: stepInstallDeps},
			{New: stepPreSnapshot},
			{New: stepCpuBench},
			{New: stepMemBench},
			{New: stepIoBench},
			{New: stepNetBench},
			{New: stepRuntimeMetrics},
			{New: stepPostSnapshot},
			{New: stepFinalize},
		},
	})
}

// FirstStepID returns S-001 (or first registry step ID).
func FirstStepID() string {
	return runner.FirstStepID(GetAllSteps(), "S")
}

// StepIDByName returns the registry ID for a step Name.
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}

func stepCheckConnectivity() *runner.Step {
	return runner.CloneStep(ossteps.StepCheckConnectivity())
}
