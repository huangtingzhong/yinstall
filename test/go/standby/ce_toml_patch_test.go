package standby_test

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/steps/standby"
)

func TestPatchStandbyCEAddTOML(t *testing.T) {
	raw := `
[[group]]
  group_type = "ce"
  name = "ceg2"
  database_role = "primary"
  public_network = "10.10.10.0/24"
  data_path = "/wrong/path"
  log_path = "/wrong/log"

  CLUSTER_INTERCONNECT = "10.10.10.182:1689"
  INTER_URL = "10.10.10.182:1788"
  REPLICATION_ADDR = "10.10.10.182:1690"
  LISTEN_ADDR = "10.10.10.182:1688"

[[host]]
  hostid = "host0003"
  hostip = "10.10.10.182"
`
	out, err := standby.PatchStandbyCEAddTOML(raw, standby.StandbyCETomlPatchOpt{
		InterCIDR:     "10.10.234.0/24",
		PublicNetwork: "10.10.10.0/24",
		DataPath:      "/data/yashan/yasdb_data",
		LogPath:       "/data/yashan/log",
		ReplicaPort:   1690,
		InterPort:     1689,
		InterURLPort:  1788,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `database_role = "standby"`) {
		t.Fatalf("role not standby:\n%s", out)
	}
	if !strings.Contains(out, `data_path = "/data/yashan/yasdb_data"`) {
		t.Fatalf("data_path:\n%s", out)
	}
	if !strings.Contains(out, `CLUSTER_INTERCONNECT = "10.10.234.182:1689"`) {
		t.Fatalf("interconnect:\n%s", out)
	}
	if !strings.Contains(out, `REPLICATION_ADDR = "10.10.234.182:1690"`) {
		t.Fatalf("replication:\n%s", out)
	}
	if !strings.Contains(out, "[host.yasdb_ip]") || !strings.Contains(out, "inter_ip") {
		t.Fatalf("yasdb_ip missing:\n%s", out)
	}
}

func TestPlanPrimaryReplicationAddrs(t *testing.T) {
	p, err := standby.PlanPrimaryReplicationAddrs("host0001", "1-1", "10.10.10.172", "", "10.10.234.0/24", 1690)
	if err != nil {
		t.Fatal(err)
	}
	if p.Addr != "10.10.234.172:1690" {
		t.Fatalf("addr=%s", p.Addr)
	}
	p2, err := standby.PlanPrimaryReplicationAddrs("host0001", "1-1", "10.10.10.172", "10.10.234.172", "10.10.234.0/24", 1690)
	if err != nil || p2.Addr != "10.10.234.172:1690" {
		t.Fatalf("p2=%v err=%v", p2, err)
	}
}

func TestParseReplicationAddrValue(t *testing.T) {
	addr, ok := standby.ParseReplicationAddrValue("REPLICATION_ADDR                                                 10.10.234.172:1690")
	if !ok || addr != "10.10.234.172:1690" {
		t.Fatalf("got %q ok=%v", addr, ok)
	}
}

func TestPatchStandbyCEAddTOML_YFSConvert(t *testing.T) {
	raw := `
[[group]]
  group_type = "ce"
  name = "ceg2"
  database_role = "primary"

[[group.diskgroup]]
  name = "WRONG"
  path = "/dev/yfs/data1"

[[group.systemdiskgroup]]
  name = "SYSTEM"
  path = "/dev/yfs/sys1"

[group.node.config]
      ARCHIVE_LOCAL_DEST = "+WRONG/arch_files"
      DB_FILE_NAME_CONVERT = "'+OLD/dbfiles','+WRONG/dbfiles'"
      DB_FLASHBACK_FILE_DEST = "+DG0/fra"
`
	// 单 data 组：对齐主库 DG0 + REDO CONVERT；清除残留 DB_FILE_NAME_CONVERT
	out, err := standby.PatchStandbyCEAddTOML(raw, standby.StandbyCETomlPatchOpt{
		ApplyYFSPatch:       true,
		DataDiskgroupName:   "DG0",
		ArchiveLocalDest:    "+DG0/arch_files",
		RedoFileNameConvert: "'+REDO/dbfiles','+DG0/dbfiles'",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `name = "DG0"`) {
		t.Fatalf("diskgroup name:\n%s", out)
	}
	if !strings.Contains(out, `name = "SYSTEM"`) {
		t.Fatalf("systemdiskgroup must stay SYSTEM:\n%s", out)
	}
	if !strings.Contains(out, `ARCHIVE_LOCAL_DEST = "+DG0/arch_files"`) {
		t.Fatalf("archive:\n%s", out)
	}
	if strings.Contains(out, "DB_FILE_NAME_CONVERT") {
		t.Fatalf("DB_FILE_NAME_CONVERT should be cleared when empty:\n%s", out)
	}
	if !strings.Contains(out, `REDO_FILE_NAME_CONVERT = "'+REDO/dbfiles','+DG0/dbfiles'"`) {
		t.Fatalf("redo convert:\n%s", out)
	}

	// 多 data 组：写入 DB_FILE_NAME_CONVERT（DG1→DG0）
	withDB, err := standby.PatchStandbyCEAddTOML(raw, standby.StandbyCETomlPatchOpt{
		ApplyYFSPatch:       true,
		DataDiskgroupName:   "DG0",
		ArchiveLocalDest:    "+DG0/arch_files",
		DBFileNameConvert:   "'+DG1/dbfiles','+DG0/dbfiles'",
		RedoFileNameConvert: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withDB, `DB_FILE_NAME_CONVERT = "'+DG1/dbfiles','+DG0/dbfiles'"`) {
		t.Fatalf("db convert:\n%s", withDB)
	}
	if strings.Contains(withDB, "REDO_FILE_NAME_CONVERT") {
		t.Fatalf("redo convert should be absent:\n%s", withDB)
	}

	cleared, err := standby.PatchStandbyCEAddTOML(out, standby.StandbyCETomlPatchOpt{
		ApplyYFSPatch:       true,
		DataDiskgroupName:   "DG0",
		ArchiveLocalDest:    "+DG0/arch_files",
		RedoFileNameConvert: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cleared, "REDO_FILE_NAME_CONVERT") {
		t.Fatalf("redo convert should be removed:\n%s", cleared)
	}
}

func TestPatchStandbyCEHostsAddTOML(t *testing.T) {
	raw := `
[[host]]
  hostid = "host0003"
  user = "yashan"
  ip = "10.10.10.182"
  port = 22
  path = "/data/stb/yasdb_home/23.4.14.100"
  log_path = "/data/stb/log"
  [host.yasagent]
    [host.yasagent.config]
      LISTEN_ADDR = "10.10.10.182:1676"
`
	out, err := standby.PatchStandbyCEHostsAddTOML(raw, "10.10.234.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[host.yasdb_ip]") || !strings.Contains(out, "10.10.234.182") {
		t.Fatalf("hosts patch:\n%s", out)
	}
	ipIdx := strings.Index(out, `ip = "10.10.10.182"`)
	yasdbIdx := strings.Index(out, "[host.yasdb_ip]")
	agentIdx := strings.Index(out, "[host.yasagent]")
	if ipIdx < 0 || yasdbIdx < 0 || agentIdx < 0 || !(ipIdx < yasdbIdx && yasdbIdx < agentIdx) {
		t.Fatalf("yasdb_ip must be after host scalars and before yasagent:\n%s", out)
	}
}

func TestPublicNetworkFromTOML(t *testing.T) {
	got := standby.PublicNetworkFromTOML(`public_network = "10.10.10.0/24"`)
	if got != "10.10.10.0/24" {
		t.Fatalf("got %q", got)
	}
	if standby.PublicNetworkFromTOML("nope") != "" {
		t.Fatal("want empty")
	}
}

func TestResolveNodeInterIP(t *testing.T) {
	ip, err := standby.ResolveNodeInterIP("host0001", "10.10.10.172", "10.10.234.0/24", "10.10.234.172", nil)
	if err != nil || ip != "10.10.234.172" {
		t.Fatalf("toml prefer: %s err=%v", ip, err)
	}
	ip2, err := standby.ResolveNodeInterIP("host0001", "10.10.10.172", "10.10.234.0/24", "", []string{"10.10.10.172", "10.10.234.99"})
	if err != nil || ip2 != "10.10.234.99" {
		t.Fatalf("probe prefer: %s err=%v", ip2, err)
	}
	ip3, err := standby.ResolveNodeInterIP("host0001", "10.10.10.172", "10.10.234.0/24", "", nil)
	if err != nil || ip3 != "10.10.234.172" {
		t.Fatalf("map fallback: %s err=%v", ip3, err)
	}
}
