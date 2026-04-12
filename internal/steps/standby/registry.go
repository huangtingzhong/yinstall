// registry.go - 备库扩容步骤注册
// 本文件注册所有备库扩容相关的步骤

package standby

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回所有备库扩容步骤
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepE001CheckPrimaryConnectivity(),
		StepE002CheckPrimaryStatus(),
		StepE003CheckArchiveMode(),
		StepE004CheckReplicationAddr(),

		StepE005CheckStandbyConnectivity(),
		StepE006CheckStandbyBeginPort(),
		StepE007CheckStandbyExpansionPaths(),
		StepE008CheckArchiveDest(),
		StepE009CheckNetworkConnectivity(),
		StepE010CheckAndCleanupExistingNodes(),

		StepE011GenExpansionConfig(),
		StepE012InstallSoftware(),
		StepE013AddStandbyInstance(),
		StepE014CheckSyncStatus(),

		StepE015ConfigEnvVars(),
		StepE016ConfigAutostart(),
		StepE017VerifyExpansion(),

		StepE018CleanupFailedExpansion(),
		StepE019ShowClusterStatus(),
	}
}
