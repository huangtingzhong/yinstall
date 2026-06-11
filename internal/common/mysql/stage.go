package mysql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	StageAll      = "all"
	StageSoftware = "software"
	StageInstance = "instance"
)

// ParseStage normalizes install/clean stage: all/a, software/s, instance/i.
func ParseStage(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", StageAll, "a":
		return StageAll, nil
	case StageSoftware, "s":
		return StageSoftware, nil
	case StageInstance, "i":
		return StageInstance, nil
	default:
		return "", fmt.Errorf("invalid --stage %q (use all/a, software/s, or instance/i)", raw)
	}
}

// DefaultInstallStage is the default for yinstall mysql install.
func DefaultInstallStage() string { return StageAll }

// DefaultStandbyStage is the default for yinstall mysql standby.
func DefaultStandbyStage() string { return StageAll }

// StandbyIncludesSoftwareInstall reports whether standby should install replica binaries.
func StandbyIncludesSoftwareInstall(stage string) bool {
	return stage == StageAll || stage == StageSoftware
}

// StandbyIncludesReplicationSetup reports whether standby should configure replication (instance + sync).
func StandbyIncludesReplicationSetup(stage string) bool {
	return stage == StageAll || stage == StageInstance
}

// DefaultCleanStage is the default for yinstall clean --type mysql.
func DefaultCleanStage() string { return StageInstance }

// OradataPortDir returns {base}/oradata/{port}.
func OradataPortDir(base string, port int) string {
	return fmt.Sprintf("%s/oradata/%d", strings.TrimRight(base, "/\\"), port)
}

// BuildRoot returns source build tree root for a version.
func BuildRoot(base, version string) string {
	return fmt.Sprintf("%s/build/%s", strings.TrimRight(base, "/\\"), version)
}

// StepMatchesInstallStage reports whether a MySQL step should run for the given install stage.
func StepMatchesInstallStage(step *runner.Step, stage string) bool {
	if stage == StageAll || step == nil {
		return true
	}
	for _, tag := range step.Tags {
		switch tag {
		case "mysql-both":
			return true
		case "mysql-software":
			if stage == StageSoftware {
				return true
			}
		case "mysql-instance":
			if stage == StageInstance {
				return true
			}
		}
	}
	return false
}

// CleanRemovePaths returns directory paths to delete for a cleanup stage.
func CleanRemovePaths(stage string, layout Layout) []string {
	switch stage {
	case StageSoftware:
		var paths []string
		if strings.TrimSpace(layout.Home) != "" {
			paths = append(paths, layout.Home)
		}
		if strings.TrimSpace(layout.Version) != "" {
			paths = append(paths, BuildRoot(layout.Base, layout.Version))
		}
		return paths
	case StageAll:
		if strings.TrimSpace(layout.Base) != "" {
			return []string{layout.Base}
		}
		return nil
	default: // instance
		return []string{OradataPortDir(layout.Base, layout.Port)}
	}
}
