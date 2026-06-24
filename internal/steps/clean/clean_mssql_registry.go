package clean

import "github.com/yinstall/internal/runner"

func GetMssqlCleanSteps() []*runner.Step {
	return []*runner.Step{
		StepCleanMssql001StopServices(),
		StepCleanMssql002UninstallInstance(),
		StepCleanMssql003RemoveDirectories(),
		StepCleanMssql004CleanEnvArtifacts(),
		StepCleanMssql005FinalCheck(),
	}
}
