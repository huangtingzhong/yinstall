package om_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	"github.com/yinstall/internal/steps/om"
)

// stubOMExec 记录命令；为 OM migrate PreCheck 提供假 status / test 结果。
type stubOMExec struct {
	mu   sync.Mutex
	cmds []string
}

func (e *stubOMExec) Host() string  { return "10.10.10.172" }
func (e *stubOMExec) IsLocal() bool { return true }
func (e *stubOMExec) Close() error  { return nil }
func (e *stubOMExec) Upload(string, string, *ssh.UploadContext) error {
	return nil
}
func (e *stubOMExec) Download(string, string) error { return nil }
func (e *stubOMExec) ExecuteScript(script string, sudo bool) (runner.ExecResult, error) {
	return e.Execute(script, sudo)
}

func (e *stubOMExec) Execute(cmd string, _ bool) (runner.ExecResult, error) {
	e.mu.Lock()
	e.cmds = append(e.cmds, cmd)
	e.mu.Unlock()

	out := ""
	exit := 0
	switch {
	case strings.Contains(cmd, "whoami"):
		out = "root\n"
	case strings.Contains(cmd, "yasboot process yasom status"):
		out = `
+----------+------+---------------+---------------------+-----------------------+---------------------+-----------+------------+---------+-------------+
| hostid   | pid  | ipaddr         | primary             | secondary             | local_yasom_addr    | role      | backup_num | max_seq | auto_repair |
+----------+------+---------------+---------------------+-----------------------+---------------------+-----------+------------+---------+-------------+
| host0001 | 1234 | 10.10.10.172  | 10.10.10.172:1675   | [10.10.10.173:1675]   | 10.10.10.172:1675   | primary   | 1          | 100     | on          |
| host0002 | 5678 | 10.10.10.173  | 10.10.10.172:1675   | [10.10.10.173:1675]   | 10.10.10.173:1675   | secondary | 1          | 100     | on          |
+----------+------+---------------+---------------------+-----------------------+---------------------+-----------+------------+---------+-------------+
`
	case strings.Contains(cmd, "test -d") || strings.Contains(cmd, "test -f"):
		out = ""
		exit = 0
	case strings.Contains(cmd, "yasboot process yasom stop"),
		strings.Contains(cmd, "yasboot process yasom recover"),
		strings.Contains(cmd, "yasboot process yasom sync"),
		strings.Contains(cmd, "yasboot host add"),
		strings.Contains(cmd, "yasboot config node gen"):
		// 破坏性/安装类命令: 若被 Action 调用仍返回成功, 由测试断言「不应出现」
		out = "ok\n"
	default:
		out = ""
		exit = 0
	}
	return &ssh.ExecResult{
		Command:  cmd,
		Stdout:   out,
		ExitCode: exit,
	}, nil
}

func (e *stubOMExec) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.cmds))
	copy(out, e.cmds)
	return out
}

func newOMTestLogger(t *testing.T) *logging.Logger {
	t.Helper()
	lg, err := logging.NewLogger("om-mode-"+t.Name(), t.TempDir(), "v", "t", "c")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { lg.Close() })
	return lg
}

func omMigrateTestParams() map[string]interface{} {
	return map[string]interface{}{
		"om_current":       "10.10.10.172",
		"om_new":           "10.10.10.173",
		"om_ip":            "10.10.10.172",
		"primary_os_user":  "yashan",
		"os_user":          "yashan",
		"db_cluster_name":  "yashandb",
		"db_port":          1688,
		"db_begin_port":    1688,
		"db_stage_dir":     "/home/yashan/install",
		"os_user_password": "secret",
	}
}

func runOMMigrateStepsInMode(t *testing.T, precheck, dryRun bool) *stubOMExec {
	t.Helper()
	exec := &stubOMExec{}
	lg := newOMTestLogger(t)
	params := omMigrateTestParams()
	results := map[string]interface{}{}
	ctxBase := func() *runner.StepContext {
		return &runner.StepContext{
			Executor: exec,
			Logger:   lg,
			Params:   params,
			Results:  results,
			Precheck: precheck,
			DryRun:   dryRun,
		}
	}

	for _, step := range om.GetMigrateSteps() {
		ctx := ctxBase()
		ctx.CurrentStepID = step.ID
		res := runner.RunStep(step, ctx)
		if res == nil {
			t.Fatalf("%s: nil result", step.Name)
		}
		// OM Sync：fixture 中 om_new 仍为 secondary；precheck/dry-run 必须失败（不得假绿）
		if step.Name == "OM Sync" {
			if res.Success {
				t.Fatalf("OM Sync unexpectedly succeeded in mode precheck=%v dryRun=%v; expected fail when new host is not primary", precheck, dryRun)
			}
			if res.Error == nil || !strings.Contains(res.Error.Error(), "not primary") {
				t.Fatalf("OM Sync expected not-primary precheck error, got: %v", res.Error)
			}
			continue
		}
		if !res.Success {
			t.Fatalf("%s failed in mode precheck=%v dryRun=%v: %v", step.Name, precheck, dryRun, res.Error)
		}
	}
	return exec
}

func assertNoDestructiveOMCmds(t *testing.T, cmds []string) {
	t.Helper()
	for _, c := range cmds {
		low := strings.ToLower(c)
		if strings.Contains(low, "yasom stop") ||
			strings.Contains(low, "yasom recover") ||
			strings.Contains(low, "yasom sync") ||
			strings.Contains(low, "host add") ||
			strings.Contains(low, "config node gen") {
			t.Fatalf("destructive/install OM command ran under precheck/dry-run:\n%s", c)
		}
	}
}

func TestOMMigrateSteps_PrecheckSkipsDestructiveActions(t *testing.T) {
	exec := runOMMigrateStepsInMode(t, true, false)
	assertNoDestructiveOMCmds(t, exec.snapshot())
}

func TestOMMigrateSteps_DryRunSkipsDestructiveActions(t *testing.T) {
	exec := runOMMigrateStepsInMode(t, false, true)
	assertNoDestructiveOMCmds(t, exec.snapshot())
}

func TestOMMigrateSteps_MarkStopAndRecoverDangerous(t *testing.T) {
	want := map[string]bool{
		"OM Stop Primary":    false,
		"OM Recover Primary": false,
	}
	for _, s := range om.GetMigrateSteps() {
		if _, ok := want[s.Name]; ok {
			if !s.Dangerous {
				t.Fatalf("%s should be Dangerous", s.Name)
			}
			want[s.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing migrate step %s", name)
		}
	}
}

func TestValidateOMMigrateParams_EmptyMeansNoMigrate(t *testing.T) {
	// 验收: 两参数空时不迁主 (standby 零回归入口)
	ok, err := om.ValidateOMMigrateParams("", "", "")
	if err != nil || ok {
		t.Fatalf("empty pair should skip migrate: ok=%v err=%v", ok, err)
	}
	ok, err = om.ValidateOMMigrateParams("", "", "10.0.0.1")
	if err != nil || ok {
		t.Fatalf("om alone should skip migrate: ok=%v err=%v", ok, err)
	}
}

func TestOMMigrateGate_AlreadyDoneClassificationFast(t *testing.T) {
	// 纯函数门禁不依赖 SSH; 保证收口用例不被慢路径拖垮
	start := time.Now()
	rows := om.ParseYasomStatus(sampleYasomStatus())
	rows[0].Role = "secondary"
	rows[1].Role = "primary"
	if got := om.ClassifyOMMigrateStatus(rows, "10.10.10.172", "10.10.10.173"); got != om.OMMigrateAlreadyDone {
		t.Fatalf("got %v", got)
	}
	if time.Since(start) > time.Second {
		t.Fatal("classification unexpectedly slow")
	}
}
