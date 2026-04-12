package os

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// areRequiredPackagesInstalledForMount checks if packages are installed (shared with b012)
func areRequiredPackagesInstalledForMount(ctx *runner.StepContext) bool {
	return areRequiredPackagesInstalled(ctx)
}

// StepB013MountISO Mount ISO (optional)
func StepB013MountISO() *runner.Step {
	return &runner.Step{
		ID:          "B-013",
		Name:        "Mount ISO",
		Description: "Mount local ISO file for YUM source",
		Tags:        []string{"os", "yum"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			if yumMode != "local-iso" {
				return fmt.Errorf("yum mode is not local-iso")
			}

			// 检查必需的软件包是否已安装，如果都已安装则跳过
			if areRequiredPackagesInstalledForMount(ctx) {
				return fmt.Errorf("all required packages already installed, skipping ISO mount")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			device := ctx.GetParamString("os_iso_device", "/dev/cdrom")
			mountpoint := ctx.GetParamString("os_iso_mountpoint", "/media")

			// 检查是否已挂载
			result, _ := ctx.Execute(fmt.Sprintf("mountpoint -q %s 2>/dev/null", mountpoint), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Mount point %s already mounted, skipping", mountpoint)
				return nil
			}

			// 确定 ISO 来源
			var isoPath string
			var err error

			if commonfile.IsDevicePath(device) {
				// 设备路径，直接使用
				isoPath = device
				ctx.Logger.Info("Using device: %s", device)
			} else {
				// ISO 文件，需要查找和分发
				ctx.Logger.Info("Looking for ISO file: %s", device)

				// 查找并分发 ISO 文件
				isoPath, err = commonfile.FindAndDistribute(
					ctx,
					device,
					ctx.LocalSoftwareDirs,
					ctx.RemoteSoftwareDir,
				)
				if err != nil {
					return fmt.Errorf("ISO file not found: %w", err)
				}
				ctx.Logger.Info("Found ISO file: %s", isoPath)
			}

			// 创建挂载点
			ctx.Execute(fmt.Sprintf("mkdir -p %s", mountpoint), true)

			// 挂载
			var mountCmd string
			if commonfile.IsDevicePath(isoPath) {
				// 设备挂载
				mountCmd = fmt.Sprintf("mount -t iso9660 %s %s", isoPath, mountpoint)
			} else {
				// ISO 文件挂载（需要 loop 选项）
				mountCmd = fmt.Sprintf("mount -o loop %s %s", isoPath, mountpoint)
			}

			ctx.Logger.Info("Mounting: %s", mountCmd)
			_, err = ctx.ExecuteWithCheck(mountCmd, true)
			if err != nil {
				return fmt.Errorf("failed to mount: %w", err)
			}

			// 存储实际使用的 ISO 路径
			ctx.SetResult("iso_path", isoPath)

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			mountpoint := ctx.GetParamString("os_iso_mountpoint", "/media")

			// 检查是否挂载成功
			result, err := ctx.Execute(fmt.Sprintf("mountpoint -q %s", mountpoint), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("mount point %s is not mounted", mountpoint)
			}

			// 检查是否可读
			result, err = ctx.Execute(fmt.Sprintf("ls %s >/dev/null 2>&1", mountpoint), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("mount point %s is not readable", mountpoint)
			}

			// 列出挂载点内容
			result, _ = ctx.Execute(fmt.Sprintf("ls %s | head -5", mountpoint), false)
			if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
				ctx.Logger.Info("Mount point contents: %s", strings.Replace(strings.TrimSpace(result.GetStdout()), "\n", ", ", -1))
			}

			return nil
		},
	}
}
