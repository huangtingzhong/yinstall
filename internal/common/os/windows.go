package os

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// IsWindowsTarget reports whether the remote shell is Windows (OpenSSH cmd/PowerShell).
func IsWindowsTarget(ctx *runner.StepContext) bool {
	res, _ := ctx.Execute(`cmd /c echo windows_probe`, false)
	if res != nil && strings.Contains(res.GetStdout(), "windows_probe") {
		return true
	}
	res, _ = ctx.Execute(`powershell -NoProfile -Command "Write-Output windows_probe"`, false)
	return res != nil && strings.Contains(res.GetStdout(), "windows_probe")
}

// DetectWindowsOSInfo fills basic OSInfo for Windows targets.
func DetectWindowsOSInfo(ctx *runner.StepContext) *runner.OSInfo {
	osInfo := &runner.OSInfo{Name: "Windows", ID: "windows", PkgManager: "none"}
	res, _ := ctx.Execute(`powershell -NoProfile -Command "[Environment]::OSVersion.VersionString"`, false)
	if res != nil {
		osInfo.Version = strings.TrimSpace(res.GetStdout())
	}
	res, _ = ctx.Execute(`powershell -NoProfile -Command "if ($env:PROCESSOR_ARCHITECTURE -eq 'AMD64') { 'x86_64' } else { $env:PROCESSOR_ARCHITECTURE }"`, false)
	if res != nil && strings.TrimSpace(res.GetStdout()) != "" {
		osInfo.Arch = strings.TrimSpace(res.GetStdout())
	} else {
		osInfo.Arch = "x86_64"
	}
	return osInfo
}

// WindowsMemoryGB returns total physical memory (best effort, e.g. "16G" or raw bytes string).
func WindowsMemoryGB(ctx *runner.StepContext) string {
	res, _ := ctx.Execute(`powershell -NoProfile -Command "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory"`, false)
	if res == nil {
		return ""
	}
	raw := strings.TrimSpace(res.GetStdout())
	if raw == "" {
		return ""
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%.0fG", float64(n)/(1024*1024*1024))
}

// WindowsLogicalCPUs returns logical CPU count on Windows.
func WindowsLogicalCPUs(ctx *runner.StepContext) string {
	res, _ := ctx.Execute(`powershell -NoProfile -Command "(Get-CimInstance Win32_ComputerSystem).NumberOfLogicalProcessors"`, false)
	if res == nil {
		return ""
	}
	return strings.TrimSpace(res.GetStdout())
}

// IsVCRedistInstalled checks whether VC++ 2015-2022 x64 runtime is present on Windows.
func IsVCRedistInstalled(ctx *runner.StepContext) bool {
	cmd := `powershell -NoProfile -Command "Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Installed -ErrorAction SilentlyContinue"`
	res, _ := ctx.Execute(cmd, false)
	if res != nil && strings.TrimSpace(res.GetStdout()) == "1" {
		return true
	}
	cmd2 := `powershell -NoProfile -Command "Test-Path 'C:\Windows\System32\vcruntime140.dll'"`
	res2, _ := ctx.Execute(cmd2, false)
	return res2 != nil && strings.Contains(strings.ToLower(res2.GetStdout()), "true")
}
