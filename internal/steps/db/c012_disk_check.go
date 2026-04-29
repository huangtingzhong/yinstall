package db

import (
	"fmt"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC012DiskCheck 校验共享盘对各节点可见性及权限
func StepC012DiskCheck() *runner.Step {
	return &runner.Step{
		ID:          "C-012",
		Name:        "Validate Shared Disks",
		Description: "Validate YAC shared disk visibility, ownership and permissions",
		Tags:        []string{"db", "yac", "disk"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if !isYACMode {
				return fmt.Errorf("not in YAC mode, skipping")
			}

			systemdg := ctx.GetParamString("yac_systemdg", "")
			datadg := ctx.GetParamString("yac_datadg", "")
			if systemdg == "" || datadg == "" {
				return fmt.Errorf("yac_systemdg and yac_datadg are required")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			systemdgStr := ctx.GetParamString("yac_systemdg", "")
			datadgStr := ctx.GetParamString("yac_datadg", "")
			archdgStr := ctx.GetParamString("yac_archdg", "")
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_dba_group", "YASDBA")
			expectedPerm := "666" // 期望的权限

			// 第一步：检查是否需要多路径映射
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			// 存储每个节点的磁盘到多路径设备的映射
			type DiskMapping struct {
				OriginalDisk  string
				MultipathDisk string
				WWID          string
			}

			// 每个节点的映射：host -> (原始磁盘 -> 多路径设备)
			nodeMappings := make(map[string]map[string]DiskMapping)

			// 收集所有原始磁盘
			var allDisks []string
			for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
				if dgStr == "" {
					continue
				}
				parts := strings.SplitN(dgStr, ":", 2)
				if len(parts) == 2 {
					for _, d := range strings.Split(parts[1], ",") {
						d = strings.TrimSpace(d)
						if d != "" {
							allDisks = append(allDisks, d)
						}
					}
				}
			}

			// 第二步：在所有节点上检查多路径设备映射
			if needMultipath {
				ctx.Logger.Info("Checking multipath device mappings on all YAC nodes...")

				for _, th := range ctx.HostsToRun() {
					hctx := ctx.ForHost(th)
					nodeMappings[th.Host] = make(map[string]DiskMapping)

					hctx.Logger.Info("Collecting multipath mappings on %s...", th.Host)

					for _, disk := range allDisks {
						// 如果已经是多路径设备，直接记录
						if commonos.IsMultipathDisk(disk) {
							nodeMappings[th.Host][disk] = DiskMapping{
								OriginalDisk:  disk,
								MultipathDisk: disk,
								WWID:          "",
							}
							hctx.Logger.Info("  %s: already multipath device", disk)
							continue
						}

						// 获取原始磁盘的 WWID
						wwid, err := commonos.GetDiskWWID(hctx, disk)
						if err != nil {
							return fmt.Errorf("failed to get WWID for disk %s on %s: %w", disk, th.Host, err)
						}

						// 通过 WWID 查找对应的多路径设备别名
						cmd := fmt.Sprintf("multipath -ll 2>/dev/null | grep '%s' | head -1 | awk '{print $1}'", wwid)
						result, _ := hctx.Execute(cmd, false)

						var multipathDisk string
						if result != nil && result.GetStdout() != "" {
							alias := strings.TrimSpace(result.GetStdout())
							if alias != "" {
								multipathDisk = fmt.Sprintf("/dev/mapper/%s", alias)
								// 验证多路径设备是否存在
								checkResult, _ := hctx.Execute(fmt.Sprintf("test -b %s || test -e %s", multipathDisk, multipathDisk), false)
								if checkResult == nil || checkResult.GetExitCode() != 0 {
									return fmt.Errorf("multipath device %s not found on %s (WWID: %s)", multipathDisk, th.Host, wwid)
								}
							}
						}

						if multipathDisk == "" {
							return fmt.Errorf("failed to find multipath device for %s (WWID: %s) on %s", disk, wwid, th.Host)
						}

						nodeMappings[th.Host][disk] = DiskMapping{
							OriginalDisk:  disk,
							MultipathDisk: multipathDisk,
							WWID:          wwid,
						}
						hctx.Logger.Info("  %s -> %s (WWID: %s)", disk, multipathDisk, wwid)
					}
				}

				// 第三步：验证所有节点的映射一致性
				ctx.Logger.Info("Validating multipath mapping consistency across all nodes...")

				// 使用第一个节点的映射作为参考
				var referenceHost string
				var referenceMappings map[string]DiskMapping
				for host, mappings := range nodeMappings {
					referenceHost = host
					referenceMappings = mappings
					break
				}

				for host, mappings := range nodeMappings {
					if host == referenceHost {
						continue
					}

					for disk, refMapping := range referenceMappings {
						nodeMapping, exists := mappings[disk]
						if !exists {
							return fmt.Errorf("disk %s mapping not found on %s", disk, host)
						}

						if nodeMapping.MultipathDisk != refMapping.MultipathDisk {
							return fmt.Errorf("multipath device mismatch for %s: %s on %s vs %s on %s",
								disk, nodeMapping.MultipathDisk, host, refMapping.MultipathDisk, referenceHost)
						}

						if nodeMapping.WWID != "" && refMapping.WWID != "" && nodeMapping.WWID != refMapping.WWID {
							return fmt.Errorf("WWID mismatch for %s: %s on %s vs %s on %s",
								disk, nodeMapping.WWID, host, refMapping.WWID, referenceHost)
						}
					}
				}

				ctx.Logger.Info("OK: Multipath mapping consistency validated across all nodes")

				// 第四步：更新磁盘组参数（raw → /dev/mapper/）
				ctx.Logger.Info("Updating diskgroup parameters with multipath devices...")

				updateDiskGroupParam := func(dgStr string) string {
					if dgStr == "" {
						return ""
					}

					parts := strings.SplitN(dgStr, ":", 2)
					if len(parts) != 2 {
						return dgStr
					}

					dgName := parts[0]
					disks := strings.Split(parts[1], ",")
					var updatedDisks []string

					for _, disk := range disks {
						disk = strings.TrimSpace(disk)
						if disk == "" {
							continue
						}

						if mapping, exists := referenceMappings[disk]; exists {
							updatedDisks = append(updatedDisks, mapping.MultipathDisk)
						} else {
							updatedDisks = append(updatedDisks, disk)
						}
					}

					return fmt.Sprintf("%s:%s", dgName, strings.Join(updatedDisks, ","))
				}

				updatedSystemdg := updateDiskGroupParam(systemdgStr)
				updatedDatadg := updateDiskGroupParam(datadgStr)
				updatedArchdg := updateDiskGroupParam(archdgStr)

				if updatedSystemdg != systemdgStr {
					ctx.Params["yac_systemdg"] = updatedSystemdg
					ctx.Logger.Info("Updated yac_systemdg: %s -> %s", systemdgStr, updatedSystemdg)
					systemdgStr = updatedSystemdg
				}

				if updatedDatadg != datadgStr {
					ctx.Params["yac_datadg"] = updatedDatadg
					ctx.Logger.Info("Updated yac_datadg: %s -> %s", datadgStr, updatedDatadg)
					datadgStr = updatedDatadg
				}

				if updatedArchdg != archdgStr {
					ctx.Params["yac_archdg"] = updatedArchdg
					ctx.Logger.Info("Updated yac_archdg: %s -> %s", archdgStr, updatedArchdg)
					archdgStr = updatedArchdg
				}

				// 重新收集更新后的磁盘列表
				allDisks = []string{}
				for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
					if dgStr == "" {
						continue
					}
					parts := strings.SplitN(dgStr, ":", 2)
					if len(parts) == 2 {
						for _, d := range strings.Split(parts[1], ",") {
							d = strings.TrimSpace(d)
							if d != "" {
								allDisks = append(allDisks, d)
							}
						}
					}
				}
			}

			// 第五步：/dev/mapper/ → /dev/yfs/ 路径替换
			// B-029 已根据 diskgroup 创建 /dev/yfs/sys{i}, data{i}, arch{i} 符号链接
			ctx.Logger.Info("Checking /dev/yfs/ symlinks...")

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			updateDiskGroupToYfs := func(dgStr, prefix string) string {
				if dgStr == "" {
					return ""
				}
				parts := strings.SplitN(dgStr, ":", 2)
				if len(parts) != 2 {
					return dgStr
				}
				dgName := parts[0]
				disks := strings.Split(parts[1], ",")
				var updatedDisks []string
				for i, disk := range disks {
					disk = strings.TrimSpace(disk)
					if disk == "" {
						continue
					}
					alias := fmt.Sprintf("%s%d", prefix, i+1)
					yfsPath := fmt.Sprintf("/dev/yfs/%s", alias)
					checkResult, _ := hctx.Execute(fmt.Sprintf("test -L %s || test -b %s", yfsPath, yfsPath), false)
					if checkResult != nil && checkResult.GetExitCode() == 0 {
						updatedDisks = append(updatedDisks, yfsPath)
						hctx.Logger.Info("  %s -> %s", disk, yfsPath)
					} else {
						hctx.Logger.Warn("  /dev/yfs/%s not found, keeping path %s", alias, disk)
						updatedDisks = append(updatedDisks, disk)
					}
				}
				return fmt.Sprintf("%s:%s", dgName, strings.Join(updatedDisks, ","))
			}

			updatedSystemdgYfs := updateDiskGroupToYfs(systemdgStr, "sys")
			updatedDatadgYfs := updateDiskGroupToYfs(datadgStr, "data")
			updatedArchdgYfs := updateDiskGroupToYfs(archdgStr, "arch")

			if updatedSystemdgYfs != systemdgStr {
				ctx.Params["yac_systemdg"] = updatedSystemdgYfs
				ctx.Logger.Info("Updated yac_systemdg: %s -> %s", systemdgStr, updatedSystemdgYfs)
				systemdgStr = updatedSystemdgYfs
			}
			if updatedDatadgYfs != datadgStr {
				ctx.Params["yac_datadg"] = updatedDatadgYfs
				ctx.Logger.Info("Updated yac_datadg: %s -> %s", datadgStr, updatedDatadgYfs)
				datadgStr = updatedDatadgYfs
			}
			if updatedArchdgYfs != archdgStr {
				ctx.Params["yac_archdg"] = updatedArchdgYfs
				ctx.Logger.Info("Updated yac_archdg: %s -> %s", archdgStr, updatedArchdgYfs)
				archdgStr = updatedArchdgYfs
			}

			allDisks = []string{}
			for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
				if dgStr == "" {
					continue
				}
				parts := strings.SplitN(dgStr, ":", 2)
				if len(parts) == 2 {
					for _, d := range strings.Split(parts[1], ",") {
						d = strings.TrimSpace(d)
						if d != "" {
							allDisks = append(allDisks, d)
						}
					}
				}
			}

			// 第六步：验证磁盘的可见性、属主和权限
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				hctx.Logger.Info("Validating shared disk visibility, ownership and permissions on %s...", th.Host)
				for _, disk := range allDisks {
					hctx.Logger.Info("Checking disk: %s", disk)
					// 1. 检查磁盘是否存在
					result, _ := hctx.Execute(fmt.Sprintf("test -b %s || test -e %s", disk, disk), false)
					if result == nil || result.GetExitCode() != 0 {
						return fmt.Errorf("disk %s not found on %s", disk, th.Host)
					}

					// 2. 获取磁盘的详细信息
					result, _ = hctx.Execute(fmt.Sprintf("ls -l %s", disk), false)
					if result != nil {
						hctx.Logger.Info("  %s", strings.TrimSpace(result.GetStdout()))
					}

					// 3. 解析符号链接或多路径设备到真实块设备
					var realDevice string
					if commonos.IsMultipathDisk(disk) || strings.HasPrefix(disk, "/dev/yfs/") {
						cmd := fmt.Sprintf("readlink -f %s 2>/dev/null", disk)
						result, _ := hctx.Execute(cmd, false)
						if result != nil && result.GetStdout() != "" {
							realDevice = strings.TrimSpace(result.GetStdout())
							hctx.Logger.Info("  %s -> real device: %s", disk, realDevice)
						}

						if realDevice == "" {
							return fmt.Errorf("failed to resolve real device for %s on %s", disk, th.Host)
						}
					} else {
						realDevice = disk
					}

					// 4. 验证真实设备的属主和权限
					result, _ = hctx.Execute(fmt.Sprintf("stat -c '%%U:%%G %%a' %s", realDevice), false)
					if result == nil || result.GetStdout() == "" {
						return fmt.Errorf("failed to get ownership and permissions for device %s (from %s) on %s", realDevice, disk, th.Host)
					}

					ownerInfo := strings.TrimSpace(result.GetStdout())
					parts := strings.Fields(ownerInfo)
					if len(parts) < 2 {
						return fmt.Errorf("invalid ownership info for device %s (from %s) on %s: %s", realDevice, disk, th.Host, ownerInfo)
					}

					ownerGroup := parts[0] // 格式: user:group
					perm := parts[1]       // 权限

					ownerParts := strings.Split(ownerGroup, ":")
					if len(ownerParts) < 2 {
						return fmt.Errorf("invalid owner format for device %s (from %s) on %s: %s", realDevice, disk, th.Host, ownerGroup)
					}

					actualUser := ownerParts[0]
					actualGroup := ownerParts[1]

					// 5. 验证属主
					if actualUser != user || actualGroup != group {
						return fmt.Errorf("disk %s (real device: %s) on %s has incorrect ownership: %s:%s, expected %s:%s",
							disk, realDevice, th.Host, actualUser, actualGroup, user, group)
					}

					// 6. 验证权限（检查权限是否 >= 666，即至少 rw-rw-rw-）
					permInt, err := strconv.Atoi(perm)
					if err != nil {
						return fmt.Errorf("invalid permission format for device %s (from %s) on %s: %s", realDevice, disk, th.Host, perm)
					}

					expectedPermInt, _ := strconv.Atoi(expectedPerm)
					if permInt < expectedPermInt {
						return fmt.Errorf("disk %s (real device: %s) on %s has insufficient permissions: %s, expected at least %s",
							disk, realDevice, th.Host, perm, expectedPerm)
					}

					if disk != realDevice {
						hctx.Logger.Info("  OK: %s (real: %s): %s:%s, perm %s", disk, realDevice, actualUser, actualGroup, perm)
					} else {
						hctx.Logger.Info("  OK: %s: %s:%s, perm %s", disk, actualUser, actualGroup, perm)
					}
				}
				hctx.Logger.Info("Expected ownership: %s:%s, permissions: %s", user, group, expectedPerm)
				hctx.Logger.Info("Shared disk validation completed on %s", th.Host)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
