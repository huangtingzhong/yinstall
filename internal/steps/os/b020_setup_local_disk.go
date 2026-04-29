package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB020SetupLocalDisk 为数据目录准备本地磁盘（LVM + 挂载）
func StepB020SetupLocalDisk() *runner.Step {
	return &runner.Step{
		ID:          "B-020",
		Name:        "Setup Local Disk",
		Description: "Create LVM and mount data directory",
		Tags:        []string{"os", "disk", "lvm"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			disks := ctx.GetParamStringSlice("os_local_disks")
			vgName := ctx.GetParamString("os_local_vg", "yasvg")
			mountPoint := ctx.GetParamString("os_local_mount", "/data")
			if len(disks) > 0 {
				// 检查 lvm2 工具是否可用
				result, _ := ctx.Execute("which pvcreate vgcreate lvcreate", false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("LVM tools not found, please install lvm2")
				}

				// 检查挂载点是否已被挂载
				result, _ = ctx.Execute(fmt.Sprintf("mountpoint -q %s 2>/dev/null", mountPoint), false)
				if result != nil && result.GetExitCode() == 0 {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "B-020",
						StepName:    "Setup Local Disk",
						Host:        ctx.Executor.Host(),
						Severity:    runner.PrecheckSeverityInfo,
						Code:        "PC.OS.DISK.ALREADY_MOUNTED",
						Message:     fmt.Sprintf("mount point %s is already mounted; apply will skip disk setup", mountPoint),
						Remediation: "If you need to rebuild, unmount it first and clean up fstab/LVM manually.",
					})
				}

				// 检查 VG 是否尚不存在
				result, _ = ctx.Execute(fmt.Sprintf("vgs %s 2>/dev/null", vgName), false)
				if result != nil && result.GetExitCode() == 0 {
					return fmt.Errorf("VG '%s' already exists, please use a different name or remove it first", vgName)
				}

				// 逐盘只读检查
				for _, disk := range disks {
					disk = strings.TrimSpace(disk)
					if disk == "" {
						continue
					}
					// 块设备是否存在？
					result, _ = ctx.Execute(fmt.Sprintf("test -b %s", disk), false)
					if result == nil || result.GetExitCode() != 0 {
						return fmt.Errorf("disk %s not found", disk)
					}
					// 整盘是否已被 mount？
					result, _ = ctx.Execute(fmt.Sprintf("mount | grep -E '^%s[[:space:]]'", disk), false)
					if result != nil && result.GetExitCode() == 0 {
						return fmt.Errorf("disk %s is currently mounted", disk)
					}
					// 分区是否已有挂载点？
					result, _ = ctx.Execute(fmt.Sprintf("lsblk -n -o MOUNTPOINT %s 2>/dev/null | grep -v '^$'", disk), false)
					if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
						return fmt.Errorf("disk %s has mounted partitions", disk)
					}
					// 是否已加入其它 VG？
					result, _ = ctx.Execute(fmt.Sprintf("pvs %s 2>/dev/null | grep -v '%s'", disk, vgName), false)
					if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
						return fmt.Errorf("disk %s is already part of another volume group", disk)
					}
					// 是否已有文件系统签名？
					result, _ = ctx.Execute(fmt.Sprintf("blkid %s 2>/dev/null", disk), false)
					if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
						if !strings.Contains(result.GetStdout(), "LVM2_member") {
							return fmt.Errorf("disk %s already has a filesystem: %s", disk, strings.TrimSpace(result.GetStdout()))
						}
					}
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			disks := ctx.GetParamStringSlice("os_local_disks")
			vgName := ctx.GetParamString("os_local_vg", "yasvg")
			lvName := ctx.GetParamString("os_local_lv", "yaslv")
			mountPoint := ctx.GetParamString("os_local_mount", "/data")
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")

			// 未指定磁盘：仅创建目录并赋权
			if len(disks) == 0 {
				ctx.Logger.Info("No local disks specified, creating directory only")
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mkdir -p %s", mountPoint), true); err != nil {
					return fmt.Errorf("failed to create directory %s: %w", mountPoint, err)
				}
				// 设置属主属组
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("chown %s:%s %s", user, group, mountPoint), true); err != nil {
					return fmt.Errorf("failed to set ownership: %w", err)
				}
				ctx.Logger.Info("Created directory: %s (owner: %s:%s)", mountPoint, user, group)
				return nil
			}

			// 若挂载点已挂载则跳过创建流程
			result, _ := ctx.Execute(fmt.Sprintf("mountpoint -q %s 2>/dev/null", mountPoint), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Mount point %s already mounted, skipping", mountPoint)
				return nil
			}

			// VG 必须不存在（否则无法安全创建）
			result, _ = ctx.Execute(fmt.Sprintf("vgs %s 2>/dev/null", vgName), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("VG '%s' already exists, please use a different name or remove it first", vgName)
			}

			// 在每个磁盘上创建 PV
			ctx.Logger.Info("Creating PVs on disks: %v", disks)
			for _, disk := range disks {
				disk = strings.TrimSpace(disk)
				if disk == "" {
					continue
				}
				// 确认块设备存在
				result, _ := ctx.Execute(fmt.Sprintf("test -b %s", disk), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("disk %s not found", disk)
				}

				// 检查磁盘是否已被占用（已挂载、分区已挂载、或属于其它 VG）
				// 1) 是否整盘已挂载
				// 使用精确匹配避免误匹配（如 /dev/sdb 不会匹配到 /dev/sdb1）
				// mount 输出格式：device on mountpoint，使用空格或制表符作为分隔符
				result, _ = ctx.Execute(fmt.Sprintf("mount | grep -E '^%s[[:space:]]'", disk), false)
				if result != nil && result.GetExitCode() == 0 {
					return fmt.Errorf("disk %s is currently mounted", disk)
				}

				// 2) 分区是否有挂载点
				result, _ = ctx.Execute(fmt.Sprintf("lsblk -n -o MOUNTPOINT %s 2>/dev/null | grep -v '^$'", disk), false)
				if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
					return fmt.Errorf("disk %s has mounted partitions", disk)
				}

				// 3) 是否已属于其它 VG（非目标 VG）
				result, _ = ctx.Execute(fmt.Sprintf("pvs %s 2>/dev/null | grep -v '%s'", disk, vgName), false)
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					return fmt.Errorf("disk %s is already part of another volume group", disk)
				}

				// 4) 是否已有非 LVM PV 的文件系统
				result, _ = ctx.Execute(fmt.Sprintf("blkid %s 2>/dev/null", disk), false)
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					// 已是 PV（LVM2_member）则允许继续
					if !strings.Contains(result.GetStdout(), "LVM2_member") {
						return fmt.Errorf("disk %s already has a filesystem: %s", disk, strings.TrimSpace(result.GetStdout()))
					}
				}

				// 若该盘已是 PV（pvs 可查），则跳过 pvcreate
				result, _ = ctx.Execute(fmt.Sprintf("pvs %s 2>/dev/null", disk), false)
				if result != nil && result.GetExitCode() == 0 {
					ctx.Logger.Info("  %s is already a PV, skipping", disk)
					continue
				}

				// 创建 PV
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("pvcreate -f %s", disk), true); err != nil {
					return fmt.Errorf("failed to create PV on %s: %w", disk, err)
				}
				ctx.Logger.Info("  Created PV on %s", disk)
			}

			// 创建 VG
			diskList := strings.Join(disks, " ")
			cmd := fmt.Sprintf("vgcreate %s %s", vgName, diskList)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to create VG %s: %w", vgName, err)
			}
			ctx.Logger.Info("Created VG: %s", vgName)

			// 确认 LV 尚不存在
			lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)
			result, _ = ctx.Execute(fmt.Sprintf("lvs %s 2>/dev/null", lvPath), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("LV '%s' already exists, please use a different name or remove it first", lvPath)
			}

			// 多块盘时使用 striping 创建 LV
			numDisks := len(disks)
			var lvCmd string
			if numDisks > 1 {
				// stripe 数量等于磁盘数量
				lvCmd = fmt.Sprintf("lvcreate -y -l 100%%FREE -i %d -n %s %s", numDisks, lvName, vgName)
				ctx.Logger.Info("Creating striped LV with %d stripes", numDisks)
			} else {
				lvCmd = fmt.Sprintf("lvcreate -y -l 100%%FREE -n %s %s", lvName, vgName)
			}
			if _, err := ctx.ExecuteWithCheck(lvCmd, true); err != nil {
				return fmt.Errorf("failed to create LV %s: %w", lvName, err)
			}
			ctx.Logger.Info("Created LV: %s", lvPath)

			// 格式化为 XFS
			ctx.Logger.Info("Formatting %s as XFS", lvPath)
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mkfs.xfs -f %s", lvPath), true); err != nil {
				return fmt.Errorf("failed to format %s: %w", lvPath, err)
			}

			// 创建挂载点目录
			ctx.Execute(fmt.Sprintf("mkdir -p %s", mountPoint), true)

			// 挂载
			ctx.Logger.Info("Mounting %s to %s", lvPath, mountPoint)
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mount %s %s", lvPath, mountPoint), true); err != nil {
				return fmt.Errorf("failed to mount %s: %w", lvPath, err)
			}

			// 写入 fstab 以实现开机自动挂载
			fstabEntry := fmt.Sprintf("%s %s xfs defaults 0 0", lvPath, mountPoint)
			// 使用精确匹配避免误匹配（如 /dev/vg1/lv1 不会匹配到 /dev/vg1/lv10）
			// 在 fstab 中，路径后面通常跟着空格或制表符
			result, _ = ctx.Execute(fmt.Sprintf("grep -E '^%s[[:space:]]' /etc/fstab", lvPath), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Info("Adding entry to /etc/fstab")
				ctx.Execute(fmt.Sprintf("echo '%s' >> /etc/fstab", fstabEntry), true)
			}

			// 挂载点赋权
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("chown %s:%s %s", user, group, mountPoint), true); err != nil {
				return fmt.Errorf("failed to set ownership: %w", err)
			}

			ctx.Logger.Info("Setup complete: %s mounted on %s (owner: %s:%s)", lvPath, mountPoint, user, group)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			mountPoint := ctx.GetParamString("os_local_mount", "/data")
			user := ctx.GetParamString("os_user", "yashan")

			// 目录是否存在
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", mountPoint), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("directory %s not found", mountPoint)
			}

			// 属主是否为预期用户
			result, _ = ctx.Execute(fmt.Sprintf("stat -c '%%U' %s", mountPoint), false)
			if result != nil && strings.TrimSpace(result.GetStdout()) != user {
				return fmt.Errorf("directory %s owner is not %s", mountPoint, user)
			}

			return nil
		},
	}
}
