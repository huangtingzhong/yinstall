package om_test

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/steps/om"
)

func TestYasomListenPort(t *testing.T) {
	if got := om.YasomListenPort(1688); got != 1675 {
		t.Fatalf("YasomListenPort(1688)=%d want 1675", got)
	}
	if got := om.YasomListenPort(10); got != 0 {
		t.Fatalf("YasomListenPort(10)=%d want 0", got)
	}
}

func TestYasomListenAddr(t *testing.T) {
	addr, err := om.YasomListenAddr("10.10.10.172", 1688)
	if err != nil || addr != "10.10.10.172:1675" {
		t.Fatalf("addr=%q err=%v", addr, err)
	}
	if _, err := om.YasomListenAddr("", 1688); err == nil {
		t.Fatal("expected error for empty ip")
	}
}

func TestValidateOMMigrateParams(t *testing.T) {
	ok, err := om.ValidateOMMigrateParams("", "", "")
	if err != nil || ok {
		t.Fatalf("empty pair: ok=%v err=%v", ok, err)
	}
	ok, err = om.ValidateOMMigrateParams("10.0.0.1", "", "")
	if err == nil || ok {
		t.Fatalf("xor should fail: ok=%v err=%v", ok, err)
	}
	ok, err = om.ValidateOMMigrateParams("", "10.0.0.2", "")
	if err == nil || ok {
		t.Fatalf("om-new without source should fail: ok=%v err=%v", ok, err)
	}
	ok, err = om.ValidateOMMigrateParams("10.0.0.1", "10.0.0.1", "")
	if err == nil || ok {
		t.Fatalf("same ip should fail: ok=%v err=%v", ok, err)
	}
	ok, err = om.ValidateOMMigrateParams("10.0.0.1", "10.0.0.2", "10.0.0.9")
	if err == nil || ok {
		t.Fatalf("om_ip mismatch should fail: ok=%v err=%v", ok, err)
	}
	ok, err = om.ValidateOMMigrateParams("10.0.0.1", "10.0.0.2", "10.0.0.1")
	if err != nil || !ok {
		t.Fatalf("valid migrate: ok=%v err=%v", ok, err)
	}
	// 方案1: --om + --om-new，省略 --om-current
	ok, err = om.ValidateOMMigrateParams("", "10.0.0.2", "10.0.0.1")
	if err != nil || !ok {
		t.Fatalf("om + om-new should migrate: ok=%v err=%v", ok, err)
	}
	// 仅 --om、无 --om-new：不迁主（日常 standby）
	ok, err = om.ValidateOMMigrateParams("", "", "10.0.0.1")
	if err != nil || ok {
		t.Fatalf("om alone should skip migrate: ok=%v err=%v", ok, err)
	}
	if got := om.ResolveOMMigrateCurrent("", "10.0.0.1"); got != "10.0.0.1" {
		t.Fatalf("ResolveOMMigrateCurrent from --om: got %q", got)
	}
	if got := om.ResolveOMMigrateCurrent("10.0.0.3", "10.0.0.1"); got != "10.0.0.3" {
		t.Fatalf("ResolveOMMigrateCurrent prefers current: got %q", got)
	}
}

func sampleYasomStatus() string {
	return `
+----------+------+---------------+---------------------+-----------------------+---------------------+-----------+------------+---------+-------------+
| hostid   | pid  | ipaddr         | primary             | secondary             | local_yasom_addr    | role      | backup_num | max_seq | auto_repair |
+----------+------+---------------+---------------------+-----------------------+---------------------+-----------+------------+---------+-------------+
| host0001 | 1234 | 10.10.10.172  | 10.10.10.172:1675   | [10.10.10.173:1675]   | 10.10.10.172:1675   | primary   | 1          | 100     | on          |
| host0002 | 5678 | 10.10.10.173  | 10.10.10.172:1675   | [10.10.10.173:1675]   | 10.10.10.173:1675   | secondary | 1          | 100     | on          |
| host0003 | -    | 10.10.10.182  | 10.10.10.172:1675   | []                    | -                   | -         | 0          | 0       | off         |
+----------+------+---------------+---------------------+-----------------------+---------------------+-----------+------------+---------+-------------+
`
}

func TestParseYasomStatus(t *testing.T) {
	rows := om.ParseYasomStatus(sampleYasomStatus())
	if len(rows) != 3 {
		t.Fatalf("rows=%d", len(rows))
	}
	pri := om.FindPrimaryRow(rows)
	if pri == nil || pri.IPAddr != "10.10.10.172" || pri.MaxSeq != 100 {
		t.Fatalf("primary=%+v", pri)
	}
	sec := om.FindRowByIP(rows, "10.10.10.173")
	if sec == nil || !strings.EqualFold(sec.Role, "secondary") {
		t.Fatalf("secondary=%+v", sec)
	}
	if !om.HostInCluster(rows, "10.10.10.182") {
		t.Fatal("182 should be in cluster")
	}
	if om.MigrateModeFromStatus(rows, "10.10.10.173") != "m1" {
		t.Fatal("173 should be m1")
	}
	if om.MigrateModeFromStatus(rows, "10.10.10.199") != "m2" {
		t.Fatal("199 should be m2")
	}
}

func TestSecondarySynced(t *testing.T) {
	rows := om.ParseYasomStatus(sampleYasomStatus())
	if err := om.SecondarySynced(rows, "10.10.10.173", "10.10.10.173:1675"); err != nil {
		t.Fatalf("expected synced: %v", err)
	}
	if err := om.SecondarySynced(rows, "10.10.10.182", "10.10.10.182:1675"); err == nil {
		t.Fatal("182 not secondary; want error")
	}
	// max_seq drift
	rows[1].MaxSeq = 99
	if err := om.SecondarySynced(rows, "10.10.10.173", "10.10.10.173:1675"); err == nil {
		t.Fatal("max_seq mismatch should fail")
	}
}

func TestPatchHostsTomlOM(t *testing.T) {
	raw := `
[host]
  hostid = "host0001"

[om]
  hostid = "host0001"
  [om.config]
    LISTEN_ADDR = "10.10.10.172:1675"
    OTHER = "x"

[node]
  id = 1
`
	out, err := om.PatchHostsTomlOM(raw, "host0002", "10.10.10.173:1675")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `hostid = "host0002"`) {
		t.Fatalf("hostid not patched:\n%s", out)
	}
	if !strings.Contains(out, `LISTEN_ADDR = "10.10.10.173:1675"`) {
		t.Fatalf("LISTEN_ADDR not patched:\n%s", out)
	}
	// 非 [om] 段的 hostid 不应被改
	if !strings.Contains(out, `[host]
  hostid = "host0001"`) {
		t.Fatalf("non-om hostid changed:\n%s", out)
	}
}

func TestGetMigrateStepsIDs(t *testing.T) {
	steps := om.GetMigrateSteps()
	if len(steps) != 10 {
		t.Fatalf("migrate steps=%d want 10", len(steps))
	}
	if steps[0].ID != "O-001" || steps[9].ID != "O-010" {
		t.Fatalf("ids=%s..%s", steps[0].ID, steps[9].ID)
	}
	if om.FirstStepID() != "O-001" {
		t.Fatalf("FirstStepID=%s", om.FirstStepID())
	}
	if id := om.StepIDByName("OM Stop Primary"); id != "O-005" {
		t.Fatalf("Stop Primary id=%s", id)
	}
	for _, s := range steps {
		if s.ID == "" || s.Name == "" {
			t.Fatalf("empty id/name: %+v", s)
		}
	}
}

func TestGetDeploySecondaryStepsIDs(t *testing.T) {
	steps := om.GetDeploySecondarySteps()
	if len(steps) != 2 {
		t.Fatalf("deploy steps=%d want 2", len(steps))
	}
	if steps[0].ID != "O-011" || steps[0].Name != "OM Deploy Secondary Gate" {
		t.Fatalf("gate=%s %s", steps[0].ID, steps[0].Name)
	}
	if steps[1].ID != "O-012" || steps[1].Name != "OM Deploy Secondary Host" {
		t.Fatalf("host=%s %s", steps[1].ID, steps[1].Name)
	}
	all := om.GetAllSteps()
	if len(all) != 13 {
		t.Fatalf("all steps=%d want 13", len(all))
	}
	if id := om.StepIDByName("OM Deploy Secondary Host"); id != "O-012" {
		t.Fatalf("deploy host id=%s", id)
	}
	if id := om.StepIDByName("OM Ipchange Yasom"); id != "O-013" {
		t.Fatalf("ipchange id=%s", id)
	}
}

func TestListSecondaryCandidateIPs(t *testing.T) {
	rows := om.ParseYasomStatus(sampleYasomStatus())
	cands := om.ListSecondaryCandidateIPs(rows)
	if len(cands) != 2 {
		t.Fatalf("cands=%v", cands)
	}
	for _, ip := range cands {
		if ip == "10.10.10.172" {
			t.Fatal("primary should be excluded")
		}
	}
}

func TestMigrateRollbackHint(t *testing.T) {
	h := om.MigrateRollbackHint("OM Stop Primary")
	if !strings.Contains(h, "start") {
		t.Fatalf("hint=%q", h)
	}
}

func TestClassifyOMMigrateStatus(t *testing.T) {
	base := om.ParseYasomStatus(sampleYasomStatus())
	if got := om.ClassifyOMMigrateStatus(base, "10.10.10.172", "10.10.10.173"); got != om.OMMigrateProceed {
		t.Fatalf("normal migrate: got %v want Proceed", got)
	}

	// 迁主完成后: NEW 为 primary running, CUR 为 secondary
	done := om.ParseYasomStatus(sampleYasomStatus())
	done[0].Role = "secondary"
	done[0].PID = "1111"
	done[1].Role = "primary"
	done[1].PID = "2222"
	done[1].Primary = "10.10.10.173:1675"
	if got := om.ClassifyOMMigrateStatus(done, "10.10.10.172", "10.10.10.173"); got != om.OMMigrateAlreadyDone {
		t.Fatalf("already done: got %v want AlreadyDone", got)
	}

	// 双主: 两边 primary 且都在跑
	dual := om.ParseYasomStatus(sampleYasomStatus())
	dual[1].Role = "primary"
	if got := om.ClassifyOMMigrateStatus(dual, "10.10.10.172", "10.10.10.173"); got != om.OMMigrateDualPrimary {
		t.Fatalf("dual primary: got %v want DualPrimary", got)
	}

	// CUR 不是 primary
	if got := om.ClassifyOMMigrateStatus(base, "10.10.10.173", "10.10.10.182"); got != om.OMMigrateCurNotPrimary {
		t.Fatalf("cur not primary: got %v want CurNotPrimary", got)
	}

	// stop 后续跑: CUR role=primary 但 pid 已停, NEW 仍是 secondary
	stopped := om.ParseYasomStatus(sampleYasomStatus())
	stopped[0].PID = "-"
	if got := om.ClassifyOMMigrateStatus(stopped, "10.10.10.172", "10.10.10.173"); got != om.OMMigrateProceed {
		t.Fatalf("after stop: got %v want Proceed", got)
	}
}

func TestIsOMStageListingReady(t *testing.T) {
	if om.IsOMStageListingReady(nil) {
		t.Fatal("empty should be false")
	}
	if om.IsOMStageListingReady([]string{"hosts.toml"}) {
		t.Fatal("hosts only should be false")
	}
	if om.IsOMStageListingReady([]string{"database-23.5.2.101-linux-aarch64.tar.gz"}) {
		t.Fatal("pkg only should be false")
	}
	if !om.IsOMStageListingReady([]string{"hosts.toml", "database-23.5.2.101-linux-aarch64.tar.gz"}) {
		t.Fatal("hosts+pkg should be ready")
	}
	names := om.ParseLSNames("hosts.toml\nfoo.toml\ndatabase-x.tar.gz\n")
	if !om.IsOMStageListingReady(names) {
		t.Fatalf("parsed listing should be ready: %v", names)
	}
	if !om.IsOMStageTomlReady([]string{"hosts.toml"}) {
		t.Fatal("toml ready")
	}
	tomlOnly := om.FilterOMStageNames([]string{"hosts.toml", "yashandb.toml", "database-x.tar.gz"}, om.OMStageSyncTOML)
	if len(tomlOnly) != 2 {
		t.Fatalf("toml filter=%v want 2 tomls", tomlOnly)
	}
	full := om.FilterOMStageNames([]string{"hosts.toml", "database-x.tar.gz", "readme.txt"}, om.OMStageSyncFull)
	if len(full) != 2 {
		t.Fatalf("full filter=%v want hosts+pkg", full)
	}
}
