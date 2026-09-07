package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// IsMultipathDisk 检查磁盘是否为多路径设备
// 支持：/dev/mapper/*, /dev/dm-*, /dev/ultrapath (华为存储多路径)
func IsMultipathDisk(disk string) bool {
	return strings.HasPrefix(disk, "/dev/mapper/") || strings.HasPrefix(disk, "/dev/dm-") || strings.HasPrefix(disk, "/dev/ultrapath")
}

// IsHuaweiMultipathDisk 检查磁盘是否为华为存储多路径磁盘
// 华为存储多路径磁盘以 /dev/ultrapath 开头
func IsHuaweiMultipathDisk(disk string) bool {
	return strings.HasPrefix(disk, "/dev/ultrapath")
}

// ParseUdevIDWWN 从 udevadm info --query=property 输出中解析 ID_WWN。
func ParseUdevIDWWN(udevadmProps string) string {
	for _, line := range strings.Split(udevadmProps, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID_WWN=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ID_WWN="))
		}
	}
	return ""
}

// GetDiskIDWWN 通过 udevadm 获取块设备 ID_WWN（跟随 symlink，对齐恩墨厂商规则）。
func GetDiskIDWWN(ctx *runner.StepContext, disk string) (string, error) {
	cmd := fmt.Sprintf("udevadm info --query=property --name=%s 2>/dev/null", disk)
	result, _ := ctx.Execute(cmd, false)
	out := ""
	if result != nil {
		out = result.GetStdout()
	}
	id := ParseUdevIDWWN(out)
	if id == "" {
		return "", fmt.Errorf("failed to get ID_WWN for disk %s", disk)
	}
	return id, nil
}

// GetDiskWWID 获取磁盘的 WWID
// 支持 NVMe、SCSI、SAS、SAN 等不同类型的设备
// 对于华为存储多路径磁盘，使用 udevadm info 获取 WWID
func GetDiskWWID(ctx *runner.StepContext, disk string) (string, error) {
	devName := strings.TrimPrefix(disk, "/dev/")
	wwid := ""

	// 华为存储多路径磁盘特殊处理
	if IsHuaweiMultipathDisk(disk) {
		return GetHuaweiDiskWWID(ctx, disk)
	}

	// 判断是否为 NVMe 设备
	isNVMe := strings.HasPrefix(devName, "nvme")

	if isNVMe {
		// NVMe 设备：从 /sys/block/nvmeXnY/wwid 获取唯一标识
		cmd := fmt.Sprintf("cat /sys/block/%s/wwid 2>/dev/null", devName)
		result, _ := ctx.Execute(cmd, false)
		if result != nil && result.GetExitCode() == 0 {
			wwid = strings.TrimSpace(result.GetStdout())
		}

		// 如果上面失败，尝试从 /sys/block/nvmeXnY/device/wwid 获取
		if wwid == "" {
			cmd = fmt.Sprintf("cat /sys/block/%s/device/wwid 2>/dev/null", devName)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				wwid = strings.TrimSpace(result.GetStdout())
			}
		}

		// 如果还是失败，尝试从 uuid 获取
		if wwid == "" {
			cmd = fmt.Sprintf("cat /sys/block/%s/uuid 2>/dev/null", devName)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				wwid = strings.TrimSpace(result.GetStdout())
			}
		}

		// 使用 nvme id-ns 命令获取 NGUID
		if wwid == "" {
			cmd = fmt.Sprintf("nvme id-ns %s 2>/dev/null | grep -i nguid | awk '{print $NF}'", disk)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				nguid := strings.TrimSpace(result.GetStdout())
				if nguid != "" && nguid != "0000000000000000" && !strings.HasPrefix(nguid, "00000000") {
					wwid = nguid
				}
			}
		}

		// 使用 nvme id-ns 命令获取 EUI64
		if wwid == "" {
			cmd = fmt.Sprintf("nvme id-ns %s 2>/dev/null | grep -i eui64 | awk '{print $NF}'", disk)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				eui64 := strings.TrimSpace(result.GetStdout())
				if eui64 != "" && eui64 != "0000000000000000" && !strings.HasPrefix(eui64, "00000000") {
					wwid = "eui." + eui64
				}
			}
		}
	} else {
		// SCSI/SAS/SAN 设备：使用 scsi_id 获取 WWID
		cmd := fmt.Sprintf("/lib/udev/scsi_id --whitelisted --replace-whitespace --device=%s 2>/dev/null", disk)
		result, _ := ctx.Execute(cmd, false)

		if result != nil && result.GetExitCode() == 0 {
			wwid = strings.TrimSpace(result.GetStdout())
		}

		// 尝试 udevadm 获取 ID_WWN
		if wwid == "" {
			cmd = fmt.Sprintf("udevadm info --query=property --name=%s 2>/dev/null | grep -E '^ID_WWN=' | cut -d= -f2", disk)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				wwid = strings.TrimSpace(result.GetStdout())
			}
		}

		// 尝试 udevadm 获取 ID_SERIAL
		if wwid == "" {
			cmd = fmt.Sprintf("udevadm info --query=property --name=%s 2>/dev/null | grep -E '^ID_SERIAL=' | cut -d= -f2", disk)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				wwid = strings.TrimSpace(result.GetStdout())
			}
		}
	}

	if wwid == "" {
		return "", fmt.Errorf("failed to get WWID for disk %s", disk)
	}

	return wwid, nil
}

// GetHuaweiDiskWWID 获取华为存储多路径磁盘的 WWID
// 华为磁盘使用 udevadm info -a --name 命令获取 WWID
// 例如：udevadm info -a --name /dev/ultrapath/dg5 | grep ATTR{wwid}
func GetHuaweiDiskWWID(ctx *runner.StepContext, disk string) (string, error) {
	wwid := ""

	// 方法1：使用 udevadm info -a 获取 ATTR{wwid}
	cmd := fmt.Sprintf("udevadm info -a --name %s 2>/dev/null | grep 'ATTR{wwid}' | head -1 | sed 's/.*ATTR{wwid}==\"\\([^\"]*\\)\".*/\\1/'", disk)
	result, _ := ctx.Execute(cmd, false)
	if result != nil && result.GetExitCode() == 0 {
		wwid = strings.TrimSpace(result.GetStdout())
		if wwid != "" {
			return wwid, nil
		}
	}

	// 方法2：使用 udevadm info --query=property 获取 ID_WWN
	cmd = fmt.Sprintf("udevadm info --query=property --name=%s 2>/dev/null | grep -E '^ID_WWN=' | cut -d= -f2", disk)
	result, _ = ctx.Execute(cmd, false)
	if result != nil && result.GetExitCode() == 0 {
		wwid = strings.TrimSpace(result.GetStdout())
		if wwid != "" {
			return wwid, nil
		}
	}

	// 方法3：使用 udevadm info --query=property 获取 ID_SERIAL
	cmd = fmt.Sprintf("udevadm info --query=property --name=%s 2>/dev/null | grep -E '^ID_SERIAL=' | cut -d= -f2", disk)
	result, _ = ctx.Execute(cmd, false)
	if result != nil && result.GetExitCode() == 0 {
		wwid = strings.TrimSpace(result.GetStdout())
		if wwid != "" {
			return wwid, nil
		}
	}

	if wwid == "" {
		return "", fmt.Errorf("failed to get WWID for Huawei disk %s using udevadm. Please run: udevadm info -a --name %s to check available attributes", disk, disk)
	}

	return wwid, nil
}
