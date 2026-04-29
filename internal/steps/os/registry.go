package os

import (
	"github.com/yinstall/internal/runner"
)

// GetAllSteps 返回全部 OS 基线 steps（执行顺序即切片顺序）
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepB001CheckConnectivity(),
		StepB002CreateGroup(),
		StepB003CreateUser(),
		StepB004SetUserPassword(),
		StepB005ConfigureSudoers(),
		StepB006ConfigureUmask(),
		StepB007SetTimezone(),
		StepB008WriteSysctlConfig(),
		StepB009ApplySysctl(),
		StepB010WriteLimitsConfig(),
		StepB011ConfigureHugepages(),
		StepB012WriteKernelArgs(),
		StepB013MountISO(),
		StepB014WriteYumRepo(),
		StepB015InstallDeps(),
		StepB016ConfigureChrony(),
		StepB017DisableFirewall(),
		StepB018OpenFirewallPorts(),
		StepB019RebootCheck(),
		// 本地磁盘/目录准备
		StepB020SetupLocalDisk(),
		// YAC 自动发现共享盘（未配置 diskgroups 时先于 B-022 执行）
		StepB021AutoDiscoverSharedDisks(),
		// YAC diskgroup 校验（须先于 multipath 相关步骤）
		// B-022 检测到非多路径磁盘时会自动设置 yac_multipath_enable=true
		StepB022ValidateYACDiskgroups(),
		// 主机名配置
		StepB023SetHostname(),
		// YAC multipath 相关（按需执行；B-022 可自动打开）
		StepB024InstallMultipath(),
		StepB025CollectWWID(),
		StepB026WriteMultipathConf(),
		StepB027EnableMultipathd(),
		StepB028VerifyMultipath(),
		StepB029WriteUdevRules(),
		StepB030TriggerUdev(),
		StepB031VerifyDiskPermissions(),
	}
}
