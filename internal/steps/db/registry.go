package db

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 DB 安装 steps（顺序即 registry 顺序；ID 由 BuildSteps 自动赋 C-001..C-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "C",
		Entries: []runner.StepEntry{
			{New: stepCheck},
			{New: stepPortCheck},
			{New: stepHomeCheck},

			{New: stepCreateInstallDir},
			{New: stepCreateDataDirs},
			{New: stepSetDirOwnership},

			{New: stepExtractPackage},
			{New: stepCleanStaleBashrc},

			{New: stepVipCheck},
			{New: stepWriteHosts},
			{New: stepScandnsCheck},
			{New: stepDiskCheck},
			{New: stepScannameCheck},

			{New: stepGenConfig},
			{New: StepSetDBTimezone},
			{New: stepSetCharacterSet},
			{New: stepDisableArchivelog},
			{New: stepConfigureRedo},
			{New: stepSetNativeType},
			{New: stepTuneYfsParams},

			{New: stepInstallSoftware},
			{New: stepDeployDatabase},
			{New: stepCreateArchdg},

			{New: stepSetEnvVars},
			{New: stepCreatePluggableDatabases},
			{New: stepConfigureDefaultProfile},
			{New: stepApplySpfileParams},
			{New: stepConfigureTpcc},
			{New: stepConfigureUnifiedAudit},
			{New: stepExecuteCustomSql},
			{New: stepRestartDatabase},
			{New: stepVerifyInstall},

			{New: stepConfigAutostartScript},
			{New: stepConfigAutostartService},

			{New: stepShowClusterStatus},
		},
	})
}

// FirstStepID returns C-001 (or first registry step ID).
func FirstStepID() string {
	return runner.FirstStepID(GetAllSteps(), "C")
}

// StepIDByName returns the registry ID for a step Name.
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}
