package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	StageAll      = "all"
	StageSoftware = "software"
)

// ParseStage normalizes stage: all/a, software/s (instance/i removed).
func ParseStage(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", StageAll, "a":
		return StageAll, nil
	case StageSoftware, "s":
		return StageSoftware, nil
	case "instance", "i":
		return "", fmt.Errorf("invalid --stage %q (mssql instance stage removed; use all/a or software/s)", raw)
	default:
		return "", fmt.Errorf("invalid --stage %q (use all/a or software/s)", raw)
	}
}

// DefaultInstallStage is the default for yinstall mssql install.
func DefaultInstallStage() string { return StageAll }

// DefaultCleanStage is the default for yinstall clean --type mssql.
func DefaultCleanStage() string { return StageAll }

// NormalizeCleanStage maps MSSQL clean --stage: all/a → software (full uninstall + yinstall artifacts, keeps ISO).
func NormalizeCleanStage(stage string) string {
	if stage == StageAll {
		return StageSoftware
	}
	return stage
}

// CleanPreservePackageDirs are never removed by cleanup (remote -R only).
func CleanPreservePackageDirs(remoteDir string) []string {
	remoteDir = strings.TrimRight(strings.TrimSpace(remoteDir), `\`)
	if remoteDir == "" {
		return nil
	}
	return []string{remoteDir}
}

func cleanInstanceDataPaths(layout Layout) []string {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		p = strings.TrimRight(strings.TrimSpace(p), `\`)
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		seen[strings.ToLower(p)] = true
		paths = append(paths, p)
	}
	admin := strings.TrimRight(strings.TrimSpace(layout.AdminBase), `\`)
	if admin != "" {
		add(joinWinPath(admin, "Configuration.ini"))
		add(joinWinPath(admin, "setup.pid"))
		add(joinWinPath(admin, toolsEnvFileName))
	}
	if !layout.UseSQLDefaults {
		add(layout.DataDir)
		add(layout.LogDir)
		add(layout.BackupDir)
		base := strings.TrimRight(strings.TrimSpace(layout.Base), `\`)
		if base != "" && !strings.EqualFold(base, admin) {
			add(base)
		}
	}
	return paths
}

// CleanRemovePaths returns paths to delete for an MSSQL cleanup stage.
func CleanRemovePaths(stage string, layout Layout) []string {
	_ = NormalizeCleanStage(stage)
	paths := cleanInstanceDataPaths(layout)
	admin := strings.TrimRight(strings.TrimSpace(layout.AdminBase), `\`)
	if admin != "" {
		paths = append(paths, joinWinPath(admin, setupEnvFileName))
	}
	return paths
}

// StepMatchesInstallStage reports whether an MSSQL step should run for the given install stage.
func StepMatchesInstallStage(step *runner.Step, stage string) bool {
	if stage == StageAll || step == nil {
		return true
	}
	for _, tag := range step.Tags {
		switch tag {
		case "mssql-both":
			return true
		case "mssql-software":
			if stage == StageSoftware {
				return true
			}
		}
	}
	return false
}
