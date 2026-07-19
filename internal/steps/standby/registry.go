package standby

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回所有备库扩容步骤（ID 由 BuildSteps 自动赋 E-001..E-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "E",
		Entries: []runner.StepEntry{
			{New: stepCheckPrimaryConnectivity},
			{New: stepCheckPrimaryStatus},
			{New: stepCheckArchiveMode},
			{New: stepCheckReplicationAddr},

			{New: stepCheckStandbyConnectivity},
			{New: stepCheckStandbyBeginPort},
			{New: stepCheckStandbyExpansionPaths},
			{New: stepCheckArchiveDest},
			{New: stepCheckNetworkConnectivity},
			{New: stepCheckAndCleanupExistingNodes},

			{New: stepGenExpansionConfig},
			{New: stepInstallSoftware},
			{New: stepAddStandbyInstance},
			{New: stepCheckSyncStatus},

			{New: stepConfigEnvVars},
			{New: stepConfigAutostart},
			{New: stepVerifyExpansion},

			{New: stepCleanupFailedExpansion},
			{New: stepShowClusterStatus},
		},
	})
}

// FirstStepID returns E-001 (or first registry step ID).
func FirstStepID() string {
	return runner.FirstStepID(GetAllSteps(), "E")
}

// StepIDByName returns the registry ID for a step Name.
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}
