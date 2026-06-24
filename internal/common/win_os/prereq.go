package win_os

import (
	"fmt"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// CheckBasePrerequisites verifies PS 5.1+ and disk space on Windows.
func CheckBasePrerequisites(ctx *runner.StepContext) error {
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "$PSVersionTable.PSVersion.Major"`, false)
	if err != nil {
		return err
	}
	major, _ := strconv.Atoi(strings.TrimSpace(res.GetStdout()))
	if major < 5 {
		return fmt.Errorf("PowerShell 5.1+ required, got major version %d", major)
	}

	res, _ = ctx.Execute(`powershell -NoProfile -Command "(Get-PSDrive C).Free / 1GB"`, false)
	if res != nil {
		free, _ := strconv.ParseFloat(strings.TrimSpace(res.GetStdout()), 64)
		if free > 0 && free < 6 {
			return fmt.Errorf("system drive free space %.1f GB < 6 GB required", free)
		}
	}
	return nil
}

// CheckMssqlPrerequisites adds .NET and existing instance registry scan.
func CheckMssqlPrerequisites(ctx *runner.StepContext) error {
	if err := CheckBasePrerequisites(ctx); err != nil {
		return err
	}
	// .NET 4.7.2+ release key check (simplified)
	res, _ := ctx.Execute(`powershell -NoProfile -Command "(Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\NET Framework Setup\NDP\v4\Full' -ErrorAction SilentlyContinue).Release"`, false)
	if res != nil {
		release, _ := strconv.Atoi(strings.TrimSpace(res.GetStdout()))
		if release > 0 && release < 461808 {
			return fmt.Errorf(".NET 4.7.2+ required (registry release %d)", release)
		}
	}
	instances, _ := ListSQLInstances(ctx)
	if len(instances) > 0 {
		ctx.SetResult("mssql_existing_instances", instances)
	}
	return nil
}

// CheckMySQLPrerequisites adds VC++ check.
func CheckMySQLPrerequisites(ctx *runner.StepContext) error {
	if err := CheckBasePrerequisites(ctx); err != nil {
		return err
	}
	if !commonos.IsVCRedistInstalled(ctx) {
		return fmt.Errorf("VC++ 2015-2022 x64 runtime not installed")
	}
	return nil
}

// ListSQLInstances reads installed instance names from registry.
func ListSQLInstances(ctx *runner.StepContext) ([]string, error) {
	res, err := ctx.Execute(`powershell -NoProfile -Command "Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty PSObject.Properties | Where-Object { $_.Name -notmatch '^PS' } | ForEach-Object { $_.Name }"`, false)
	if err != nil || res == nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(res.GetStdout(), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// TestPendingReboot returns true if reboot is pending.
func TestPendingReboot(ctx *runner.StepContext) bool {
	script := `$pending = $false
if (Get-Item 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending' -ErrorAction SilentlyContinue) { $pending = $true }
if (Get-Item 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired' -ErrorAction SilentlyContinue) { $pending = $true }
if ($pending) { 'true' } else { 'false' }`
	res, _ := ctx.Execute(`powershell -NoProfile -Command "`+script+`"`, false)
	return res != nil && strings.Contains(strings.ToLower(res.GetStdout()), "true")
}
