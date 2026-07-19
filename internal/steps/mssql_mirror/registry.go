package mssql_mirror

import "github.com/yinstall/internal/runner"

// GetMirrorAddSteps returns the M-* step sequence for adding a mirror partner.
func GetMirrorAddSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "M",
		Entries: []runner.StepEntry{
			{New: stepResolveInstance},
			{New: stepMirrorPreflight},
			{New: stepCompareVersions},
			{New: stepFirewallPrepare},
			{New: stepMirrorCertEndpoint},
			{New: stepPublishCert},
			{New: stepImportCert},
			{New: stepEndpointPortVerify},
			{New: stepBackupSeed},
			{New: stepSetPartner},
			{New: stepPostVerify},
		},
	})
}

// GetMirrorRemoveSteps returns the tear-down sequence (fixed legacy IDs M-051..M-054).
func GetMirrorRemoveSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "M",
		Entries: []runner.StepEntry{
			{New: stepResolveInstance},
			{FixedID: "M-051", New: stepRemovePreflight},
			{FixedID: "M-052", New: stepRemovePartner},
			{FixedID: "M-053", New: stepRecoverSecondary},
			{FixedID: "M-054", New: stepDropSecondaryDb},
		},
	})
}

// Exported step factories for CLI orchestration (cross-package reuse).
func StepResolveInstance() *runner.Step    { return stepResolveInstance() }
func StepCheckPrimary() *runner.Step       { return stepCheckPrimary() }
func StepPlanReplicaInstall() *runner.Step { return stepPlanReplicaInstall() }
func StepInstallReplica() *runner.Step     { return stepInstallReplica() }
