package clean_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yinstall/internal/steps/clean"
	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestNormalizeNodeIDForRemove(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"1-3:3", "1-3"},
		{"1-2:2", "1-2"},
		{"1-1", "1-1"},
		{" 1-3:3 ", "1-3"},
		{"", ""},
	}
	for _, c := range cases {
		if got := clean.NormalizeNodeIDForRemove(c.in); got != c.want {
			t.Fatalf("NormalizeNodeIDForRemove(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestIsPrimaryRole(t *testing.T) {
	t.Parallel()
	if !clean.IsPrimaryRole("primary") || !clean.IsPrimaryRole("PRIMARY") {
		t.Fatal("expected primary")
	}
	if clean.IsPrimaryRole("standby") || clean.IsPrimaryRole("") {
		t.Fatal("expected non-primary")
	}
}

func TestIsBlankOrUnknownRole(t *testing.T) {
	t.Parallel()
	for _, r := range []string{"", "-", " - ", "unknown", "UNKNOWN"} {
		if !clean.IsBlankOrUnknownRole(r) {
			t.Fatalf("expected blank/unknown for %q", r)
		}
	}
	if clean.IsBlankOrUnknownRole("primary") || clean.IsBlankOrUnknownRole("standby") {
		t.Fatal("primary/standby must not be blank/unknown")
	}
}

func TestParseClusterStatusTableCDBPdbRole(t *testing.T) {
	t.Parallel()
	// CDB: 列名为 pdb_role / pdb_status，须映射到 DatabaseRole
	table := `
| hostid   | nodeid | node_type | pdb_name | pid     | instance_status | pdb_status | pdb_role | source_node | listen_address    | data_path |
| host0001 | 1-1:1  | cdb       | cdb$root | 1988979 | open            | normal     | primary  | -           | 10.10.10.130:5188 | /data/a   |
|          |        |           | pdb1     | 1989811 | open            | normal     | primary  | -           |                   |           |
`
	rows := dbsteps.ParseClusterStatusTable(table)
	host, node, role, ok := clean.FindClusterIdentityForIP(rows, "10.10.10.130")
	if !ok || host != "host0001" || node != "1-1:1" {
		t.Fatalf("identity host=%q node=%q ok=%v", host, node, ok)
	}
	if !clean.IsPrimaryRole(role) {
		t.Fatalf("CDB pdb_role=primary not detected; role=%q rows=%+v", role, rows)
	}
}

func TestFindClusterIdentityForIP(t *testing.T) {
	t.Parallel()
	table := `
+----+
| hostid   | node_type | nodeid | pid | instance_status | database_status | database_role | listen_address    | source_node | data_path |
+----+
| host0001 | db        | 1-1:1  | 1   | open            | normal          | primary       | 10.10.10.130:1688 | -           | /data/a   |
| host0003 | db        | 1-3:3  | 2   | open            | normal          | standby       | 10.10.10.135:1688 | 1-1         | /data/b   |
`
	rows := dbsteps.ParseClusterStatusTable(table)
	host, node, role, ok := clean.FindClusterIdentityForIP(rows, "10.10.10.135")
	if !ok || host != "host0003" || node != "1-3:3" || !strings.EqualFold(role, "standby") {
		t.Fatalf("got host=%q node=%q role=%q ok=%v", host, node, role, ok)
	}
	_, _, _, ok = clean.FindClusterIdentityForIP(rows, "10.10.10.199")
	if ok {
		t.Fatal("expected not found")
	}
	if !clean.ClusterStatusContainsIP(table, "10.10.10.130") {
		t.Fatal("expected 130 in cluster")
	}
	if clean.ClusterStatusContainsIP(table, "10.10.10.199") {
		t.Fatal("expected 199 absent")
	}
}

func TestNodeRemoveLooksSuccessful(t *testing.T) {
	t.Parallel()
	okOut := `
| task  | e446 | NodeRemove       | - | yashandb | SUCCESS | 0 | 100 | 9 |
task completed, status: SUCCESS
`
	if !clean.NodeRemoveLooksSuccessful(okOut) {
		t.Fatal("expected success")
	}
	failOut := `NodeRemove FAILED
task completed, status: FAILED`
	if clean.NodeRemoveLooksSuccessful(failOut) {
		t.Fatal("expected not success")
	}
}

func TestYasagentStatusContainsHostID(t *testing.T) {
	t.Parallel()
	out := `
| hostid   | pid | run_user | listen_address |
| host0001 | 1   | yashan   | 10.10.10.130:1676 |
| host0003 | 2   | yashan   | 10.10.10.135:1676 |
`
	if !clean.YasagentStatusContainsHostID(out, "host0003") {
		t.Fatal("expected host0003")
	}
	if clean.YasagentStatusContainsHostID(out, "host0009") {
		t.Fatal("expected missing")
	}
}

func TestResolveClusterIdentityGhostListen(t *testing.T) {
	t.Parallel()
	// 本机 wipe 后 ghost: cluster status listen=-，yasagent 仍有 manage IP
	status := `
| hostid   | node_type | nodeid | pid | instance_status | database_status | database_role | listen_address | source_node | data_path |
| host0001 | db        | 1-1:1  | 1   | open            | normal          | primary       | 10.10.10.130:1688 | - | /data/a |
| host0003 | db        | 1-3:3  | -   | -               | -               | -             | -                 |   | /data/b |
`
	agent := `
| hostid   | pid | run_user | listen_address    | run_path |
| host0001 | 1   | yashan   | 10.10.10.130:1676 | /bin/yasagent |
| host0003 | -   | yashan   | 10.10.10.135:1676 | /bin/yasagent |
`
	rows := dbsteps.ParseClusterStatusTable(status)
	_, _, _, ok := clean.FindClusterIdentityForIP(rows, "10.10.10.135")
	if ok {
		t.Fatal("listen-only match should fail for ghost")
	}
	hid := clean.HostIDFromYasagentListenIP(agent, "10.10.10.135")
	if hid != "host0003" {
		t.Fatalf("HostIDFromYasagentListenIP got %q", hid)
	}
	host, node, role, via, found := clean.ResolveClusterIdentity(rows, agent, "10.10.10.135")
	if !found || host != "host0003" || node != "1-3:3" || via != "yasagent" {
		t.Fatalf("got host=%q node=%q role=%q via=%q found=%v", host, node, role, via, found)
	}
	_ = role
	if !clean.ClusterStatusContainsHostID(status, "host0003") {
		t.Fatal("expected host0003 still in status")
	}
	if clean.ClusterStatusContainsIP(status, "10.10.10.135") {
		t.Fatal("ghost should not match by listen IP")
	}
}

func TestResolveClusterIdentityPreferListen(t *testing.T) {
	t.Parallel()
	status := `
| hostid   | node_type | nodeid | pid | instance_status | database_status | database_role | listen_address | data_path |
| host0003 | db        | 1-3:3  | 1   | open            | normal          | standby       | 10.10.10.135:1688 | /data/b |
`
	agent := `
| hostid   | pid | listen_address |
| host0003 | 1   | 10.10.10.135:1676 |
`
	rows := dbsteps.ParseClusterStatusTable(status)
	_, _, _, via, found := clean.ResolveClusterIdentity(rows, agent, "10.10.10.135")
	if !found || via != "listen" {
		t.Fatalf("expected listen via, got via=%q found=%v", via, found)
	}
}

func TestIsDetachUnavailableError(t *testing.T) {
	t.Parallel()
	if !clean.IsDetachUnavailableError(fmt.Errorf("OM host x has no usable yasboot env (primary environment file not found)")) {
		t.Fatal("expected unavailable")
	}
	if !clean.IsDetachUnavailableError(fmt.Errorf("cannot detach from cluster (no om)")) {
		t.Fatal("expected cannot detach")
	}
	if clean.IsDetachUnavailableError(fmt.Errorf("standby detach requires --db-admin-password")) {
		t.Fatal("password error must not degrade")
	}
	if clean.IsDetachUnavailableError(nil) {
		t.Fatal("nil")
	}
}
