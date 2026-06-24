package mssql

import (
	"fmt"
	"strconv"
	"strings"
)

// MinWS2016Build is minimum Windows Server build for workgroup WSFC (14393 = WS2016 RTM).
const MinWS2016Build = 14393

// OSBuildCheckPowerShell returns WMI build number query.
func OSBuildCheckPowerShell() string {
	return `(Get-CimInstance Win32_OperatingSystem).BuildNumber`
}

// ParseOSBuildNumber parses build number from PowerShell stdout.
func ParseOSBuildNumber(stdout string) (int, error) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		return n, nil
	}
	return 0, fmt.Errorf("cannot parse OS build number from: %q", strings.TrimSpace(stdout))
}

// ValidateOSBuild ensures build >= minBuild.
func ValidateOSBuild(build, minBuild int) error {
	if build < minBuild {
		return fmt.Errorf("Windows build %d < required %d (Windows Server 2016+ required for workgroup WSFC)", build, minBuild)
	}
	return nil
}

// WSFCClusterNamePowerShell returns existing cluster name or empty.
func WSFCClusterNamePowerShell() string {
	return `Get-Cluster -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name`
}
