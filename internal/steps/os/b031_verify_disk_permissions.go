package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB031VerifyDiskPermissions Verify disk permissions (YAC)
func StepB031VerifyDiskPermissions() *runner.Step {
	return &runner.Step{
		ID:          "B-031",
		Name:        "Verify Disk Permissions",
		Description: "Verify shared disk permission settings",
		Tags:        []string{"os", "yac", "udev"},
		Optional:    true, // 单机环境下不需要多路径/udev，可以跳过

		PreCheck: func(ctx *runner.StepContext) error {
			// YAC 模式下需要验证磁盘权限
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				return nil
			}

			// 非 YAC 模式：检查是否显式启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			if !enabled && !needMultipath {
				return fmt.Errorf("multipath/udev not enabled and not required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// 检查 /dev/mapper/ 下的多路径设备
			result, _ := ctx.Execute("ls -l /dev/mapper/ 2>/dev/null | grep -E 'sys|data|arch' || echo 'No matching devices in /dev/mapper/'", true)
			ctx.Logger.Info("Multipath devices (/dev/mapper/):\n%s", result.GetStdout())

			// 检查 /dev/yfs/ 下的符号链接（udev 创建）
			yfsResult, _ := ctx.Execute("ls -l /dev/yfs/ 2>/dev/null || echo 'No /dev/yfs/ directory'", true)
			ctx.Logger.Info("YFS device symlinks (/dev/yfs/):\n%s", yfsResult.GetStdout())

			combined := result.GetStdout() + "\n" + yfsResult.GetStdout()
			ctx.SetResult("disk_permissions", combined)
			return nil
		},
	}
}
