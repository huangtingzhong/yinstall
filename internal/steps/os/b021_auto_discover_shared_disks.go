package os

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// DiskInfo 表示从单个节点采集到的磁盘信息
type DiskInfo struct {
	Path string // 磁盘路径，如 /dev/sdc
	WWID string // 全局唯一标识符
	Size int64  // 大小（字节）
	Node string // 节点主机名
}

// StepB021AutoDiscoverSharedDisks YAC 模式下自动发现各节点共享磁盘
// 当 yac_systemdg、yac_datadg、yac_archdg 都未配置时，自动发现共享磁盘
func StepB021AutoDiscoverSharedDisks() *runner.Step {
	return &runner.Step{
		ID:          "B-021",
		Name:        "Auto Discover Shared Disks",
		Description: "Automatically discover shared disks across all YAC nodes",
		Tags:        []string{"os", "yac", "diskgroup", "auto-discover"},
		Optional:    true,
		Global:      true,

		PreCheck: func(ctx *runner.StepContext) error {
			// 只在 YAC 模式下运行
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if !isYACMode {
				return fmt.Errorf("not in YAC mode, skipping auto-discovery")
			}

			// 检查是否已经配置了磁盘组参数
			systemdg := ctx.GetParamString("yac_systemdg", "")
			datadg := ctx.GetParamString("yac_datadg", "")
			archdg := ctx.GetParamString("yac_archdg", "")

			// 如果任何一个磁盘组已配置，跳过自动发现
			if systemdg != "" || datadg != "" || archdg != "" {
				return fmt.Errorf("disk groups already configured, skipping auto-discovery")
			}

			// 检查是否有多个节点
			hosts := ctx.HostsToRun()
			if len(hosts) < 2 {
				return fmt.Errorf("auto-discovery requires at least 2 nodes")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Starting automatic shared disk discovery...")

			// 获取配置参数
			diskPattern := ctx.GetParamString("yac_disk_pattern", "")
			excludeDisksStr := ctx.GetParamString("yac_exclude_disks", "")
			systemdgSizeMaxStr := ctx.GetParamString("yac_systemdg_size_max", "10G")

			// 解析排除列表
			var excludeDisks []string
			if excludeDisksStr != "" {
				for _, disk := range strings.Split(excludeDisksStr, ",") {
					disk = strings.TrimSpace(disk)
					if disk != "" {
						excludeDisks = append(excludeDisks, disk)
					}
				}
			}

			// 解析 systemdg 大小阈值
			systemdgSizeMax, err := parseSizeToBytes(systemdgSizeMaxStr)
			if err != nil {
				return fmt.Errorf("invalid yac_systemdg_size_max: %w", err)
			}

			ctx.Logger.Info("Configuration:")
			if diskPattern != "" {
				ctx.Logger.Info("  Disk pattern: %s", diskPattern)
			}
			if len(excludeDisks) > 0 {
				ctx.Logger.Info("  Exclude disks: %v", excludeDisks)
			}
			ctx.Logger.Info("  SystemDG size threshold: %s (< %d bytes)", systemdgSizeMaxStr, systemdgSizeMax)

			// 步骤 1: 在所有节点上扫描磁盘
			allNodeDisks, err := scanDisksOnAllNodes(ctx, diskPattern, excludeDisks)
			if err != nil {
				return fmt.Errorf("failed to scan disks: %w", err)
			}

			if len(allNodeDisks) == 0 {
				return fmt.Errorf("no disks found on any node")
			}

			// 步骤 2: 跨节点匹配共享磁盘（基于 WWID）
			sharedDisks, err := matchSharedDisks(ctx, allNodeDisks)
			if err != nil {
				return fmt.Errorf("failed to match shared disks: %w", err)
			}

			if len(sharedDisks) == 0 {
				return fmt.Errorf("no shared disks found across all nodes")
			}

			ctx.Logger.Info("Found %d shared disks across all nodes", len(sharedDisks))

			// 步骤 3: 按大小分类磁盘
			archdgEnabled := ctx.GetParamBool("yac_archdg_enable", false)
			systemdgDisks, datadgDisks, archdgDisk, err := classifyDisksBySize(ctx, sharedDisks, systemdgSizeMax, archdgEnabled)
			if err != nil {
				return fmt.Errorf("failed to classify disks: %w", err)
			}

			// 步骤 4: 生成磁盘组参数
			if len(systemdgDisks) == 0 {
				return fmt.Errorf("no disks found for systemdg (size < %s)", systemdgSizeMaxStr)
			}
			if len(datadgDisks) == 0 && archdgDisk == nil {
				return fmt.Errorf("no disks found for datadg (size >= %s)", systemdgSizeMaxStr)
			}

			systemdgParam := generateDiskGroupParam("systemdg", systemdgDisks)
			datadgParam := generateDiskGroupParam("datadg", datadgDisks)
			archdgParam := ""
			if archdgDisk != nil {
				archdgParam = generateDiskGroupParam("archdg", []*DiskInfo{archdgDisk})
			}

			// 步骤 5: 更新上下文参数
			ctx.Params["yac_systemdg"] = systemdgParam
			ctx.Params["yac_datadg"] = datadgParam
			if archdgParam != "" {
				ctx.Params["yac_archdg"] = archdgParam
			}

			// 步骤 6: 输出结果
			ctx.Logger.Info("")
			ctx.Logger.Info("=== Auto-Discovery Results ===")
			ctx.Logger.Info("SystemDG: %s", systemdgParam)
			ctx.Logger.Info("DataDG:   %s", datadgParam)
			if archdgParam != "" {
				ctx.Logger.Info("ArchDG:   %s", archdgParam)
			} else {
				ctx.Logger.Info("ArchDG:   (disabled, all data disks assigned to DataDG)")
			}
			ctx.Logger.Info("==============================")
			ctx.Logger.Info("")

			// 保存结果供后续步骤使用
			ctx.SetResult("auto_discovered_systemdg", systemdgParam)
			ctx.SetResult("auto_discovered_datadg", datadgParam)
			ctx.SetResult("auto_discovered_archdg", archdgParam)

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// 验证生成的参数格式正确
			systemdg := ctx.GetParamString("yac_systemdg", "")
			datadg := ctx.GetParamString("yac_datadg", "")

			if systemdg == "" {
				return fmt.Errorf("systemdg not generated")
			}
			if datadg == "" {
				return fmt.Errorf("datadg not generated")
			}

			// 验证格式
			if _, err := ParseDiskGroupConfig(systemdg); err != nil {
				return fmt.Errorf("invalid systemdg format: %w", err)
			}
			if _, err := ParseDiskGroupConfig(datadg); err != nil {
				return fmt.Errorf("invalid datadg format: %w", err)
			}

			archdg := ctx.GetParamString("yac_archdg", "")
			if archdg != "" {
				if _, err := ParseDiskGroupConfig(archdg); err != nil {
					return fmt.Errorf("invalid archdg format: %w", err)
				}
			}

			return nil
		},
	}
}

// scanDisksOnAllNodes 在所有节点上扫描磁盘
func scanDisksOnAllNodes(ctx *runner.StepContext, diskPattern string, excludeDisks []string) (map[string][]*DiskInfo, error) {
	allNodeDisks := make(map[string][]*DiskInfo)
	hosts := ctx.HostsToRun()

	ctx.Logger.Info("Scanning disks on %d nodes...", len(hosts))

	for _, host := range hosts {
		hostCtx := ctx.ForHost(host)
		disks, err := scanDisksOnNode(hostCtx, diskPattern, excludeDisks)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node %s: %w", host.Host, err)
		}

		allNodeDisks[host.Host] = disks
		ctx.Logger.Info("  Node %s: found %d candidate disks", host.Host, len(disks))
	}

	return allNodeDisks, nil
}

// scanDisksOnNode 在单个节点上扫描磁盘
func scanDisksOnNode(ctx *runner.StepContext, diskPattern string, excludeDisks []string) ([]*DiskInfo, error) {
	// 获取所有块设备
	cmd := "lsblk -d -n -b -o NAME,SIZE,TYPE 2>/dev/null | awk '$3==\"disk\" {print $1,$2}'"
	result, err := ctx.Execute(cmd, false)
	if err != nil || result.GetExitCode() != 0 {
		return nil, fmt.Errorf("failed to list block devices: %w", err)
	}

	var disks []*DiskInfo
	lines := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		diskName := fields[0]
		diskPath := "/dev/" + diskName
		sizeStr := fields[1]

		// 解析大小
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			ctx.Logger.Warn("Failed to parse size for %s: %v", diskPath, err)
			continue
		}

		// 跳过排除列表中的磁盘
		if contains(excludeDisks, diskPath) {
			continue
		}

		// 应用磁盘模式过滤（如果指定）
		if diskPattern != "" {
			// 简单的通配符匹配
			if !matchPattern(diskPath, diskPattern) {
				continue
			}
		}

		// 检查磁盘是否已挂载
		mountCheckCmd := fmt.Sprintf("lsblk -n -o MOUNTPOINT %s 2>/dev/null | grep -v '^$' | head -1", diskPath)
		mountResult, _ := ctx.Execute(mountCheckCmd, false)
		if mountResult != nil && mountResult.GetExitCode() == 0 {
			mountPoint := strings.TrimSpace(mountResult.GetStdout())
			if mountPoint != "" {
				continue
			}
		}

		// 检查磁盘是否有分区
		partCheckCmd := fmt.Sprintf("lsblk -n -o TYPE %s 2>/dev/null | grep -c part || true", diskPath)
		partResult, _ := ctx.Execute(partCheckCmd, false)
		if partResult != nil && partResult.GetExitCode() == 0 {
			partCount := strings.TrimSpace(partResult.GetStdout())
			if partCount != "0" && partCount != "" {
				continue
			}
		}

		// 获取 WWID
		wwid, err := getWWID(ctx, diskPath)
		if err != nil || wwid == "" {
			ctx.Logger.Warn("Failed to get WWID for %s, skipping: %v", diskPath, err)
			continue
		}

		disks = append(disks, &DiskInfo{
			Path: diskPath,
			WWID: wwid,
			Size: size,
			Node: ctx.Executor.Host(),
		})
	}

	return disks, nil
}

// getWWID 获取磁盘的 WWID
func getWWID(ctx *runner.StepContext, diskPath string) (string, error) {
	// 尝试多种方法获取 WWID
	methods := []string{
		fmt.Sprintf("/lib/udev/scsi_id -g -u -d %s 2>/dev/null", diskPath),
		fmt.Sprintf("udevadm info --query=property --name=%s 2>/dev/null | grep 'ID_SERIAL=' | head -1 | cut -d= -f2", diskPath),
		fmt.Sprintf("lsblk -n -o SERIAL %s 2>/dev/null | head -1", diskPath),
	}

	for _, cmd := range methods {
		result, err := ctx.Execute(cmd, false)
		if err == nil && result.GetExitCode() == 0 {
			wwid := strings.TrimSpace(result.GetStdout())
			if wwid != "" && wwid != "unknown" && wwid != "none" {
				return wwid, nil
			}
		}
	}

	return "", fmt.Errorf("no WWID found for %s", diskPath)
}

// matchSharedDisks 匹配所有节点上都存在的共享磁盘
// 策略：先尝试 WWID 精确匹配（每个 WWID 对应每节点恰好 1 块盘）；
// 若 WWID 不可靠（如 VMware 虚拟 NVMe 所有磁盘同 WWID），回退到路径+大小匹配。
func matchSharedDisks(ctx *runner.StepContext, allNodeDisks map[string][]*DiskInfo) ([]*DiskInfo, error) {
	if len(allNodeDisks) == 0 {
		return nil, fmt.Errorf("no node disk data")
	}

	nodeCount := len(allNodeDisks)

	// 尝试 WWID 精确匹配
	sharedDisks := matchByWWID(ctx, allNodeDisks, nodeCount)
	if len(sharedDisks) > 0 {
		ctx.Logger.Info("Matched %d shared disks by WWID", len(sharedDisks))
		return sharedDisks, nil
	}

	// WWID 匹配失败，回退到路径+大小匹配
	ctx.Logger.Info("WWID matching unreliable (possibly VMware/virtual environment), falling back to path+size matching")
	sharedDisks = matchByPathAndSize(ctx, allNodeDisks, nodeCount)
	if len(sharedDisks) > 0 {
		ctx.Logger.Info("Matched %d shared disks by path+size", len(sharedDisks))
		return sharedDisks, nil
	}

	return nil, nil
}

// matchByWWID 基于 WWID 精确匹配：每个 WWID 在每个节点上恰好出现 1 次，且大小一致
func matchByWWID(ctx *runner.StepContext, allNodeDisks map[string][]*DiskInfo, nodeCount int) []*DiskInfo {
	wwidMap := make(map[string][]*DiskInfo)
	for _, disks := range allNodeDisks {
		for _, disk := range disks {
			wwidMap[disk.WWID] = append(wwidMap[disk.WWID], disk)
		}
	}

	var sharedDisks []*DiskInfo
	for wwid, disks := range wwidMap {
		nodeSet := make(map[string]bool)
		for _, disk := range disks {
			nodeSet[disk.Node] = true
		}

		if len(nodeSet) != nodeCount {
			continue
		}

		// 每个 WWID 在每个节点上应该恰好出现 1 次，否则 WWID 不可靠
		if len(disks) != nodeCount {
			ctx.Logger.Warn("WWID %s appears %d times across %d nodes (expected %d), WWID unreliable",
				wwid, len(disks), nodeCount, nodeCount)
			return nil
		}

		firstDisk := disks[0]
		sizeConsistent := true
		for _, disk := range disks[1:] {
			sizeDiff := abs(disk.Size - firstDisk.Size)
			if firstDisk.Size > 0 && float64(sizeDiff)/float64(firstDisk.Size) > 0.01 {
				ctx.Logger.Warn("WWID %s has inconsistent sizes: %d vs %d", wwid, firstDisk.Size, disk.Size)
				sizeConsistent = false
				break
			}
		}

		if sizeConsistent {
			sharedDisks = append(sharedDisks, firstDisk)
		}
	}

	sort.Slice(sharedDisks, func(i, j int) bool {
		return sharedDisks[i].Size < sharedDisks[j].Size
	})

	return sharedDisks
}

// matchByPathAndSize 基于路径名+大小匹配：同一路径在所有节点上都存在且大小一致
func matchByPathAndSize(ctx *runner.StepContext, allNodeDisks map[string][]*DiskInfo, nodeCount int) []*DiskInfo {
	// path -> node -> DiskInfo
	pathMap := make(map[string]map[string]*DiskInfo)
	for _, disks := range allNodeDisks {
		for _, disk := range disks {
			if pathMap[disk.Path] == nil {
				pathMap[disk.Path] = make(map[string]*DiskInfo)
			}
			pathMap[disk.Path][disk.Node] = disk
		}
	}

	var sharedDisks []*DiskInfo
	for path, nodeDisks := range pathMap {
		if len(nodeDisks) != nodeCount {
			continue
		}

		var refDisk *DiskInfo
		sizeConsistent := true
		for _, disk := range nodeDisks {
			if refDisk == nil {
				refDisk = disk
				continue
			}
			sizeDiff := abs(disk.Size - refDisk.Size)
			if refDisk.Size > 0 && float64(sizeDiff)/float64(refDisk.Size) > 0.01 {
				ctx.Logger.Warn("Path %s has inconsistent sizes across nodes: %d vs %d", path, refDisk.Size, disk.Size)
				sizeConsistent = false
				break
			}
		}

		if sizeConsistent && refDisk != nil {
			sharedDisks = append(sharedDisks, refDisk)
		}
	}

	sort.Slice(sharedDisks, func(i, j int) bool {
		return sharedDisks[i].Size < sharedDisks[j].Size
	})

	return sharedDisks
}

// validSystemdgCounts systemdg 允许的磁盘数量
var validSystemdgCounts = []int{5, 3, 1}

// bestValidCount 返回不超过 n 的最大合法 systemdg 磁盘数；无合法值返回 0
func bestValidCount(n int) int {
	for _, c := range validSystemdgCounts {
		if n >= c {
			return c
		}
	}
	return 0
}

// sameSizeTolerance 判断两块磁盘是否"同大小"（差异 <= 1%）
func sameSizeTolerance(a, b int64) bool {
	diff := abs(a - b)
	ref := a
	if b > a {
		ref = b
	}
	if ref == 0 {
		return a == b
	}
	return float64(diff)/float64(ref) <= 0.01
}

// classifyDisksBySize 按大小分类磁盘
// systemdg 要求：数量必须是 1、3 或 5，且所有磁盘大小一致
// splitArchDG=true 时从大盘中拆分一个独立 archdg；false 时所有大盘归 datadg
func classifyDisksBySize(ctx *runner.StepContext, disks []*DiskInfo, systemdgMaxBytes int64, splitArchDG bool) (systemdg, datadg []*DiskInfo, archdg *DiskInfo, err error) {
	if len(disks) == 0 {
		return nil, nil, nil, fmt.Errorf("no disks to classify")
	}

	// 按大小分为"小盘候选"和"大盘"
	var smallDisks []*DiskInfo
	var largeDisks []*DiskInfo

	for _, disk := range disks {
		if disk.Size < systemdgMaxBytes {
			smallDisks = append(smallDisks, disk)
		} else {
			largeDisks = append(largeDisks, disk)
		}
	}

	ctx.Logger.Info("Disk classification:")
	ctx.Logger.Info("  Small disks (< %s): %d", formatBytes(systemdgMaxBytes), len(smallDisks))
	for _, d := range smallDisks {
		ctx.Logger.Info("    - %s (%s)", d.Path, formatBytes(d.Size))
	}
	ctx.Logger.Info("  Large disks (>= %s): %d", formatBytes(systemdgMaxBytes), len(largeDisks))
	for _, d := range largeDisks {
		ctx.Logger.Info("    - %s (%s)", d.Path, formatBytes(d.Size))
	}

	// 小盘按大小排序后分组（同大小一组）
	sort.Slice(smallDisks, func(i, j int) bool { return smallDisks[i].Size < smallDisks[j].Size })

	type sizeGroup struct {
		refSize int64
		disks   []*DiskInfo
	}
	var groups []sizeGroup
	for _, d := range smallDisks {
		placed := false
		for i := range groups {
			if sameSizeTolerance(groups[i].refSize, d.Size) {
				groups[i].disks = append(groups[i].disks, d)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, sizeGroup{refSize: d.Size, disks: []*DiskInfo{d}})
		}
	}

	// 选择最优的 size group：取合法数量最大的组
	var bestGroup *sizeGroup
	bestCount := 0
	for i := range groups {
		vc := bestValidCount(len(groups[i].disks))
		if vc > bestCount {
			bestCount = vc
			bestGroup = &groups[i]
		}
	}

	if bestGroup != nil && bestCount > 0 {
		systemdg = bestGroup.disks[:bestCount]
		ctx.Logger.Info("  SystemDG: selected %d disks of size %s (from %d candidates)",
			bestCount, formatBytes(bestGroup.refSize), len(bestGroup.disks))

		// 未选中的小盘放回大盘池
		selectedSet := make(map[string]bool)
		for _, d := range systemdg {
			selectedSet[d.Path] = true
		}
		for _, d := range smallDisks {
			if !selectedSet[d.Path] {
				largeDisks = append(largeDisks, d)
			}
		}
	} else {
		ctx.Logger.Info("  No valid systemdg group found in small disks")
	}

	// 大盘按大小排序后按大小分组，取最大组用于 datadg/archdg（保证同组大小一致）
	sort.Slice(largeDisks, func(i, j int) bool { return largeDisks[i].Size < largeDisks[j].Size })

	if len(largeDisks) > 0 {
		var lgGroups []sizeGroup
		for _, d := range largeDisks {
			placed := false
			for i := range lgGroups {
				if sameSizeTolerance(lgGroups[i].refSize, d.Size) {
					lgGroups[i].disks = append(lgGroups[i].disks, d)
					placed = true
					break
				}
			}
			if !placed {
				lgGroups = append(lgGroups, sizeGroup{refSize: d.Size, disks: []*DiskInfo{d}})
			}
		}

		// 选择磁盘数最多的组作为数据池；相同数量时优先更大的磁盘
		var dataPool *sizeGroup
		for i := range lgGroups {
			if dataPool == nil || len(lgGroups[i].disks) > len(dataPool.disks) ||
				(len(lgGroups[i].disks) == len(dataPool.disks) && lgGroups[i].refSize > dataPool.refSize) {
				dataPool = &lgGroups[i]
			}
		}

		// 不在数据池中的磁盘作为 archdg 候选
		poolSet := make(map[string]bool)
		for _, d := range dataPool.disks {
			poolSet[d.Path] = true
		}
		var archCandidates []*DiskInfo
		for _, d := range largeDisks {
			if !poolSet[d.Path] {
				archCandidates = append(archCandidates, d)
			}
		}

		if !splitArchDG {
			// ArchDG 未启用：所有大盘归 datadg
			datadg = dataPool.disks
			if len(archCandidates) > 0 {
				ctx.Logger.Warn("  %d disk(s) excluded from diskgroups (size mismatch with data pool):", len(archCandidates))
				for _, d := range archCandidates {
					ctx.Logger.Warn("    - %s (%s)", d.Path, formatBytes(d.Size))
				}
			}
		} else if len(dataPool.disks) > 1 && len(archCandidates) > 0 {
			// ArchDG 启用且有独立 arch 候选
			datadg = dataPool.disks
			archdg = archCandidates[len(archCandidates)-1]
			if len(archCandidates) > 1 {
				ctx.Logger.Warn("  %d disk(s) excluded from diskgroups (size mismatch):", len(archCandidates)-1)
				for _, d := range archCandidates[:len(archCandidates)-1] {
					ctx.Logger.Warn("    - %s (%s)", d.Path, formatBytes(d.Size))
				}
			}
		} else if splitArchDG && len(dataPool.disks) > 1 {
			// ArchDG 启用，无独立候选：从数据池末尾取一个
			datadg = dataPool.disks[:len(dataPool.disks)-1]
			archdg = dataPool.disks[len(dataPool.disks)-1]
		} else {
			// 只有一块盘：全给 datadg
			datadg = dataPool.disks
		}

		ctx.Logger.Info("  DataDG: %d disk(s), size %s each", len(datadg), formatBytes(datadg[0].Size))
		if archdg != nil {
			ctx.Logger.Info("  ArchDG: 1 disk (%s, %s)", archdg.Path, formatBytes(archdg.Size))
		}
	}

	return systemdg, datadg, archdg, nil
}

// generateDiskGroupParam 生成磁盘组参数字符串
func generateDiskGroupParam(dgName string, disks []*DiskInfo) string {
	if len(disks) == 0 {
		return ""
	}

	var paths []string
	for _, disk := range disks {
		paths = append(paths, disk.Path)
	}

	return fmt.Sprintf("%s:%s", dgName, strings.Join(paths, ","))
}

// parseSizeToBytes 将大小字符串（如 "10G", "512M"）转换为字节数
func parseSizeToBytes(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(strings.ToUpper(sizeStr))
	if sizeStr == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// 提取数字和单位
	var value float64
	var unit string

	// 尝试解析格式：数字 + 单位（如 "10G", "512M"）
	n, err := fmt.Sscanf(sizeStr, "%f%s", &value, &unit)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	// 如果没有单位，默认为字节
	if n == 1 {
		return int64(value), nil
	}

	// 移除可能的 'B' 后缀
	unit = strings.TrimSuffix(unit, "B")

	multiplier := int64(1)
	switch unit {
	case "K":
		multiplier = 1024
	case "M":
		multiplier = 1024 * 1024
	case "G":
		multiplier = 1024 * 1024 * 1024
	case "T":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "":
		multiplier = 1
	default:
		return 0, fmt.Errorf("unknown size unit: %s", unit)
	}

	return int64(value * float64(multiplier)), nil
}

// formatBytes 将字节数格式化为人类可读的字符串
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGT"[exp])
}

// contains 检查字符串是否在切片中
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// abs 返回 int64 的绝对值
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// matchPattern 简单的通配符匹配（支持 * 和 ?）
func matchPattern(s, pattern string) bool {
	// 简化版：只支持前缀/后缀匹配
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(s, parts[0]) && strings.HasSuffix(s, parts[1])
		}
	}
	return s == pattern
}
