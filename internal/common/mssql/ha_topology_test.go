package mssql

import (
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestInstanceNameFromReplicaServerName(t *testing.T) {
	if got := InstanceNameFromReplicaServerName(`WIN-5M9N51AQ3FH\SQL2`); got != "SQL2" {
		t.Fatalf("got %q", got)
	}
	if got := InstanceNameFromReplicaServerName(`WIN-HOST`); got != DefaultInstance {
		t.Fatalf("got %q", got)
	}
}

func TestHATopologyHostsIncludesAutoDiscovered(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"mssql_primary_host":  "10.0.0.1",
			"mssql_replica_hosts": []string{"10.0.0.3"},
			agTopologyHostsParam:  []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
	}
	hosts := HATopologyHosts(ctx)
	if len(hosts) != 3 {
		t.Fatalf("got %v", hosts)
	}
}
