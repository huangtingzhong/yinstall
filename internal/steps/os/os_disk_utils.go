package os

import (
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// GetDiskWWID 获取磁盘的 WWID（兼容性包装）
func GetDiskWWID(ctx *runner.StepContext, disk string) (string, error) {
	return commonos.GetDiskWWID(ctx, disk)
}

// IsMultipathDisk 检查磁盘是否为多路径设备（兼容性包装）
func IsMultipathDisk(disk string) bool {
	return commonos.IsMultipathDisk(disk)
}
