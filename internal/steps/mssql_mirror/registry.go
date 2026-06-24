package mssql_mirror

import "github.com/yinstall/internal/runner"

// GetMirrorAddSteps returns the M-* step sequence for adding a mirror partner
// (or rebuilding an existing mirror topology).
func GetMirrorAddSteps() []*runner.Step {
	return []*runner.Step{
		StepM001ResolveInstance(),
		StepM005MirrorPreflight(),
		StepM006MirrorCompareVersions(),
		StepM007HAFirewallPrepare(),
		StepM008MirrorCertEndpoint(),
		StepM009MirrorPublishCert(),
		StepM010MirrorImportCert(),
		StepM011HAEndpointPortVerify(),
		StepM012BackupSeed(),
		StepM013MirrorSetPartner(),
		StepM014MirrorPostVerify(),
	}
}

// GetMirrorRemoveSteps returns the M-051..M-054 step sequence for tearing down
// a mirror partnership (SET PARTNER OFF + optional RECOVERY/DROP on secondary).
func GetMirrorRemoveSteps() []*runner.Step {
	return []*runner.Step{
		StepM001ResolveInstance(),
		StepM051MirrorRemovePreflight(),
		StepM052MirrorRemovePartner(),
		StepM053MirrorRecoverSecondary(),
		StepM054MirrorDropSecondaryDB(),
	}
}
