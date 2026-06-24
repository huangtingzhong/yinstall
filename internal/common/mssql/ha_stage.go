package mssql

import (
	"fmt"
	"strings"
)

const (
	// HAStageAll installs replica SQL (matching primary) then configures Mirror/AG.
	HAStageAll = "all"
	// HAStageSoftware installs replica SQL only (no MSH HA steps).
	HAStageSoftware = "software"
	// HAStageHA configures Mirror/AG only; replica SQL must already exist and match primary.
	HAStageHA = "ha"
)

// DefaultHAStage is the default for yinstall mssql ha (one-click install + HA).
func DefaultHAStage() string { return HAStageAll }

// ParseHAStage normalizes mssql ha --stage: all/a, software/s, ha/h.
func ParseHAStage(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", HAStageAll, "a":
		return HAStageAll, nil
	case HAStageSoftware, "s":
		return HAStageSoftware, nil
	case HAStageHA, "h":
		return HAStageHA, nil
	default:
		return "", fmt.Errorf("invalid mssql ha --stage %q (use all/a, software/s, or ha/h)", raw)
	}
}

// HAIncludesSoftwareInstall reports whether ha flow should install SQL on replica(s).
func HAIncludesSoftwareInstall(stage string) bool {
	return stage == HAStageAll || stage == HAStageSoftware
}

// HAIncludesHASetup reports whether ha flow should run MSH-* Mirror/AG steps.
func HAIncludesHASetup(stage string) bool {
	return stage == HAStageAll || stage == HAStageHA
}
