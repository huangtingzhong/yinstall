package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yinstall/internal/logging"
)

func TestCommandStream_EndEmptyMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lg, err := logging.NewLogger("test", dir, "v", "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()

	s := lg.BeginCommandStream("h1", "C-001")
	s.MarkAttached()
	s.End(0, 12*time.Millisecond)

	data, err := os.ReadFile(lg.DebugLogPath())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"exit_code=0",
		"stdout| (empty)",
		"stderr| (empty)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("debug log missing %q\n%s", want, text)
		}
	}
}

func TestCommandStream_OnLineWritesBeforeEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lg, err := logging.NewLogger("test", dir, "v", "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()

	s := lg.BeginCommandStream("h1", "C-001")
	s.MarkAttached()
	s.OnLine("stdout", "tick-1")
	mid, err := os.ReadFile(lg.DebugLogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mid), "stdout| tick-1") {
		t.Fatalf("expected streamed line before End, got:\n%s", mid)
	}
	s.End(0, time.Millisecond)
	end, _ := os.ReadFile(lg.DebugLogPath())
	if strings.Count(string(end), "stdout| tick-1") != 1 {
		t.Fatalf("stdout line should appear once (no full dump), got:\n%s", end)
	}
	if strings.Contains(string(end), "stdout| (empty)") {
		t.Fatal("should not mark stdout empty after lines were streamed")
	}
}

func TestLogCommandOutputLine_writesStream(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lg, err := logging.NewLogger("test", dir, "v", "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	lg.LogCommandOutputLine("h", "G-001", "stderr", "hello")
	b, err := os.ReadFile(filepath.Clean(lg.DebugLogPath()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "stderr| hello") {
		t.Fatalf("unexpected debug content:\n%s", b)
	}
}
