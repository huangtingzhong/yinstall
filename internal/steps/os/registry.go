package os

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 OS 基线 steps（ID 由 BuildSteps 自动赋 B-001..B-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "B",
		Entries: []runner.StepEntry{
			{New: stepCheckConnectivity},
			{New: stepCreateGroup},
			{New: stepCreateUser},
			{New: stepSetPassword},
			{New: stepConfigureSudoers},
			{New: stepConfigureUmask},
			{New: stepSetTimezone},
			{New: stepWriteSysctl},
			{New: stepApplySysctl},
			{New: stepWriteLimits},
			{New: stepConfigureHugepages},
			{New: stepKernelArgs},
			{New: stepMountIso},
			{New: stepWriteYumRepo},
			{New: stepInstallDeps},
			{New: stepConfigureChrony},
			{New: stepDisableFirewall},
			{New: stepOpenFirewallPorts},
			{New: stepRebootCheck},
			{New: stepSetupLocalDisk},
			{New: stepAutoDiscoverSharedDisks},
			{New: stepValidateYacDiskgroups},
			{New: stepSetHostname},
			{New: stepInstallMultipath},
			{New: stepCollectWwid},
			{New: stepWriteMultipathConf},
			{New: stepEnableMultipathd},
			{New: stepVerifyMultipath},
			{New: stepWriteUdevRules},
			{New: stepTriggerUdev},
			{New: stepVerifyDiskPermissions},
			{New: stepConfigureSelinux},
		},
	})
}

// StepCheckConnectivity 供 collect/stress 等域包装复用的连通性步骤（ID 由调用方 registry 赋值）。
func StepCheckConnectivity() *runner.Step {
	return stepCheckConnectivity()
}

// FirstStepID returns B-001 (or first registry step ID).
func FirstStepID() string {
	return runner.FirstStepID(GetAllSteps(), "B")
}

// StepsByName returns steps whose Name matches any of the given names, in registry order.
func StepsByName(names ...string) []*runner.Step {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var out []*runner.Step
	for _, s := range GetAllSteps() {
		if s != nil && want[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// StepIDByName returns the registry ID for a step Name.
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}

// StepSetHostname is exported for cross-package reuse (mysql write hosts).
func StepSetHostname() *runner.Step {
	return stepSetHostname()
}
