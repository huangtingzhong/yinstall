// registry.go - collect 步骤注册表
package collect

import (
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
)

// GetAllSteps 返回按执行顺序排列的全部 collect 步骤（R-001..R-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "R",
		Entries: []runner.StepEntry{
			{New: stepCheckConnectivity},
			{New: stepInitArchive},
			{New: stepSnapshotInstall},
			{New: stepDiscoverEnv},
			{New: stepHostIdentity},
			{New: stepDmidecode},
			{New: stepUserLimits},
			{New: stepKernelParams},
			{New: stepTimeNtp},
			{New: stepNetworkInterfaces},
			{New: stepNetworkRoutes},
			{New: stepFirewall},
			{New: stepPackages},
			{New: stepStorage},
			{New: stepDbPaths},
			{New: stepDbConfig},
			{New: stepDbFilesystem},
			{New: stepDbClusterStatus},
			{New: stepDbProcesses},
			{New: stepDbAutostart},
			{New: stepDbSql},
			{New: stepDbConfigDrift},
			{New: stepRules},
			{New: stepDbLogs},
			{New: stepYacCluster},
			{New: stepSessionLogs},
			{New: stepFinalize},
		},
	})
}

func stepCheckConnectivity() *runner.Step {
	return runner.CloneStep(ossteps.StepCheckConnectivity())
}
