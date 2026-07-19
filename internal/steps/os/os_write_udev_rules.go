package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// udevRuleActionOpts 与 installer.md 3.13.2 一致：仅在 add/change 时匹配，并关闭设备 watch。
const udevRuleActionOpts = `, ACTION=="add|change", OPTIONS:="nowatch"`

// stepWriteUdevRules 写入 udev 规则（YAC）
func stepWriteUdevRules() *runner.Step {
	return &runner.Step{
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
			osLogPhase(ctx, "plan", "B-029: Write Udev Rules")
			rulesFile := ctx.GetParamString("yac_udev_rules_file", "/etc/udev/rules.d/99-yashandb-permissions.rules")
			owner := ctx.GetParamString("yac_udev_owner", "yashan")
			group := ctx.GetParamString("yac_udev_group", "YASDBA")
			mode := ctx.GetParamString("yac_udev_mode", "0666")

			devYfsDir := "/dev/yfs"

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
						rule := fmt.Sprintf(`SUBSYSTEM=="block", ATTR{wwid}=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"%s`,
							wwid, alias, owner, group, mode, udevRuleActionOpts)
						rules = append(rules, rule)
						ctx.Logger.Info("  Generated WWID-based rule for Huawei disk %s -> /dev/yfs/%s (wwid: %s)", disk, alias, wwid)
					} else if IsMultipathDisk(disk) {
						dmAlias := strings.TrimPrefix(disk, "/dev/mapper/")
						if dmAlias == disk {
							dmAlias = strings.TrimPrefix(disk, "/dev/dm-")
						}
						rule := fmt.Sprintf(`SUBSYSTEM=="block", ENV{DM_NAME}=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"%s`,
							dmAlias, alias, owner, group, mode, udevRuleActionOpts)
						rules = append(rules, rule)
						ctx.Logger.Info("  Generated DM_NAME-based rule for multipath disk %s -> /dev/yfs/%s", disk, alias)
					} else if needMultipath {
						dmRule := fmt.Sprintf(`SUBSYSTEM=="block", ENV{DM_NAME}=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"%s`,
							alias, alias, owner, group, mode, udevRuleActionOpts)
						rules = append(rules, dmRule)
						ctx.Logger.Info("  Generated DM_NAME rule for raw disk %s -> /dev/yfs/%s (multipath enabled)", disk, alias)
					} else {
						diskName := strings.TrimPrefix(disk, "/dev/")
						kernelRule := fmt.Sprintf(`SUBSYSTEM=="block", KERNEL=="%s", SYMLINK+="yfs/%s", OWNER="%s", GROUP="%s", MODE="%s"%s`,
							diskName, alias, owner, group, mode, udevRuleActionOpts)
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
			if archdg != nil && (datadg == nil || DiskGroupConfigsDistinct(archdg, datadg)) {
				if err := processDisks(archdg, "arch"); err != nil {
					return err
				}
			}

			if len(rules) == 0 {
				rule := fmt.Sprintf(`SUBSYSTEM=="block", ENV{DM_NAME}=~"^(data|sys|arch)", SYMLINK+="yfs/%%E{DM_NAME}", OWNER="%s", GROUP="%s", MODE="%s"%s`,
					owner, group, mode, udevRuleActionOpts)
				rules = append(rules, rule)
				ctx.Logger.Info("  Using default SYMLINK-based rule for all multipath disks")
			}

			rulesContent := strings.Join(rules, "\n")
			yfsOK := udevYfsDirReady(ctx, devYfsDir, owner, group)
			rulesOK := false
			if !ctx.IsForceStep() {
				existing, _ := ctx.Execute(fmt.Sprintf("cat %s 2>/dev/null || true", rulesFile), false)
				cur := ""
				if existing != nil {
					cur = existing.GetStdout()
				}
				rulesOK = commonos.TextContentEqual(cur, rulesContent)
			}
			if !ctx.IsForceStep() && yfsOK && rulesOK {
				ctx.Logger.Info("Udev rules and /dev/yfs already configured, skipping (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=udev")
				ctx.SetResult("yac_udev_changed", false)
				return nil
			}

			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", devYfsDir), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Info("Directory %s does not exist, creating it", devYfsDir)
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mkdir -p %s", devYfsDir), true); err != nil {
					return fmt.Errorf("failed to create directory %s: %v", devYfsDir, err)
				}
			} else {
				ctx.Logger.Info("Directory %s already exists", devYfsDir)
			}

			chownCmd := fmt.Sprintf("chown %s:%s %s", owner, group, devYfsDir)
			if _, err := ctx.ExecuteWithCheck(chownCmd, true); err != nil {
				return fmt.Errorf("failed to set owner and group for %s: %v", devYfsDir, err)
			}
			ctx.Logger.Info("Set owner and group for %s to %s:%s", devYfsDir, owner, group)

			chmodCmd := fmt.Sprintf("chmod 0755 %s", devYfsDir)
			if _, err := ctx.ExecuteWithCheck(chmodCmd, true); err != nil {
				return fmt.Errorf("failed to set permissions for %s: %v", devYfsDir, err)
			}
			ctx.Logger.Info("Set permissions for %s to 0755", devYfsDir)

			osLogPhase(ctx, "op-start", fmt.Sprintf("file=%s rules=%d", rulesFile, len(rules)))
			cmd := fmt.Sprintf("echo '%s' > %s", rulesContent, rulesFile)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				osLogPhase(ctx, "op-fail", runner.TruncateForLog(err.Error(), 80))
				return err
			}
			ctx.SetResult("yac_udev_changed", true)
			osLogPhase(ctx, "op-done", fmt.Sprintf("file=%s rules=%d", rulesFile, len(rules)))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			rulesFile := ctx.GetParamString("yac_udev_rules_file", "/etc/udev/rules.d/99-yashandb-permissions.rules")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", rulesFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("udev rules file not created")
			}
			return nil
		},
	}
}

func udevYfsDirReady(ctx *runner.StepContext, dir, owner, group string) bool {
	r, _ := ctx.Execute(fmt.Sprintf("test -d %s && stat -c '%%U %%G %%a' %s 2>/dev/null", dir, dir), false)
	if r == nil || r.GetExitCode() != 0 {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(r.GetStdout()))
	if len(fields) < 3 {
		return false
	}
	return fields[0] == owner && fields[1] == group && fields[2] == "755"
}
