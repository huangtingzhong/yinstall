package standby_test

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/steps/standby"
)

func TestResolveStandbyCEPath(t *testing.T) {
	cases := []struct {
		name        string
		yac, ce     bool
		wantCE      bool
		wantErrPart string
	}{
		{"ce_yac", true, true, true, ""},
		{"se_plain", false, false, false, ""},
		{"ce_no_yac", false, true, false, "requires --yac"},
		{"se_with_yac", true, false, false, "primary is SE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := standby.ResolveStandbyCEPath(tc.yac, tc.ce)
			if tc.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("err=%v want contain %q", err, tc.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantCE {
				t.Fatalf("useCE=%v want %v", got, tc.wantCE)
			}
		})
	}
}

func TestValidateStandbyCEParams(t *testing.T) {
	vips2 := []string{"10.10.10.184/24", "10.10.10.185/24"}
	if err := standby.ValidateStandbyCEParams("10.10.234.0/24", "/dev/a", "/dev/b", vips2, 2); err != nil {
		t.Fatal(err)
	}
	if err := standby.ValidateStandbyCEParams("", "/dev/a", "/dev/b", vips2, 2); err == nil {
		t.Fatal("want inter-cidr required")
	}
	if err := standby.ValidateStandbyCEParams("10.10.234.0/24", "/dev/a", "/dev/b", vips2, 1); err == nil {
		t.Fatal("want vip count mismatch")
	}
	if err := standby.ValidateStandbyCEParams("not-a-cidr", "/dev/a", "/dev/b", vips2, 2); err == nil {
		t.Fatal("want invalid cidr")
	}
}

func TestBuildConfigGroupGenCmd(t *testing.T) {
	cmd := standby.BuildConfigGroupGenCmd(standby.StandbyCEGroupGenParams{
		StageDir:      "/home/yashan/install",
		ClusterName:   "yashandb",
		User:          "yashan",
		Password:      "'Yashan1!'",
		IPs:           "10.10.10.182,10.10.10.183",
		SSHPort:       22,
		InstallPath:   "/data/yashan/yasdb_home",
		DataPath:      "/data/yashan/yasdb_data",
		LogPath:       "/data/yashan/log",
		BeginPort:     1688,
		NodeCount:     2,
		SystemDisks:   "/dev/yfs/sys1,/dev/yfs/sys2,/dev/yfs/sys3",
		DataDisks:     "/dev/yfs/data1,/dev/yfs/data2",
		DiskFoundPath: "/dev/yfs",
		VIPs:          []string{"10.10.10.184/24", "10.10.10.185"}, // 混用带/不带 prefix，应归一化为 /24
		PublicNetwork: "10.10.10.0/24",
		InterCIDR:     "10.10.234.0/24",
	})
	if strings.Contains(cmd, "config node gen") {
		t.Fatalf("must not use node gen: %s", cmd)
	}
	for _, want := range []string{
		"config group gen",
		"-t ce",
		"--system-data /dev/yfs/sys1,/dev/yfs/sys2,/dev/yfs/sys3",
		"--data /dev/yfs/data1,/dev/yfs/data2",
		"--vips 10.10.10.184/24,10.10.10.185/24",
		"--node 2",
		"--ip 10.10.10.182,10.10.10.183",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("cmd missing %q\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "10.10.10.184/24/24") {
		t.Fatalf("double prefix: %s", cmd)
	}
}

func TestPrimaryLooksLikeCE(t *testing.T) {
	if !standby.PrimaryLooksLikeCE(`group_type = "ce"`) {
		t.Fatal("toml ce")
	}
	if !standby.PrimaryLooksLikeCE("| ceg1 | primary |") {
		t.Fatal("ceg1 status")
	}
	if standby.PrimaryLooksLikeCE("| 1-1 | primary | open |") {
		t.Fatal("plain SE-like should be false")
	}
}

func TestEnsureStandbyCEPathIdempotentWhenResolved(t *testing.T) {
	ctx := &runner.StepContext{
		Params: map[string]interface{}{
			"standby_ce_path_resolved": true,
			"standby_ce_path":          true,
			"yac_mode":                 true,
		},
	}
	if err := standby.EnsureStandbyCEPath(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if !ctx.GetParamBool("standby_ce_path", false) {
		t.Fatal("should keep CE path")
	}
}

func TestRequireCEAdminPassword(t *testing.T) {
	if err := standby.RequireCEAdminPassword(""); err == nil {
		t.Fatal("want error for empty password")
	}
	if err := standby.RequireCEAdminPassword("sys"); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSafeCECleanupCommands(t *testing.T) {
	purge, err := standby.BuildSafeCECleanupCommands("yashandb", []string{"ceg2"}, []string{"ceg1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(purge, "\n")
	if !strings.Contains(joined, "--group-ids 2") || !strings.Contains(joined, "--purge") {
		t.Fatalf("purge cmds=%v", purge)
	}
	clean, err := standby.BuildSafeCECleanupCommands("yashandb", []string{"ceg2"}, []string{"ceg1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(clean, "\n"), "--clean") || strings.Contains(strings.Join(clean, "\n"), "--purge") {
		t.Fatalf("failed-scale cmds=%v", clean)
	}
	if strings.Contains(joined, "--group-ids ceg2") {
		t.Fatalf("must normalize to numeric id: %v", purge)
	}
	if _, err := standby.BuildSafeCECleanupCommands("yashandb", []string{"ceg1"}, nil, true); err == nil {
		t.Fatal("want refuse ceg1")
	}
	if _, err := standby.BuildSafeCECleanupCommands("yashandb", []string{"1"}, nil, false); err == nil {
		t.Fatal("want refuse group 1")
	}
	if _, err := standby.BuildSafeCECleanupCommands("yashandb", []string{"ceg2"}, []string{"ceg2"}, true); err == nil {
		t.Fatal("want refuse when standby id is also primary")
	}
	if _, err := standby.BuildSafeCECleanupCommands("yashandb", nil, []string{"ceg1"}, true); err == nil {
		t.Fatal("want error when no group id (no blanket clean)")
	}
}

func TestPredictNextCEGroupName(t *testing.T) {
	if g := standby.PredictNextCEGroupName([]string{"ceg1"}); g != "ceg2" {
		t.Fatalf("got %s", g)
	}
	if g := standby.PredictNextCEGroupName([]string{"ceg1", "ceg2"}); g != "ceg3" {
		t.Fatalf("got %s", g)
	}
}

func TestSelectCEGroupsForFailedCleanup(t *testing.T) {
	got := standby.SelectCEGroupsForFailedCleanup([]string{"ceg2"}, []string{"ceg2", "ceg3"}, "ceg3")
	if len(got) != 1 || got[0] != "ceg3" {
		t.Fatalf("got %v want [ceg3]", got)
	}
	// 禁止清基线备组
	got2 := standby.SelectCEGroupsForFailedCleanup([]string{"ceg2"}, []string{"ceg2"}, "ceg2")
	if len(got2) != 0 {
		t.Fatalf("must not clean baseline: %v", got2)
	}
	// 失败时尚无新 standby 行，仍可按 expected 清
	got3 := standby.SelectCEGroupsForFailedCleanup([]string{"ceg2"}, []string{"ceg2"}, "ceg3")
	if len(got3) != 1 || got3[0] != "ceg3" {
		t.Fatalf("got %v", got3)
	}
}

func TestGroupNameFromAddTOML(t *testing.T) {
	raw := `
[[group]]
  name = "ceg3"
  database_role = "standby"
`
	if g := standby.GroupNameFromAddTOML(raw); g != "ceg3" {
		t.Fatalf("got %s", g)
	}
}

func TestFormatCEGroupRoleSummary(t *testing.T) {
	// 模拟 yasboot -b group 合并单元格（续行 group_name 为空）
	out := `
| group_name | node_type | nodeid | database_role |
+------------+-----------+--------+---------------+
| ceg1       | ce        | 1-1:1  | primary       |
+            +-----------+--------+---------------+
|            | ce        | 1-2:2  | primary       |
+------------+-----------+--------+---------------+
| ceg2       | ce        | 2-1:1  | standby       |
+            +-----------+--------+---------------+
|            | ce        | 2-2:2  | standby       |
+------------+-----------+--------+---------------+
`
	lines := standby.FormatCEGroupRoleSummary(out)
	joined := strings.Join(lines, ";")
	if !strings.Contains(joined, "ceg1=primary (primary_rows=2") || !strings.Contains(joined, "ceg2=standby (primary_rows=0 standby_rows=2)") {
		t.Fatalf("lines=%v", lines)
	}
	prim, stbys := standby.ParseCEGroupNamesByRole(out)
	if len(prim) != 1 || prim[0] != "ceg1" || len(stbys) != 1 || stbys[0] != "ceg2" {
		t.Fatalf("prim=%v stbys=%v", prim, stbys)
	}
}
