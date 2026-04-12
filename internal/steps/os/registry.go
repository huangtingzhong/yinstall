package os

import (
	"github.com/yinstall/internal/runner"
)

// GetAllSteps Get all OS baseline steps
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
		// Local disk setup
		StepB020SetupLocalDisk(),
		// YAC auto-discover shared disks (runs before B-022 when diskgroups not configured)
		StepB021AutoDiscoverSharedDisks(),
		// YAC diskgroup validation (must run before multipath steps)
		// B-022 检测到非多路径磁盘时会自动设置 yac_multipath_enable=true
		StepB022ValidateYACDiskgroups(),
		// Hostname configuration
		StepB023SetHostname(),
		// YAC multipath related (runs only when needed, auto-enabled by B-022)
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
