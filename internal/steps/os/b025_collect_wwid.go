package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB025CollectWWID 采集共享盘 WWID（供 multipath 配置）
// 当检测到非多路径磁盘需要配置多路径时，此步骤为必需步骤
// 如果获取 WWID 失败，将退出脚本执行
func StepB025CollectWWID() *runner.Step {
	return &runner.Step{
		ID:          "B-025",
		Name:        "Collect Disk WWID",
		Description: "Collect shared disk WWID information for multipath configuration",
		Tags:        []string{"os", "yac", "multipath"},
		Optional:    true, // 单机环境下不需要多路径，可以跳过

		PreCheck: func(ctx *runner.StepContext) error {
			// 强制模式下，跳过检查直接执行
			if ctx.IsForceStep() {
				ctx.Logger.Info("Force mode: will collect WWID even if multipath not enabled")
				return nil
			}

			// YAC 模式下需要收集 WWID（除非磁盘已经是多路径设备）
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				// 如果已经是多路径磁盘，跳过 WWID 收集
				hasMultipathDisks := ctx.GetParamBool("yac_has_multipath_disks", false)
				if hasMultipathDisks {
					return fmt.Errorf("multipath disks detected, WWID collection not needed (use -f B-025 to force collection)")
				}
				return nil
			}

			// 非 YAC 模式：检查是否显式启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			if enabled {
				return nil
			}

			// 检查是否由 B-022 自动检测到需要多路径
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)
			if needMultipath {
				return nil
			}

			return fmt.Errorf("multipath not enabled and not required (use -f B-025 to force collection)")
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Collecting disk WWID information...")

			// 获取所有 YAC 磁盘组中的磁盘
			systemdgStr := ctx.GetParamString("yac_systemdg", "")
			datadgStr := ctx.GetParamString("yac_datadg", "")
			archdgStr := ctx.GetParamString("yac_archdg", "")

			// 收集所有需要配置的磁盘
			allDisks := make(map[string]bool)

			for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
				if dgStr == "" {
					continue
				}
				dg, err := ParseDiskGroupConfig(dgStr)
				if err != nil || dg == nil {
					continue
				}
				for _, disk := range dg.Disks {
					// 跳过华为存储多路径磁盘（华为磁盘已经是多路径设备，无需配置）
					if commonos.IsHuaweiMultipathDisk(disk) {
						ctx.Logger.Info("  %s is Huawei multipath disk, skipping WWID collection", disk)
						continue
					}

					// 只收集非多路径磁盘的 WWID
					if !IsMultipathDisk(disk) {
						allDisks[disk] = true
					}
				}
			}

			if len(allDisks) == 0 {
				ctx.Logger.Info("No non-multipath disks found, skipping WWID collection")
				return nil
			}

			// 收集每个磁盘的 WWID
			var wwidList []string
			var failedDisks []string

			for disk := range allDisks {
				devName := strings.TrimPrefix(disk, "/dev/")

				// 使用公共函数获取 WWID
				wwid, err := GetDiskWWID(ctx, disk)
				if err != nil {
					ctx.Logger.Error("Failed to get WWID for disk: %s", disk)
					failedDisks = append(failedDisks, disk)
				} else {
					ctx.Logger.Info("  %s (%s): %s", disk, devName, wwid)
					wwidList = append(wwidList, fmt.Sprintf("%s %s", disk, wwid))
				}
			}

			// 如果有任何磁盘获取 WWID 失败，报错退出
			if len(failedDisks) > 0 {
				return fmt.Errorf("failed to get WWID for disks: %v. Please check if disks exist and have valid identifiers", failedDisks)
			}

			// 保存 WWID 列表供后续步骤使用
			ctx.SetResult("wwid_list", strings.Join(wwidList, "\n"))
			ctx.SetResult("wwid_count", len(wwidList))

			ctx.Logger.Info("Successfully collected WWID for %d disks", len(wwidList))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// 验证 WWID 列表已收集
			wwidCount, ok := ctx.Results["wwid_count"].(int)
			if !ok || wwidCount == 0 {
				// 检查是否有非多路径磁盘需要处理
				needMultipath := ctx.GetParamBool("yac_need_multipath", false)
				hasMultipathDisks := ctx.GetParamBool("yac_has_multipath_disks", false)

				// 如果所有磁盘都已经是多路径设备，则不需要收集 WWID
				if hasMultipathDisks && !needMultipath {
					ctx.Logger.Info("All disks are multipath devices, WWID collection not needed")
					return nil
				}

				// 如果需要多路径但 WWID 未收集，检查是否所有磁盘都已经是多路径设备
				if needMultipath {
					// 检查当前参数中的磁盘是否都是多路径设备
					systemdgStr := ctx.GetParamString("yac_systemdg", "")
					datadgStr := ctx.GetParamString("yac_datadg", "")
					archdgStr := ctx.GetParamString("yac_archdg", "")

					allMultipath := true
					for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
						if dgStr == "" {
							continue
						}
						dg, err := ParseDiskGroupConfig(dgStr)
						if err != nil || dg == nil {
							continue
						}
						for _, disk := range dg.Disks {
							if !IsMultipathDisk(disk) {
								allMultipath = false
								break
							}
						}
						if !allMultipath {
							break
						}
					}

					if allMultipath {
						ctx.Logger.Info("All disks are multipath devices (parameters updated), WWID collection not needed")
						return nil
					}

					return fmt.Errorf("WWID collection required but no WWIDs collected")
				}
			}
			return nil
		},
	}
}
