package win_os

import (
	"fmt"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// DataMountPath returns os_local_mount or os_product_base.
func DataMountPath(ctx *runner.StepContext) string {
	if p := strings.TrimSpace(ctx.GetParamString("os_local_mount", "")); p != "" {
		return p
	}
	return strings.TrimSpace(ctx.GetParamString("os_product_base", ""))
}

// DataDriveLetter derives drive letter from mount path.
func DataDriveLetter(ctx *runner.StepContext) string {
	if l := commonos.DriveLetterFromPath(DataMountPath(ctx)); l != "" {
		return l
	}
	return "D:"
}

// PrepareDataVolume initializes disk(s) and assigns drive letter on Windows.
func PrepareDataVolume(ctx *runner.StepContext) error {
	disks := ctx.GetParamStringSlice("os_local_disks")
	letter := strings.TrimSuffix(DataDriveLetter(ctx), ":")
	label := ctx.GetParamString("os_local_volume_label", "SQLData")
	if len(disks) == 0 {
		return fmt.Errorf("no os_local_disks specified")
	}
	for _, d := range disks {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, err := strconv.Atoi(d); err != nil {
			return fmt.Errorf("invalid disk number %q (Windows expects numeric disk id)", d)
		}
		script := fmt.Sprintf(`
$disk = Get-Disk -Number %s -ErrorAction Stop
if ($disk.PartitionStyle -eq 'RAW') { Initialize-Disk -Number %s -PartitionStyle GPT -Confirm:$false }
$part = Get-Partition -DiskNumber %s -ErrorAction SilentlyContinue | Where-Object { $_.DriveLetter -eq '%s' } | Select-Object -First 1
if (-not $part) {
  $part = New-Partition -DiskNumber %s -UseMaximumSize -DriveLetter %s
  Format-Volume -Partition $part -FileSystem NTFS -NewFileSystemLabel '%s' -Confirm:$false | Out-Null
}
`, d, d, d, letter, d, letter, label)
		ctx.LogScriptPreview("powershell", "W-006 prepare disk "+d, script)
		if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+strings.ReplaceAll(script, "\n", " ")+`"`, false); err != nil {
			return fmt.Errorf("disk %s: %w", d, err)
		}
	}
	return nil
}

// VolumeExists checks if drive letter path exists.
func VolumeExists(ctx *runner.StepContext, letter string) bool {
	letter = strings.TrimSuffix(strings.TrimSpace(letter), ":")
	if letter == "" {
		return false
	}
	res, _ := ctx.Execute(fmt.Sprintf(`powershell -NoProfile -Command "Test-Path '%s:\'"`, letter), false)
	return res != nil && strings.Contains(strings.ToLower(res.GetStdout()), "true")
}
