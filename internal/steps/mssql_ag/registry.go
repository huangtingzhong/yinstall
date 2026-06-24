package mssql_ag

import (
	"github.com/yinstall/internal/runner"
)

// GetAGAddSteps returns the A-* step sequence for adding replicas to an
// availability group (or rebuilding an existing AG topology). Uses cert-based
// HADR endpoint auth (A-007 + A-008).
func GetAGAddSteps() []*runner.Step {
	return []*runner.Step{
		StepA001ResolveInstance(),
		StepA005HAPreflight(),
		StepA006aUpdateHostsFile(),
		StepA006HAFirewallPrepare(),
		StepA007HADRCertEndpoint(),
		StepA008ExchangeHADRCerts(),
		StepA009HAEndpointPortVerify(),
		StepA010EnableAlwaysOn(),
		StepA011AGCreate(),
		StepA012AddReplica(),
		StepA013Listener(),
		StepA014AddAGDatabases(),
		StepA015PostHAVerify(),
	}
}

// GetAGRemoveSteps returns the A-051..A-053 sequence for tearing down an
// availability group (DROP AG on primary + optional DROP DATABASE on secondary).
func GetAGRemoveSteps() []*runner.Step {
	return []*runner.Step{
		StepA001ResolveInstance(),
		StepA051AGRemovePreflight(),
		StepA052DropAvailabilityGroup(),
		StepA053AGDropSecondaryDB(),
	}
}
