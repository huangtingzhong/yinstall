package mysql_standby

import "github.com/yinstall/internal/runner"

// GetAllSteps returns ordered MySQL standby steps (MR-001 .. MR-NNN).
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "MR",
		Entries: []runner.StepEntry{
			{New: stepCheckPrimaryConnectivity},
			{New: stepCheckPrimaryReplicationReady},
			{New: stepConfigurePrimaryForReplication},
			{New: stepCreateReplicationUser},
			{New: stepInstallClonePluginPrimary},
			{New: stepCheckReplicaConnectivity},
			{New: stepPlanReplicaLayout},
			{New: stepInstallReplicaSoftware},
			{New: stepInstallReplicaInstance},
			{New: stepCopyPatchReplicaCnf},
			{New: stepInstallClonePluginReplica},
			{New: stepReplicationFirewallPrepare},
			{New: stepSyncFromPrimary},
			{New: stepConfigureReplicationSource},
			{New: stepStartReplica},
			{New: stepVerifyReplication},
			{New: stepCleanupFailedReplica},
		},
	})
}

// SemiSyncSteps returns MR-018; not part of default standby flow (MR-016 is Verify Replication).
func SemiSyncSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "MR",
		Entries: []runner.StepEntry{
			{FixedID: "MR-018", New: stepEnableSemiSync},
		},
	})
}
