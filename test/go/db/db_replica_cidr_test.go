package db_test

import (
	"strings"
	"testing"

	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestReplicaPort(t *testing.T) {
	if got := dbsteps.ReplicaPort(1688, false); got != 1689 {
		t.Fatalf("standalone: got %d want 1689", got)
	}
	if got := dbsteps.ReplicaPort(1688, true); got != 1690 {
		t.Fatalf("yac: got %d want 1690", got)
	}
	if got := dbsteps.ReplicaPort(2688, true); got != 2690 {
		t.Fatalf("yac custom begin: got %d want 2690", got)
	}
}

func TestBuildYACCeGenCommandReplicaCIDR(t *testing.T) {
	base := dbsteps.YACCeGenParams{
		StageDir:      "/home/yashan/install",
		YasbootPath:   "yasboot",
		ClusterName:   "yashandb",
		User:          "yashan",
		Password:      "pass",
		IPs:           "10.10.10.172,10.10.10.173",
		SSHPort:       22,
		InstallPath:   "/data/yashan/yasdb_home",
		DataPath:      "/data/yashan/yasdb_data",
		LogPath:       "/data/yashan/log",
		BeginPort:     1688,
		NodeCount:     2,
		InterCIDR:     "10.10.234.0/24",
		PublicNetwork: "10.10.10.0/24",
		AccessMode:    "direct",
		DiskFoundPath: "/dev/yfs/",
		SystemDisks:   "/dev/yfs/sys1",
		DataDisks:     "/dev/yfs/data1",
	}

	empty := dbsteps.BuildYACCeGenCommand(base)
	if strings.Contains(empty, "--replica-cidr") {
		t.Fatalf("empty ReplicaCIDR must omit --replica-cidr; got:\n%s", empty)
	}

	base.ReplicaCIDR = "10.10.234.0/24"
	with := dbsteps.BuildYACCeGenCommand(base)
	if !strings.Contains(with, "--replica-cidr 10.10.234.0/24") {
		t.Fatalf("want --replica-cidr 10.10.234.0/24 in:\n%s", with)
	}
}

func TestAppendReplicaCIDRFlag(t *testing.T) {
	cmd := "yasboot package se gen --begin-port 1688"
	if got := dbsteps.AppendReplicaCIDRFlag(cmd, ""); got != cmd {
		t.Fatalf("empty: got %q", got)
	}
	got := dbsteps.AppendReplicaCIDRFlag(cmd, "10.10.234.0/24")
	want := cmd + " \\\n--replica-cidr 10.10.234.0/24"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureReplicationAddrInTOMLContent(t *testing.T) {
	in := `
[[group]]
  [[group.node]]
    [group.node.config]
      LISTEN_ADDR = "10.10.10.172:1688"
      CLUSTER_INTERCONNECT = ":1689"
  [[group.node]]
    [group.node.config]
      LISTEN_ADDR = "10.10.10.173:1688"
`
	out, err := dbsteps.EnsureReplicationAddrInTOMLContent(in, 1690)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, `REPLICATION_ADDR = ":1690"`) != 2 {
		t.Fatalf("want 2 REPLICATION_ADDR lines, got:\n%s", out)
	}

	// 已有公网形式时覆盖为端口形式
	in2 := strings.ReplaceAll(in, `LISTEN_ADDR = "10.10.10.172:1688"`,
		`REPLICATION_ADDR = "10.10.10.172:1690"`+"\n"+`      LISTEN_ADDR = "10.10.10.172:1688"`)
	out2, err := dbsteps.EnsureReplicationAddrInTOMLContent(in2, 1690)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out2, "10.10.10.172:1690") {
		t.Fatalf("old full REPLICATION_ADDR should be replaced:\n%s", out2)
	}
	if strings.Count(out2, `REPLICATION_ADDR = ":1690"`) != 2 {
		t.Fatalf("want 2 port-only lines:\n%s", out2)
	}

	legacy := "[db]\nLISTEN_ADDR = \"10.10.10.1:1688\"\n"
	out3, err := dbsteps.EnsureReplicationAddrInTOMLContent(legacy, 1689)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out3, `REPLICATION_ADDR = ":1689"`) {
		t.Fatalf("legacy [db]:\n%s", out3)
	}
}

func TestReplicationAddrAlreadyCorrect(t *testing.T) {
	ok := `
[[group]]
  [[group.node]]
    [group.node.config]
      LISTEN_ADDR = "10.10.10.172:1688"
      REPLICATION_ADDR = ":1690"
  [[group.node]]
    [group.node.config]
      LISTEN_ADDR = "10.10.10.173:1688"
      REPLICATION_ADDR = ":1690"
`
	if !dbsteps.ReplicationAddrAlreadyCorrect(ok, 1690) {
		t.Fatal("want already correct")
	}
	if dbsteps.ReplicationAddrAlreadyCorrect(ok, 1689) {
		t.Fatal("wrong port must be false")
	}

	// yasboot 可能写出的行序不同，但值已正确 → skip
	reordered := `
[[group]]
  [[group.node]]
    [group.node.config]
      REPLICATION_ADDR = ":1689"
      LISTEN_ADDR = "10.10.10.157:1688"
`
	if !dbsteps.ReplicationAddrAlreadyCorrect(reordered, 1689) {
		t.Fatal("reordered but correct should skip rewrite")
	}

	missing := `
[[group]]
  [[group.node]]
    [group.node.config]
      LISTEN_ADDR = "10.10.10.172:1688"
  [[group.node]]
    [group.node.config]
      LISTEN_ADDR = "10.10.10.173:1688"
      REPLICATION_ADDR = ":1690"
`
	if dbsteps.ReplicationAddrAlreadyCorrect(missing, 1690) {
		t.Fatal("incomplete per-node REPLICATION_ADDR must be false")
	}

	publicFull := `
[[group]]
  [[group.node]]
    [group.node.config]
      REPLICATION_ADDR = "10.10.10.172:1690"
  [[group.node]]
    [group.node.config]
      REPLICATION_ADDR = "10.10.10.173:1690"
`
	if dbsteps.ReplicationAddrAlreadyCorrect(publicFull, 1690) {
		t.Fatal("full IP:port form must not count as already correct")
	}
}
