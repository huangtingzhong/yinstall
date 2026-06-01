// Package archive provides default output directory layout and cross-platform packing
// for collect/stressos result trees.
package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TimestampFormat is used for default output subdirectory names (e.g. 20260528150405).
const TimestampFormat = "20060102150405"

// ResolveOutputDir returns the directory to write collect/stress results.
// explicit is the value of --output; when empty, DefaultOutputDir is used.
func ResolveOutputDir(explicit, kind string) (dir string, usedTempFallback bool, err error) {
	if explicit != "" {
		dir, err = resolveExplicitOutput(explicit)
		return dir, false, err
	}
	return DefaultOutputDir(kind)
}

func resolveExplicitOutput(explicit string) (string, error) {
	if filepath.IsAbs(explicit) {
		return filepath.Clean(explicit), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return filepath.Clean(filepath.Join(cwd, explicit)), nil
}

// DefaultOutputDir prefers <cwd>/output/<kind>/<timestamp>.
// If the cwd output tree is not writable, falls back to <TempDir>/output/<kind>/<timestamp>
// (TempDir is /tmp on Linux, $TMPDIR on macOS, %TEMP% on Windows).
func DefaultOutputDir(kind string) (dir string, usedTempFallback bool, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}
	ts := time.Now().Format(TimestampFormat)

	primaryBase := filepath.Join(cwd, "output")
	if canCreateUnder(primaryBase) {
		return filepath.Join(primaryBase, kind, ts), false, nil
	}

	tempBase := filepath.Join(os.TempDir(), "output")
	if !canCreateUnder(tempBase) {
		return "", true, fmt.Errorf("cannot create output under %q or temp %q", primaryBase, tempBase)
	}
	return filepath.Join(tempBase, kind, ts), true, nil
}

// canCreateUnder checks that base can be created and is writable (mkdir + temp file probe).
func canCreateUnder(base string) bool {
	if err := os.MkdirAll(base, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(base, ".yinstall-write-")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// EnsureOutputDir creates dir (and parents) after ResolveOutputDir.
func EnsureOutputDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", dir, err)
	}
	return nil
}
