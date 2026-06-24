package mssql

import (
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// GetAllSteps returns MSSQL install steps MS-001..MS-020 in order.
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepMS001PlatformTransportDetect(),
		StepMS001RResolveInstance(),
		StepMS002SQLInstallPreflight(),
		StepMS003PortCheck(),
		StepMS004ResolveSetupMedia(),
		StepMS005PrepareDirectories(),
		StepMS006DistributeSetupMedia(),
		StepMS020WriteSetupSoftwareEnv(),
		StepMS007GenerateConfigurationINI(),
		StepMS008InstallInstance(),
		StepMS009ConfigureTCPPort(),
		StepMS010EnsureSQLServices(),
		StepMS011ApplyCUSP(),
		StepMS012ServiceAccount(),
		StepMS013WriteSqlToolsEnv(),
		StepMS014SetSAPassword(),
		StepMS015RemoteSAAccess(),
		StepMS018ConfigureMaxMemory(),
		StepMS019WriteInstanceProfile(),
		StepMS016VerifyInstallation(),
		StepMS017CustomSQL(),
	}
}

// GetInstallStepsForStage returns MS-* steps for embedded replica install.
func GetInstallStepsForStage(stage string) []*runner.Step {
	var out []*runner.Step
	for _, s := range GetAllSteps() {
		if s == nil || !strings.HasPrefix(s.ID, "MS-") {
			continue
		}
		if commonmssql.StepMatchesInstallStage(s, stage) {
			out = append(out, s)
		}
	}
	return out
}
