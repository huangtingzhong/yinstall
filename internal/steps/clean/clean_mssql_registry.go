package clean

import "github.com/yinstall/internal/runner"

func GetMssqlCleanSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "CLEAN-MSSQL",
		Entries: []runner.StepEntry{
			{New: StepCleanMssql001StopServices},
			{New: StepCleanMssql002UninstallInstance},
			{New: StepCleanMssql003RemoveDirectories},
			{New: StepCleanMssql004CleanEnvArtifacts},
			{New: StepCleanMssql005FinalCheck},
		},
	})
}
