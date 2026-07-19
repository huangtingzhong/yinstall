package mssql_ag

import "github.com/yinstall/internal/runner"

// GetAGAddSteps returns the A-* step sequence for adding replicas to an AG.
func GetAGAddSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "A",
		Entries: []runner.StepEntry{
			{New: stepResolveInstance},
			{New: stepAgPreflight},
			{New: stepUpdateHostsFile},
			{New: stepFirewallPrepare},
			{New: stepHadrCertEndpoint},
			{New: stepExchangeCerts},
			{New: stepEndpointPortVerify},
			{New: stepEnableAlwaysOn},
			{New: stepCreateAg},
			{New: stepJoinAg},
			{New: stepListener},
			{New: stepAddDatabases},
			{New: stepPostVerify},
		},
	})
}

// GetAGRemoveSteps returns the tear-down sequence (fixed legacy IDs for remove flow).
func GetAGRemoveSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "A",
		Entries: []runner.StepEntry{
			{New: stepResolveInstance},
			{FixedID: "A-051", New: stepRemovePreflight},
			{FixedID: "A-052", New: stepDropAg},
			{FixedID: "A-053", New: stepDropSecondaryDb},
		},
	})
}

// Exported step factories for CLI orchestration (cross-package reuse).
func StepResolveInstance() *runner.Step    { return stepResolveInstance() }
func StepCheckPrimary() *runner.Step       { return stepCheckPrimary() }
func StepPlanReplicaInstall() *runner.Step { return stepPlanReplicaInstall() }
func StepInstallReplica() *runner.Step     { return stepInstallReplica() }
