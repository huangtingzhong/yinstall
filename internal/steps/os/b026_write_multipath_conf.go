package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB026WriteMultipathConf Write multipath.conf (YAC)
// 使用 B-025 收集的 WWID 信息生成 multipath.conf
func StepB026WriteMultipathConf() *runner.Step {
	return &runner.Step{
		ID:          "B-026",
		Name:        "Write Multipath Config",
		Description: "Configure multipath with collected WWIDs",
		Tags:        []string{"os", "yac", "multipath"},
		Optional:    true, // 如果 multipath.conf 已存在且有效，可以跳过
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			// YAC 模式下需要配置 multipath（除非磁盘已经是多路径设备）
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				// 检查 multipath.conf 是否已配置
				confPath := ctx.GetParamString("yac_multipath_conf", "/etc/multipath.conf")
				confResult, _ := ctx.Execute(fmt.Sprintf("test -f %s", confPath), false)
				hasValidConfig := false
				if confResult != nil && confResult.GetExitCode() == 0 {
					// multipath.conf 已存在，检查是否包含有效的配置
					// 检查是否包含 multipaths 块
					checkResult, _ := ctx.Execute(fmt.Sprintf("grep -q 'multipaths' %s && grep -q 'multipath {' %s", confPath, confPath), false)
					if checkResult != nil && checkResult.GetExitCode() == 0 {
						// multipath.conf 已配置，检查是否需要配置（根据 yac_need_multipath）
						needMultipath := ctx.GetParamBool("yac_need_multipath", false)
						if !needMultipath {
							// 不需要多路径配置，跳过
							return fmt.Errorf("multipath disks already configured")
						}
						// 需要多路径配置，但 multipath.conf 已存在，检查配置是否完整
						// 检查是否包含所有需要的磁盘配置
						hasValidConfig = true
					}
				}

				// 检查实际的多路径设备是否存在，而不是依赖参数
				// 因为参数可能在第一个节点上被更新，但第二个节点上还没有配置多路径
				systemdgStr := ctx.GetParamString("yac_systemdg", "")
				datadgStr := ctx.GetParamString("yac_datadg", "")
				archdgStr := ctx.GetParamString("yac_archdg", "")

				// 检查参数中的磁盘是否都是多路径设备
				allMultipath := true
				hasAnyDisk := false
				for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
					if dgStr == "" {
						continue
					}
					dg, err := ParseDiskGroupConfig(dgStr)
					if err != nil || dg == nil {
						continue
					}
					for _, disk := range dg.Disks {
						hasAnyDisk = true
						if !IsMultipathDisk(disk) {
							allMultipath = false
							break
						}
						// 如果是多路径设备路径，检查实际设备是否存在
						if IsMultipathDisk(disk) {
							checkResult, _ := ctx.Execute(fmt.Sprintf("test -b %s || test -e %s", disk, disk), false)
							if checkResult == nil || checkResult.GetExitCode() != 0 {
								// 多路径设备路径不存在，说明还没有配置多路径
								allMultipath = false
								break
							}
						}
					}
					if !allMultipath {
						break
					}
				}

				// 如果所有磁盘都是多路径设备且实际设备存在，且 multipath.conf 已配置，则跳过配置
				if hasAnyDisk && allMultipath && hasValidConfig {
					return fmt.Errorf("multipath disks already configured")
				}
				return nil
			}

			// 非 YAC 模式：检查是否显式启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			if !enabled && !needMultipath {
				return fmt.Errorf("multipath not enabled and not required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			confPath := ctx.GetParamString("yac_multipath_conf", "/etc/multipath.conf")

			// 获取磁盘组配置
			systemdgStr := ctx.GetParamString("yac_systemdg", "")
			datadgStr := ctx.GetParamString("yac_datadg", "")
			archdgStr := ctx.GetParamString("yac_archdg", "")

			// 解析磁盘组
			systemdg, _ := ParseDiskGroupConfig(systemdgStr)
			datadg, _ := ParseDiskGroupConfig(datadgStr)
			archdg, _ := ParseDiskGroupConfig(archdgStr)

			// 收集所有磁盘的 WWID
			var multipathEntries []string

			// 为每个磁盘组的磁盘收集 WWID 并生成配置
			collectDisksWWID := func(dg *DiskGroupConfig, prefix string) error {
				if dg == nil {
					return nil
				}

				for i, disk := range dg.Disks {
					// 检查是否为华为存储多路径磁盘，如果是则跳过多路径配置
					if commonos.IsHuaweiMultipathDisk(disk) {
						ctx.Logger.Info("  %s is Huawei multipath disk, skipping multipath configuration", disk)
						continue
					}

					// 如果是多路径设备路径，检查实际设备是否存在
					if IsMultipathDisk(disk) {
						// 检查多路径设备是否存在
						checkResult, _ := ctx.Execute(fmt.Sprintf("test -b %s || test -e %s", disk, disk), false)
						if checkResult != nil && checkResult.GetExitCode() == 0 {
							// 多路径设备已存在，跳过配置
							ctx.Logger.Info("  %s already exists, skipping", disk)
							continue
						}
						// 多路径设备不存在，说明参数已被更新但实际设备还未创建
						// 尝试从多路径设备路径获取 WWID（通过 multipath -ll 命令）
						// 提取别名（例如：/dev/mapper/sys1 -> sys1）
						alias := strings.TrimPrefix(disk, "/dev/mapper/")
						if alias == disk {
							// 不是 /dev/mapper/ 格式，尝试其他格式
							alias = strings.TrimPrefix(disk, "/dev/dm-")
							if alias == disk {
								alias = strings.TrimPrefix(disk, "/dev/ultrapath")
							}
						}
						// 尝试从 multipath -ll 获取 WWID
						cmd := fmt.Sprintf("multipath -ll %s 2>/dev/null | grep -oP '\\S+ \\(\\K[^)]+' | head -1", alias)
						result, _ := ctx.Execute(cmd, false)
						if result != nil && result.GetStdout() != "" {
							wwid := strings.TrimSpace(result.GetStdout())
							// 使用获取到的 WWID 生成配置
							entry := fmt.Sprintf(`    multipath {
        wwid     %s
        alias    %s
    }`, wwid, alias)
							multipathEntries = append(multipathEntries, entry)
							ctx.Logger.Info("  %s -> %s (wwid: %s, from multipath -ll)", disk, alias, wwid)
							continue
						}
						// 如果无法获取 WWID，说明多路径设备还未创建，需要配置
						// 但是无法从多路径设备路径推断原始磁盘路径，这里报错
						return fmt.Errorf("multipath device %s does not exist, and cannot get WWID from multipath -ll. This may indicate that multipath was configured on another node but not on this node. Please ensure multipath is configured on all nodes before updating parameters", disk)
					}

					// 使用公共函数获取 WWID
					wwid, err := GetDiskWWID(ctx, disk)
					if err != nil {
						return fmt.Errorf("failed to get WWID for disk %s: %w", disk, err)
					}

					// 生成别名
					alias := fmt.Sprintf("%s%d", prefix, i+1)

					entry := fmt.Sprintf(`    multipath {
        wwid     %s
        alias    %s
    }`, wwid, alias)
					multipathEntries = append(multipathEntries, entry)
					ctx.Logger.Info("  %s -> %s (wwid: %s)", disk, alias, wwid)
				}
				return nil
			}

			ctx.Logger.Info("Generating multipath configuration...")

			// 收集 systemdg 磁盘
			if systemdg != nil {
				ctx.Logger.Info("Processing systemdg '%s':", systemdg.Name)
				if err := collectDisksWWID(systemdg, "sys"); err != nil {
					return err
				}
			}

			// 收集 datadg 磁盘
			if datadg != nil {
				ctx.Logger.Info("Processing datadg '%s':", datadg.Name)
				if err := collectDisksWWID(datadg, "data"); err != nil {
					return err
				}
			}

			// 收集 archdg 磁盘（如果与 datadg 不同）
			if archdg != nil && (datadg == nil || archdg.Name != datadg.Name) {
				ctx.Logger.Info("Processing archdg '%s':", archdg.Name)
				if err := collectDisksWWID(archdg, "arch"); err != nil {
					return err
				}
			}

			// 生成配置文件
			config := fmt.Sprintf(`defaults {
    user_friendly_names yes
    find_multipaths off
    reservation_key file
}

multipaths {
%s
}
`, strings.Join(multipathEntries, "\n"))

			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", confPath, config)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			if err != nil {
				return fmt.Errorf("failed to write multipath.conf: %w", err)
			}

			ctx.Logger.Info("Created %s with %d multipath entries", confPath, len(multipathEntries))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			confPath := ctx.GetParamString("yac_multipath_conf", "/etc/multipath.conf")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", confPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("multipath.conf not created")
			}
			return nil
		},
	}
}
