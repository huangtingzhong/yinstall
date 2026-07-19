package mssql

import (
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// GetAllSteps returns MSSQL install steps MS-001..MS-NNN in registry order.
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "MS",
		Entries: []runner.StepEntry{
			{New: stepPlatformTransportDetect},
			{New: stepResolveInstance},
			{New: stepSqlInstallPreflight},
			{New: stepPortCheck},
			{New: stepResolveSetupMedia},
			{New: stepPrepareDirectories},
			{New: stepDistributeSetupMedia},
			{New: stepWriteSetupSoftwareEnv},
			{New: stepGenerateConfigurationIni},
			{New: stepInstallInstance},
			{New: stepConfigureTcpPort},
			{New: stepEnsureSqlServices},
			{New: stepApplyCuSp},
			{New: stepServiceAccount},
			{New: stepWriteSqlToolsEnv},
			{New: stepSetSaPassword},
			{New: stepRemoteSaAccess},
			{New: stepConfigureMaxMemory},
			{New: stepWriteInstanceProfile},
			{New: stepVerifyInstallation},
			{New: stepCustomSql},
		},
	})
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
