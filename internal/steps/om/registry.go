// registry.go - OM 域步骤注册 (O-*)
package om

import "github.com/yinstall/internal/runner"

func omAllEntries() []runner.StepEntry {
	return []runner.StepEntry{
		// 迁主 P1
		{New: stepMigrateGate},
		{New: stepHostPrepare},
		{New: stepHostAdd},
		{New: stepRecoverSecondary},
		{New: stepStopPrimary},
		{New: stepRecoverPrimary},
		{New: stepSync},
		{New: stepUpdateHostsTOML},
		{New: stepRecoverOldSecondary},
		{New: stepSwitchContext},
		// 部署备 OM P2
		{New: stepDeploySecondaryGate},
		{New: stepDeploySecondaryHost},
		// 同机改 IP
		{New: stepIpchangeYasom},
	}
}

// GetAllSteps 返回迁主 + 部署备 OM 全部步骤 (连续 O-ID)。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix:  "O",
		Entries: omAllEntries(),
	})
}

// GetMigrateSteps 返回迁主步骤 (O-001..O-010)。
func GetMigrateSteps() []*runner.Step {
	return filterOMStepsByTag(GetAllSteps(), "migrate")
}

// GetDeploySecondarySteps 返回 P2 部署备 OM 步骤。
func GetDeploySecondarySteps() []*runner.Step {
	return filterOMStepsByTag(GetAllSteps(), "deploy-secondary")
}

// GetIpchangeSteps 返回同机改 IP 步骤。
func GetIpchangeSteps() []*runner.Step {
	return filterOMStepsByTag(GetAllSteps(), "ipchange")
}

func filterOMStepsByTag(steps []*runner.Step, tag string) []*runner.Step {
	var out []*runner.Step
	for _, s := range steps {
		if s == nil {
			continue
		}
		for _, t := range s.Tags {
			if t == tag {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// FirstStepID 返回首个 O 步 ID。
func FirstStepID() string {
	return runner.FirstStepID(GetAllSteps(), "O")
}

// StepIDByName 按 Name 查 O 步 ID。
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}
