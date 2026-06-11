package mysql_standby

import "github.com/yinstall/internal/runner"

// GetAllSteps returns ordered MySQL standby steps MR-001 .. MR-018 and MR-017.
// MR-016 (semi-sync) is opt-in via --enable-semi-sync only; see SemiSyncSteps().
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepMR001CheckPrimaryConnectivity(),
		StepMR002CheckPrimaryReplicationReady(),
		StepMR003ConfigurePrimaryForReplication(),
		StepMR004CreateReplicationUser(),
		StepMR005InstallClonePluginPrimary(),
		StepMR006CheckReplicaConnectivity(),
		StepMR007PlanReplicaLayout(),
		StepMR018InstallReplicaSoftware(),
		StepMR008InstallReplicaInstance(),
		StepMR009CopyPatchReplicaCnf(),
		StepMR010InstallClonePluginReplica(),
		StepMR011SyncFromPrimary(),
		StepMR013ConfigureReplicationSource(),
		StepMR014StartReplica(),
		StepMR015VerifyReplication(),
		StepMR017CleanupFailedReplica(),
	}
}

// SemiSyncSteps returns MR-016; not part of default standby flow.
func SemiSyncSteps() []*runner.Step {
	return []*runner.Step{StepMR016EnableSemiSync()}
}
