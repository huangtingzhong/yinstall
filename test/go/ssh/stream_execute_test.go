package ssh_test

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yinstall/internal/ssh"
)

func TestLocalExecute_streamsLinesBeforeProcessExit(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	e := &ssh.LocalExecutor{}
	var mu sync.Mutex
	var lines []string
	firstAt := make(chan time.Time, 1)

	e.SetOutputLineHandler(func(stream, line string) {
		mu.Lock()
		lines = append(lines, stream+":"+line)
		n := len(lines)
		mu.Unlock()
		if n == 1 {
			select {
			case firstAt <- time.Now():
			default:
			}
		}
	})

	cmd := `python3 -c 'import time; print("one", flush=True); time.sleep(0.35); print("two", flush=True)'`
	start := time.Now()
	done := make(chan struct{})
	var res *ssh.ExecResult
	go func() {
		defer close(done)
		var err error
		res, err = e.Execute(cmd, false)
		if err != nil {
			t.Errorf("Execute error: %v", err)
		}
	}()

	var tFirst time.Time
	select {
	case tFirst = <-firstAt:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first streamed stdout line")
	}

	<-done
	elapsedToFirst := tFirst.Sub(start)
	if elapsedToFirst > 250*time.Millisecond {
		// first line should arrive well before the 350ms sleep completes
		t.Fatalf("first line arrived too late (%v); streaming may still be buffered until exit", elapsedToFirst)
	}

	mu.Lock()
	got := append([]string(nil), lines...)
	mu.Unlock()
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "stdout:one") || !strings.Contains(joined, "stdout:two") {
		t.Fatalf("unexpected lines: %v", got)
	}
	if res == nil || !strings.Contains(res.Stdout, "one") || !strings.Contains(res.Stdout, "two") {
		t.Fatalf("ExecResult.Stdout incomplete: %+v", res)
	}
}

func TestBindOutputLineHandler_localAttached(t *testing.T) {
	t.Parallel()
	e := &ssh.LocalExecutor{}
	called := false
	clear, attached := ssh.BindOutputLineHandler(e, func(stream, line string) {
		called = true
		_ = stream
		_ = line
	})
	if !attached {
		t.Fatal("LocalExecutor should support BindOutputLineHandler")
	}
	clear()
	if called {
		t.Fatal("handler should not run without Execute")
	}
}
