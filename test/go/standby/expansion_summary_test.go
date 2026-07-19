package standby_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yinstall/internal/steps/db"
	"github.com/yinstall/internal/steps/standby"
)

const sampleClusterStatus = `
========== Cluster Status ==========
+----------------------------------------------------------------------------------------------------------------------------------------------------------------+
| hostid   | node_type | nodeid | pid     | instance_status | database_status | database_role | listen_address    | source_node | data_path                      |
+----------------------------------------------------------------------------------------------------------------------------------------------------------------+
| host0001 | db        | 1-1:1  | 1725265 | open            | normal          | primary       | 10.10.10.130:1688 | -           | /data/yashan/yasdb_data/db-1-1 |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+-------------+--------------------------------+
| host0002 | db        | 1-2:2  | 220343  | open            | normal          | standby       | 10.10.10.131:1688 | 1-1         | /data/yashan/yasdb_data/db-1-2 |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+-------------+--------------------------------+
| host0003 | db        | 1-3:3  | 466696  | mounted         | normal          | standby       | 10.10.10.135:1688 | unknown     | /data/yashan/yasdb_data/db-1-3 |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+-------------+--------------------------------+
=====================================
`

const sampleClusterStatusAllOpen = `
| hostid   | node_type | nodeid | pid     | instance_status | database_status | database_role | listen_address    | source_node | data_path                      |
| host0001 | db        | 1-1:1  | 1725265 | open            | normal          | primary       | 10.10.10.130:1688 | -           | /data/yashan/yasdb_data/db-1-1 |
| host0002 | db        | 1-2:2  | 220343  | open            | normal          | standby       | 10.10.10.131:1688 | 1-1         | /data/yashan/yasdb_data/db-1-2 |
| host0003 | db        | 1-3:3  | 466696  | open            | normal          | standby       | 10.10.10.135:1688 | 1-1         | /data/yashan/yasdb_data/db-1-3 |
`

func TestParseClusterStatusTableMounted(t *testing.T) {
	rows := db.ParseClusterStatusTable(sampleClusterStatus)
	if len(rows) != 3 {
		t.Fatalf("rows=%d want 3", len(rows))
	}
	pending := standby.PendingStandbyOpenNodes(rows, nil)
	if len(pending) != 1 || pending[0] != "1-3:3" {
		t.Fatalf("PendingStandbyOpenNodes=%v want [1-3:3]", pending)
	}
}

func TestPendingStandbyOpenNodesStartedTarget(t *testing.T) {
	const startedStatus = `
| hostid   | node_type | nodeid | pid    | instance_status | database_status | database_role | listen_address    | source_node | data_path |
| host0001 | db        | 1-1:1  | 1      | open            | normal          | primary       | 10.10.10.130:1688 | -           | /data/a   |
| host0003 | db        | 1-3:3  | 494008 | started         | -               | -             | 10.10.10.135:1688 | -           | /data/c   |
`
	rows := db.ParseClusterStatusTable(startedStatus)
	pending := standby.PendingStandbyOpenNodes(rows, []string{"10.10.10.135"})
	if len(pending) != 1 || pending[0] != "1-3:3" {
		t.Fatalf("PendingStandbyOpenNodes=%v want [1-3:3]", pending)
	}
	if got := standby.StandbyRowHealthLabel(rows[1]); got != "WARN" {
		t.Fatalf("started target label=%s want WARN", got)
	}
}

func TestStandbyRowHealthLabel(t *testing.T) {
	rows := db.ParseClusterStatusTable(sampleClusterStatus)
	want := map[string]string{
		"1-1:1": "OK",
		"1-2:2": "OK",
		"1-3:3": "WARN",
	}
	for _, r := range rows {
		got := standby.StandbyRowHealthLabel(r)
		if got != want[r.Nodeid] {
			t.Fatalf("node %s label=%s want %s", r.Nodeid, got, want[r.Nodeid])
		}
	}
	openRows := db.ParseClusterStatusTable(sampleClusterStatusAllOpen)
	for _, r := range openRows {
		if standby.StandbyRowHealthLabel(r) != "OK" {
			t.Fatalf("open row %s label=%s want OK", r.Nodeid, standby.StandbyRowHealthLabel(r))
		}
	}
}

func TestPollStandbyOpenUntilReadyOpens(t *testing.T) {
	calls := 0
	sleepCalls := 0
	final, still, timedOut, err := standby.PollStandbyOpenUntilReadyForTargets(
		sampleClusterStatus,
		nil,
		func() (string, error) {
			calls++
			if calls >= 2 {
				return sampleClusterStatusAllOpen, nil
			}
			return sampleClusterStatus, nil
		},
		5,
		func(time.Duration) { sleepCalls++ },
		time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("poll err: %v", err)
	}
	if timedOut {
		t.Fatal("expected not timed out")
	}
	if len(still) != 0 {
		t.Fatalf("stillPending=%v", still)
	}
	if calls != 2 || sleepCalls != 2 {
		t.Fatalf("calls=%d sleepCalls=%d", calls, sleepCalls)
	}
	if !strings.Contains(final, "10.10.10.135:1688") {
		t.Fatalf("final output missing standby listen")
	}
	if len(standby.PendingStandbyOpenNodes(db.ParseClusterStatusTable(final), nil)) != 0 {
		t.Fatal("expected no pending after open")
	}
}

func TestPollStandbyOpenUntilReadyTimeout(t *testing.T) {
	calls := 0
	final, still, timedOut, err := standby.PollStandbyOpenUntilReadyForTargets(
		sampleClusterStatus,
		nil,
		func() (string, error) {
			calls++
			return sampleClusterStatus, nil
		},
		3,
		func(time.Duration) {},
		time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("poll err: %v", err)
	}
	if !timedOut {
		t.Fatal("expected timedOut")
	}
	if len(still) != 1 || still[0] != "1-3:3" {
		t.Fatalf("stillPending=%v", still)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
	if strings.TrimSpace(final) == "" {
		t.Fatal("expected final status output")
	}
}

func TestPrimaryListenPortFromStatus(t *testing.T) {
	t.Parallel()
	const diffPortStatus = `
| hostid   | node_type | nodeid | pid | instance_status | database_status | database_role | listen_address    | source_node | data_path |
| host0001 | db        | 1-1:1  | 1   | open            | normal          | primary       | 10.10.10.130:6688 | -           | /data/a   |
| host0002 | db        | 1-2:2  | 2   | open            | normal          | standby       | 10.10.10.131:6788 | 1-1         | /data/b   |
`
	rows := db.ParseClusterStatusTable(diffPortStatus)
	if got := standby.PrimaryListenPortFromStatus(rows, "10.10.10.130", 6788); got != 6688 {
		t.Fatalf("PrimaryListenPortFromStatus=%d want 6688 (not standby begin-port fallback)", got)
	}
	if got := standby.ListenPortFromAddress("10.10.10.131:6788"); got != 6788 {
		t.Fatalf("ListenPortFromAddress=%d want 6788", got)
	}
	if got := standby.PrimaryListenPortFromStatus(nil, "", 6988); got != 6988 {
		t.Fatalf("empty rows fallback=%d want 6988", got)
	}
}

func TestClusterHasIPListenPort(t *testing.T) {
	t.Parallel()
	const status = `
| hostid   | node_type | nodeid | pid | instance_status | database_status | database_role | listen_address    | source_node | data_path |
| host0001 | db        | 1-1:1  | 1   | open            | normal          | primary       | 10.10.10.130:7288 | -           | /data/a   |
| host0002 | db        | 1-2:2  | 2   | open            | normal          | standby       | 10.10.10.131:7388 | 1-1         | /data/b   |
`
	if !standby.ClusterHasIP(status, "10.10.10.131") {
		t.Fatal("expected 131 in cluster")
	}
	if !standby.ClusterHasIPListenPort(status, "10.10.10.131", 7388) {
		t.Fatal("expected 131:7388")
	}
	if standby.ClusterHasIPListenPort(status, "10.10.10.131", 7488) {
		t.Fatal("7488 should not exist yet")
	}
}
