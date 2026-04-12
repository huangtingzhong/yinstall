package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB029WriteUdevRules Write udev rules (YAC)
func StepB029WriteUdevRules() *runner.Step {
	return &runner.Step{
		ID:          "B-029",
		Name:        "Write Udev Rules",
		Description: "Configure shared disk permissions",
		Tags:        []string{"os", "yac", "udev"},
		Optional:    true, // 单机环境下不需要多路径/udev，可以跳过

		PreCheck: func(ctx *runner.StepContext) error {
			// YAC 模式下需要配置 udev 规则
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
			rulesFile := ctx.GetParamString("yac_udev_rules_file", "/etc/udev/rules.d/99-yashandb-permissions.rules")
			owner := ctx.GetParamString("yac_udev_owner", "yashan")
			group := ctx.GetParamString("yac_udev_group", "YASDBA")
			mode := ctx.GetParamString("yac_udev_mode", "0666")

			devYfsDir := "/dev/yfs"

			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", devYfsDir), false)
			if result.GetExitCode() != 0 {
				ctx.Logger.Info("Directory %s does not exist, creating it", devYfsDir)
				_, err := ctx.ExecuteWithCheck(fmt.Sprintf("mkdir -p %s", devYfsDir), true)
				if err != nil {
					return fmt.Errorf("failed to create directory %s: %v", devYfsDir, err)
				}
			} else {
				ctx.Logger.Info("Directory %s already exists", devYfsDir)
			}

			chownCmd := fmt.Sprintf("chown %s:%s %s", owner, group, devYfsDir)
			_, err := ctx.ExecuteWithCheck(chownCmd, true)
			if err != nil {
				return fmt.Errorf("failed to set owner and group for %s: %v", devYfsDir, err)
			}
			ctx.Logger.Info("Set owner and group for %s to %s:%s", devYfsDir, owner, group)

			chmodCmd := fmt.Sprintf("chmod 0755 %s", devYfsDir)
			_, err = ctx.ExecuteWithCheck(chmodCmd, true)
			if err != nil {
				return fmt.Errorf("failed to set permissions for %s: %v", devYfsDir, err)
			}
			ctx.Logger.Info("Set permissions for %s to 0755", devYfsDir)

			systemdgStr := ctx.GetParamString("yac_systemdg", "")
			datadgStr := ctx.GetParamString("yac_datadg", "")
			archdgStr := ctx.GetParamString("yac_archdg", "")

			systemdg, _ := ParseDiskGroupConfig(systemdgStr)
			datadg, _ := ParseDiskGroupConfig(datadgStr)
			archdg, _ := ParseDiskGroupConfig(archdgStr)

			var rules []string

			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			processDisks := func(dg *DiskGroupConfig, prefix string) error {
				if dg == nil {
					return nil
				}

				for i, disk := range dg.Disks {
					alias := fmt.Sprintf("%s%d", prefix, i+1)

					if commonos.IsHuaweiMultipathDisk(disk) {
						wwid, err := commonos.GetDiskWWID(ctx, disk)
						if err != nil {
							ctx.Logger.Warn("Failed to get WWID for Huawei disk %s: %v, skipping udev rule", disk, err)
							continue
						}
						rule := fmt.Sprintf(`SUBSYSTEM=="block", ATTR{wwid}=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"`,
							wwid, alias, owner, group, mode)
						rules = append(rules, rule)
						ctx.Logger.Info("  Generated WWID-based rule for Huawei disk %s -> /dev/yfs/%s (wwid: %s)", disk, alias, wwid)
					} else if IsMultipathDisk(disk) {
						dmAlias := strings.TrimPrefix(disk, "/dev/mapper/")
						if dmAlias == disk {
							dmAlias = strings.TrimPrefix(disk, "/dev/dm-")
						}
						rule := fmt.Sprintf(`SUBSYSTEM=="block", ENV{DM_NAME}=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"`,
							dmAlias, alias, owner, group, mode)
						rules = append(rules, rule)
						ctx.Logger.Info("  Generated DM_NAME-based rule for multipath disk %s -> /dev/yfs/%s", disk, alias)
					} else if needMultipath {
						dmRule := fmt.Sprintf(`SUBSYSTEM=="block", ENV{DM_NAME}=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"`,
							alias, alias, owner, group, mode)
						rules = append(rules, dmRule)
						ctx.Logger.Info("  Generated DM_NAME rule for raw disk %s -> /dev/yfs/%s (multipath enabled)", disk, alias)
					} else {
						diskName := strings.TrimPrefix(disk, "/dev/")
						kernelRule := fmt.Sprintf(`KERNEL=="%s", SUBSYSTEM=="block", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"`,
							diskName, alias, owner, group, mode)
						rules = append(rules, kernelRule)
						ctx.Logger.Info("  Generated KERNEL rule for raw disk %s -> /dev/yfs/%s (no multipath)", disk, alias)
					}
				}
				return nil
			}

			if err := processDisks(systemdg, "sys"); err != nil {
				return err
			}
			if err := processDisks(datadg, "data"); err != nil {
				return err
			}
			if archdg != nil && (datadg == nil || archdg.Name != datadg.Name) {
				if err := processDisks(archdg, "arch"); err != nil {
					return err
				}
			}

			if len(rules) == 0 {
				rule := fmt.Sprintf(`SUBSYSTEM=="block", ENV{DM_NAME}=~"^(data|sys|arch)", SYMLINK+="yfs/%%E{DM_NAME}", OWNER="%s", GROUP="%s", MODE="%s"`,
					owner, group, mode)
				rules = append(rules, rule)
				ctx.Logger.Info("  Using default SYMLINK-based rule for all multipath disks")
			}

			rulesContent := strings.Join(rules, "\n")
			cmd := fmt.Sprintf("echo '%s' > %s", rulesContent, rulesFile)
			_, err = ctx.ExecuteWithCheck(cmd, true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			rulesFile := ctx.GetParamString("yac_udev_rules_file", "/etc/udev/rules.d/99-yashandb-permissions.rules")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", rulesFile), false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("udev rules file not created")
			}
			return nil
		},
	}
}
