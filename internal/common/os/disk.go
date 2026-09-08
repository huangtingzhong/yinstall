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

// DiskPathKind 共享盘路径形态（决定是否配 OS multipath 与 yfs udev 风格）。
type DiskPathKind string

const (
	DiskRealDM    DiskPathKind = "real_dm"    // 真 dm / ultrapath 等
	DiskAliasNVMe DiskPathKind = "alias_nvme" // 假 mapper symlink → nvme
	DiskBareNVMe  DiskPathKind = "bare_nvme"  // /dev/nvme*
	DiskBareOther DiskPathKind = "bare_other" // 其它裸盘
)

// blockBaseName 取 /dev/xxx 或绝对路径的末段设备名。
func blockBaseName(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/dev/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return path
}

// ClassifyDiskPathLocal 根据路径与 readlink -f 目标做本地分类（便于单测）。
// resolvedTarget 仅对 mapper/dm 有意义；裸盘可传空。
func ClassifyDiskPathLocal(disk, resolvedTarget string) DiskPathKind {
	disk = strings.TrimSpace(disk)
	base := blockBaseName(disk)
	if strings.HasPrefix(base, "nvme") {
		return DiskBareNVMe
	}
	if IsHuaweiMultipathDisk(disk) {
		return DiskRealDM
	}
	if strings.HasPrefix(disk, "/dev/mapper/") || strings.HasPrefix(disk, "/dev/dm-") {
		if strings.HasPrefix(blockBaseName(resolvedTarget), "nvme") {
			return DiskAliasNVMe
		}
		return DiskRealDM
	}
	return DiskBareOther
}

// ClassifyDiskPath 在目标机上分类磁盘路径。
func ClassifyDiskPath(ctx *runner.StepContext, disk string) (DiskPathKind, error) {
	disk = strings.TrimSpace(disk)
	if disk == "" {
		return "", fmt.Errorf("empty disk path")
	}
	if strings.HasPrefix(blockBaseName(disk), "nvme") || IsHuaweiMultipathDisk(disk) ||
		!(strings.HasPrefix(disk, "/dev/mapper/") || strings.HasPrefix(disk, "/dev/dm-")) {
		return ClassifyDiskPathLocal(disk, ""), nil
	}
	resolved := ""
	if ctx != nil {
		res, _ := ctx.Execute(fmt.Sprintf("readlink -f %s 2>/dev/null", disk), false)
		if res != nil && res.GetExitCode() == 0 {
			resolved = strings.TrimSpace(res.GetStdout())
		}
	}
	return ClassifyDiskPathLocal(disk, resolved), nil
}

// NVMeNativeMultipathEnabled 读取 nvme_core.multipath 是否为 Y。
func NVMeNativeMultipathEnabled(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	res, _ := ctx.Execute("cat /sys/module/nvme_core/parameters/multipath 2>/dev/null", false)
	if res == nil || res.GetExitCode() != 0 {
		return false
	}
	return strings.TrimSpace(res.GetStdout()) == "Y"
}

// diskStorageFlags 返回该形态是否需要 OS multipath、是否用 ID_WWN udev。
func diskStorageFlags(kind DiskPathKind, nativeMP bool) (needOSMultipath, useIDWWN bool) {
	switch kind {
	case DiskRealDM:
		return false, false
	case DiskAliasNVMe:
		return false, true
	case DiskBareNVMe:
		if nativeMP {
			return false, true
		}
		return true, false
	default:
		return true, false
	}
}

// YACDiskStoragePolicy YAC 共享盘存储策略（整次安装一致）。
type YACDiskStoragePolicy struct {
	NeedOSMultipath bool
	UseIDWWNUdev    bool
}

// ResolveYACDiskStoragePolicyFromKinds 按已分类结果与原生 MP 开关汇总策略（纯函数，便于模拟各环境）。
func ResolveYACDiskStoragePolicyFromKinds(kinds []DiskPathKind, nativeMP bool) (YACDiskStoragePolicy, error) {
	var out YACDiskStoragePolicy
	if len(kinds) == 0 {
		return out, fmt.Errorf("no disks to resolve storage policy")
	}
	var (
		set      bool
		needOS   bool
		useIDWWN bool
	)
	for i, kind := range kinds {
		n, id := diskStorageFlags(kind, nativeMP)
		if !set {
			needOS, useIDWWN, set = n, id, true
			continue
		}
		if n != needOS || id != useIDWWN {
			return out, fmt.Errorf("mixed YAC disk storage policies at index %d (kind=%s); refuse mixed layouts", i, kind)
		}
	}
	out.NeedOSMultipath = needOS
	out.UseIDWWNUdev = useIDWWN
	return out, nil
}

// ResolveYACDiskStoragePolicy 汇总全部盘的策略；策略冲突则报错。
func ResolveYACDiskStoragePolicy(ctx *runner.StepContext, disks []string) (YACDiskStoragePolicy, error) {
	if len(disks) == 0 {
		return YACDiskStoragePolicy{}, fmt.Errorf("no disks to resolve storage policy")
	}
	nativeMP := NVMeNativeMultipathEnabled(ctx)
	kinds := make([]DiskPathKind, 0, len(disks))
	for _, disk := range disks {
		kind, err := ClassifyDiskPath(ctx, disk)
		if err != nil {
			return YACDiskStoragePolicy{}, err
		}
		kinds = append(kinds, kind)
	}
	return ResolveYACDiskStoragePolicyFromKinds(kinds, nativeMP)
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
