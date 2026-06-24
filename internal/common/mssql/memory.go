package mssql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// MinMaxServerMemoryMB is the minimum allowed max server memory (MB).
	MinMaxServerMemoryMB = 512
	// MinOSReserveMB is RAM left for the OS when sizing from percent.
	MinOSReserveMB = 1024
)

// ComputeMaxServerMemoryMB resolves max server memory from explicit MB or percent of total RAM.
// Returns (0, false, nil) when both explicitMB and memoryPercent are unset (skip configure).
func ComputeMaxServerMemoryMB(totalRAMBytes uint64, explicitMB, memoryPercent int) (int, bool, error) {
	if explicitMB > 0 {
		if explicitMB < MinMaxServerMemoryMB {
			return 0, false, fmt.Errorf("max server memory %d MB below minimum %d MB", explicitMB, MinMaxServerMemoryMB)
		}
		return explicitMB, true, nil
	}
	if memoryPercent <= 0 {
		return 0, false, nil
	}
	if memoryPercent > 100 {
		return 0, false, fmt.Errorf("memory percent %d out of range (1-100)", memoryPercent)
	}
	totalMB := int(totalRAMBytes / (1024 * 1024))
	if totalMB <= 0 {
		return 0, false, fmt.Errorf("invalid total physical memory")
	}
	maxMB := totalMB * memoryPercent / 100
	reserve := osReserveMB(totalMB)
	capMB := totalMB - reserve
	if capMB < MinMaxServerMemoryMB {
		capMB = MinMaxServerMemoryMB
	}
	if maxMB > capMB {
		maxMB = capMB
	}
	if maxMB < MinMaxServerMemoryMB {
		return 0, false, fmt.Errorf("computed max server memory %d MB below minimum %d MB (total RAM %d MB, percent %d)",
			maxMB, MinMaxServerMemoryMB, totalMB, memoryPercent)
	}
	return maxMB, true, nil
}

func osReserveMB(totalMB int) int {
	reserve := MinOSReserveMB
	if totalMB > 8192 {
		pct := totalMB / 10
		if pct > reserve {
			reserve = pct
		}
	}
	return reserve
}

// WindowsTotalMemoryBytes reads physical RAM from the target host.
func WindowsTotalMemoryBytes(ctx *runner.StepContext) (uint64, error) {
	if ctx == nil || ctx.Executor == nil {
		return 0, fmt.Errorf("no executor")
	}
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory"`, false)
	if err != nil {
		return 0, fmt.Errorf("read physical memory: %w", err)
	}
	raw := strings.TrimSpace(res.GetStdout())
	if raw == "" {
		return 0, fmt.Errorf("empty physical memory probe output")
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse physical memory %q: %w", raw, err)
	}
	return n, nil
}

// ConfigureMaxMemorySQL returns T-SQL to set max server memory (MB).
func ConfigureMaxMemorySQL(maxMB int) string {
	return fmt.Sprintf(
		"EXEC sp_configure 'show advanced options', 1; RECONFIGURE; EXEC sp_configure 'max server memory (MB)', %d; RECONFIGURE;",
		maxMB,
	)
}
