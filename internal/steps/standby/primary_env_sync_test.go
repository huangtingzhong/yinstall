package standby

import (
	"strings"
	"testing"
	"time"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

type stubExecResult struct {
	stdout string
	exit   int
}

func (s *stubExecResult) GetStdout() string          { return s.stdout }
func (s *stubExecResult) GetStderr() string          { return "" }
func (s *stubExecResult) GetExitCode() int           { return s.exit }
func (s *stubExecResult) GetDuration() time.Duration { return 0 }

type stubExecutor struct {
	catStdout string
}

func (e *stubExecutor) Execute(cmd string, sudo bool) (runner.ExecResult, error) {
	if strings.HasPrefix(cmd, "cat ") {
		return &stubExecResult{stdout: e.catStdout, exit: 0}, nil
	}
	return &stubExecResult{exit: 0}, nil
}
func (e *stubExecutor) Host() string                              { return "stub" }
func (e *stubExecutor) Close() error                              { return nil }
func (e *stubExecutor) Upload(localPath, remotePath string) error { return nil }

func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	l, err := logging.NewLogger("test", t.TempDir(), "v", "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestSyncPrimaryClusterNameFromEnvFile_explicitFailsOnBadContent(t *testing.T) {
	t.Parallel()
	ctx := &runner.StepContext{
		Executor: &stubExecutor{catStdout: "export PATH=/usr/bin\n"},
		Logger:   testLogger(t),
		Params: map[string]interface{}{
			"primary_env_file": ".port3988",
		},
	}
	err := SyncPrimaryClusterNameFromEnvFile(ctx, "/home/yashan/.port3988")
	if err == nil {
		t.Fatal("expected error when primary_env_file set but content not parseable")
	}
}

func TestSyncPrimaryClusterNameFromEnvFile_noExplicitSkipsUnparseable(t *testing.T) {
	t.Parallel()
	ctx := &runner.StepContext{
		Executor: &stubExecutor{catStdout: "export PATH=/usr/bin\n"},
		Logger:   testLogger(t),
		Params: map[string]interface{}{
			"db_cluster_name": "yashandb",
		},
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, "/home/yashan/.bashrc"); err != nil {
		t.Fatal(err)
	}
	if ctx.Params["db_cluster_name"] != "yashandb" {
		t.Fatalf("params unchanged, got %v", ctx.Params["db_cluster_name"])
	}
}

func TestSyncPrimaryClusterNameFromEnvFile_parsesWithoutPrimaryEnvFileParam(t *testing.T) {
	t.Parallel()
	content := "source /home/yashan/.yasboot/yashandb_3988_yasdb_home/conf/yashandb_3988.bashrc\n"
	ctx := &runner.StepContext{
		Executor: &stubExecutor{catStdout: content},
		Logger:   testLogger(t),
		Params: map[string]interface{}{
			"db_cluster_name": "yashandb",
		},
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, "/home/yashan/.port3988"); err != nil {
		t.Fatal(err)
	}
	if ctx.Params["db_cluster_name"] != "yashandb_3988" {
		t.Fatalf("got %v", ctx.Params["db_cluster_name"])
	}
}
