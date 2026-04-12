package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB020SetupLocalDisk Setup local disk for data directory
func StepB020SetupLocalDisk() *runner.Step {
	return &runner.Step{
		ID:          "B-020",
		Name:        "Setup Local Disk",
		Description: "Create LVM and mount data directory",
		Tags:        []string{"os", "disk", "lvm"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			disks := ctx.GetParamStringSlice("os_local_disks")
			if len(disks) > 0 {
				// Check if lvm2 tools are available
				result, _ := ctx.Execute("which pvcreate vgcreate lvcreate", false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("LVM tools not found, please install lvm2")
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

			// If no disks specified, just create directory
			if len(disks) == 0 {
				ctx.Logger.Info("No local disks specified, creating directory only")
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mkdir -p %s", mountPoint), true); err != nil {
					return fmt.Errorf("failed to create directory %s: %w", mountPoint, err)
				}
				// Set ownership
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("chown %s:%s %s", user, group, mountPoint), true); err != nil {
					return fmt.Errorf("failed to set ownership: %w", err)
				}
				ctx.Logger.Info("Created directory: %s (owner: %s:%s)", mountPoint, user, group)
				return nil
			}

			// Check if already mounted
			result, _ := ctx.Execute(fmt.Sprintf("mountpoint -q %s 2>/dev/null", mountPoint), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Mount point %s already mounted, skipping", mountPoint)
				return nil
			}

			// Check if VG already exists
			result, _ = ctx.Execute(fmt.Sprintf("vgs %s 2>/dev/null", vgName), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("VG '%s' already exists, please use a different name or remove it first", vgName)
			}

			// Create PV on each disk
			ctx.Logger.Info("Creating PVs on disks: %v", disks)
			for _, disk := range disks {
				disk = strings.TrimSpace(disk)
				if disk == "" {
					continue
				}
				// Check if disk exists
				result, _ := ctx.Execute(fmt.Sprintf("test -b %s", disk), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("disk %s not found", disk)
				}

				// Check if disk is in use (mounted, has partitions, or part of other VG)
				// 1. Check if mounted
				// 使用精确匹配避免误匹配（如 /dev/sdb 不会匹配到 /dev/sdb1）
				// mount 输出格式：device on mountpoint，使用空格或制表符作为分隔符
				result, _ = ctx.Execute(fmt.Sprintf("mount | grep -E '^%s[[:space:]]'", disk), false)
				if result != nil && result.GetExitCode() == 0 {
					return fmt.Errorf("disk %s is currently mounted", disk)
				}

				// 2. Check if has partitions in use
				result, _ = ctx.Execute(fmt.Sprintf("lsblk -n -o MOUNTPOINT %s 2>/dev/null | grep -v '^$'", disk), false)
				if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
					return fmt.Errorf("disk %s has mounted partitions", disk)
				}

				// 3. Check if part of another VG (not our target VG)
				result, _ = ctx.Execute(fmt.Sprintf("pvs %s 2>/dev/null | grep -v '%s'", disk, vgName), false)
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					return fmt.Errorf("disk %s is already part of another volume group", disk)
				}

				// 4. Check if disk has filesystem
				result, _ = ctx.Execute(fmt.Sprintf("blkid %s 2>/dev/null", disk), false)
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					// Check if it's already a PV
					if !strings.Contains(result.GetStdout(), "LVM2_member") {
						return fmt.Errorf("disk %s already has a filesystem: %s", disk, strings.TrimSpace(result.GetStdout()))
					}
				}

				// Check if already a PV for our VG
				result, _ = ctx.Execute(fmt.Sprintf("pvs %s 2>/dev/null", disk), false)
				if result != nil && result.GetExitCode() == 0 {
					ctx.Logger.Info("  %s is already a PV, skipping", disk)
					continue
				}

				// Create PV
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("pvcreate -f %s", disk), true); err != nil {
					return fmt.Errorf("failed to create PV on %s: %w", disk, err)
				}
				ctx.Logger.Info("  Created PV on %s", disk)
			}

			// Create VG
			diskList := strings.Join(disks, " ")
			cmd := fmt.Sprintf("vgcreate %s %s", vgName, diskList)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to create VG %s: %w", vgName, err)
			}
			ctx.Logger.Info("Created VG: %s", vgName)

			// Check if LV already exists
			lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)
			result, _ = ctx.Execute(fmt.Sprintf("lvs %s 2>/dev/null", lvPath), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("LV '%s' already exists, please use a different name or remove it first", lvPath)
			}

			// Create LV with striping if multiple disks
			numDisks := len(disks)
			var lvCmd string
			if numDisks > 1 {
				// Use striping with stripe count = number of disks
				lvCmd = fmt.Sprintf("lvcreate -y -l 100%%FREE -i %d -n %s %s", numDisks, lvName, vgName)
				ctx.Logger.Info("Creating striped LV with %d stripes", numDisks)
			} else {
				lvCmd = fmt.Sprintf("lvcreate -y -l 100%%FREE -n %s %s", lvName, vgName)
			}
			if _, err := ctx.ExecuteWithCheck(lvCmd, true); err != nil {
				return fmt.Errorf("failed to create LV %s: %w", lvName, err)
			}
			ctx.Logger.Info("Created LV: %s", lvPath)

			// Format as XFS
			ctx.Logger.Info("Formatting %s as XFS", lvPath)
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mkfs.xfs -f %s", lvPath), true); err != nil {
				return fmt.Errorf("failed to format %s: %w", lvPath, err)
			}

			// Create mount point
			ctx.Execute(fmt.Sprintf("mkdir -p %s", mountPoint), true)

			// Mount
			ctx.Logger.Info("Mounting %s to %s", lvPath, mountPoint)
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mount %s %s", lvPath, mountPoint), true); err != nil {
				return fmt.Errorf("failed to mount %s: %w", lvPath, err)
			}

			// Add to fstab for persistent mount
			fstabEntry := fmt.Sprintf("%s %s xfs defaults 0 0", lvPath, mountPoint)
			// 使用精确匹配避免误匹配（如 /dev/vg1/lv1 不会匹配到 /dev/vg1/lv10）
			// 在 fstab 中，路径后面通常跟着空格或制表符
			result, _ = ctx.Execute(fmt.Sprintf("grep -E '^%s[[:space:]]' /etc/fstab", lvPath), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Info("Adding entry to /etc/fstab")
				ctx.Execute(fmt.Sprintf("echo '%s' >> /etc/fstab", fstabEntry), true)
			}

			// Set ownership
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("chown %s:%s %s", user, group, mountPoint), true); err != nil {
				return fmt.Errorf("failed to set ownership: %w", err)
			}

			ctx.Logger.Info("Setup complete: %s mounted on %s (owner: %s:%s)", lvPath, mountPoint, user, group)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			mountPoint := ctx.GetParamString("os_local_mount", "/data")
			user := ctx.GetParamString("os_user", "yashan")

			// Check directory exists
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", mountPoint), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("directory %s not found", mountPoint)
			}

			// Check ownership
			result, _ = ctx.Execute(fmt.Sprintf("stat -c '%%U' %s", mountPoint), false)
			if result != nil && strings.TrimSpace(result.GetStdout()) != user {
				return fmt.Errorf("directory %s owner is not %s", mountPoint, user)
			}

			return nil
		},
	}
}
