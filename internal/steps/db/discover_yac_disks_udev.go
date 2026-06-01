// discover_yac_disks_udev.go — C-001B：db --skip-os 时从 /dev/yfs（或 mapper）发现 YAC 磁盘组
package db

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

const (
	yfsDiscoverRootDefault = "/dev/yfs"
	mapperDiscoverRoot     = "/dev/mapper"
	diskGroupNameSystem    = "systemdg"
	diskGroupNameData      = "datadg"
	diskGroupNameArch      = "archdg"
)

var (
	reAliasSys  = regexp.MustCompile(`^sys(\d+)$`)
	reAliasData = regexp.MustCompile(`^data(\d+)$`)
	reAliasArch = regexp.MustCompile(`^arch(\d+)$`)
)

// yacAliasLayout 单节点上按别名分组的设备路径（如 sys1 -> /dev/yfs/sys1）。
type yacAliasLayout struct {
	Sys  []string // 已排序的完整路径
	Data []string
	Arch []string
}

// ShouldRunYACUdevDiskDiscovery 判断是否执行 C-001B。
func ShouldRunYACUdevDiskDiscovery(params map[string]interface{}) bool {
	if !getParamBool(params, "yac_mode", false) {
		return false
	}
	if !getParamBool(params, "db_skip_os", false) {
		return false
	}
	if !getParamBool(params, "yac_auto_discover_disks", false) {
		return false
	}
	if getParamString(params, "yac_systemdg", "") != "" {
		return false
	}
	if getParamString(params, "yac_datadg", "") != "" {
		return false
	}
	if getParamString(params, "yac_archdg", "") != "" {
		return false
	}
	return true
}

// RunYACUdevDiskDiscovery 在 YAC + skip-os 且 diskgroup 为空时，从 udev 视图发现共享盘并写入 params。
func RunYACUdevDiskDiscovery(hosts []HostExec, params map[string]interface{}, logger *logging.Logger) error {
	if len(hosts) == 0 {
		return fmt.Errorf("YAC udev disk discovery requires at least one target host")
	}
	if !ShouldRunYACUdevDiskDiscovery(params) {
		logger.Info("C-001B: skipped (disk groups configured or auto-discover disabled)")
		return nil
	}

	firstHost := hosts[0].Host
	logger.ConsoleWithType("C-001B", "YAC Udev Disk Discovery", firstHost, "start", "", "", 0)

	root := getParamString(params, "yac_discover_root", yfsDiscoverRootDefault)
	fallbackMapper := getParamBool(params, "yac_discover_fallback_mapper", true)
	archEnable := getParamBool(params, "yac_archdg_enable", false)
	excludes := commonos.ParseYACExcludeDisks(getParamString(params, "yac_exclude_disks", ""))

	logger.Info("C-001B: discovering YAC disk groups (root=%s fallback_mapper=%v)", root, fallbackMapper)
	if len(excludes) > 0 {
		logger.Info("C-001B: exclude disks: %v", excludes)
	}
	logger.Debug(logging.LogEntry{
		Host: firstHost, StepID: "C-001B", Level: "debug",
		Phase:   "discover-plan",
		Message: fmt.Sprintf("hosts=%d root=%s arch_enable=%v excludes=%d", len(hosts), root, archEnable, len(excludes)),
	})

	perHost := make(map[string]yacAliasLayout, len(hosts))
	for _, h := range hosts {
		layout, source, err := discoverLayoutOnHost(h, root, fallbackMapper, excludes, logger)
		if err != nil {
			return fmt.Errorf("node %s: %w", h.Host, err)
		}
		if len(layout.Sys) == 0 && len(layout.Data) == 0 {
			return fmt.Errorf("node %s: no sys*/data* disks under %s (source=%s); ensure OS udev has created /dev/yfs links",
				h.Host, root, source)
		}
		perHost[h.Host] = layout
		logger.Info("Node %s: source=%s sys=%d data=%d arch=%d",
			h.Host, source, len(layout.Sys), len(layout.Data), len(layout.Arch))
	}

	if err := validateCrossNodeAliasLayouts(perHost); err != nil {
		return err
	}

	ref := perHost[hosts[0].Host]
	if err := validateSystemDiskCount(len(ref.Sys)); err != nil {
		return err
	}
	if len(ref.Data) == 0 {
		if len(excludes) > 0 {
			return fmt.Errorf("no data disk aliases left after exclusions under %s (check --yac-exclude-disks)", root)
		}
		return fmt.Errorf("no data disk aliases found (expected data1 or more under %s)", root)
	}

	if err := verifyBlockDevicesOnAllHosts(hosts, ref, archEnable); err != nil {
		return err
	}

	systemdg := generateDiskGroupParamFromPaths(diskGroupNameSystem, ref.Sys)
	datadg := generateDiskGroupParamFromPaths(diskGroupNameData, ref.Data)
	params["yac_systemdg"] = systemdg
	params["yac_datadg"] = datadg

	archdg := ""
	if archEnable && len(ref.Arch) > 0 {
		archdg = generateDiskGroupParamFromPaths(diskGroupNameArch, ref.Arch)
		params["yac_archdg"] = archdg
	}

	logger.Info("C-001B discovery results:")
	logger.Info("  %s", systemdg)
	logger.Info("  %s", datadg)
	if archdg != "" {
		logger.Info("  %s", archdg)
	}

	logger.Debug(logging.LogEntry{
		Host: firstHost, StepID: "C-001B", Level: "debug",
		Phase:   "discover-done",
		Message: fmt.Sprintf("systemdg_paths=%d datadg_paths=%d", len(ref.Sys), len(ref.Data)),
	})

	logger.ConsoleWithType("C-001B", "YAC Udev Disk Discovery", firstHost, "success", "",
		fmt.Sprintf("system=%d data=%d", len(ref.Sys), len(ref.Data)), time.Duration(0))
	return nil
}

func discoverLayoutOnHost(h HostExec, yfsRoot string, fallbackMapper bool, excludes []string, logger *logging.Logger) (yacAliasLayout, string, error) {
	layout, err := listYfsLayout(h, yfsRoot)
	if err != nil {
		return yacAliasLayout{}, "", err
	}
	source := yfsRoot
	if len(layout.Sys) == 0 && len(layout.Data) == 0 {
		if !fallbackMapper {
			return filterYACAliasLayoutOnHost(h, layout, excludes, logger), source, nil
		}
		logger.Warn("Node %s: empty %s, trying %s", h.Host, yfsRoot, mapperDiscoverRoot)
		mapperLayout, err := listMapperLayout(h, mapperDiscoverRoot)
		if err != nil {
			return yacAliasLayout{}, "", err
		}
		layout = mapperLayout
		source = mapperDiscoverRoot
	}
	return filterYACAliasLayoutOnHost(h, layout, excludes, logger), source, nil
}

func filterYACAliasLayoutOnHost(h HostExec, layout yacAliasLayout, excludes []string, logger *logging.Logger) yacAliasLayout {
	if len(excludes) == 0 {
		return layout
	}
	beforeSys, beforeData, beforeArch := len(layout.Sys), len(layout.Data), len(layout.Arch)
	layout.Sys = filterExcludedDiskPathsOnHost(h, layout.Sys, excludes)
	layout.Data = filterExcludedDiskPathsOnHost(h, layout.Data, excludes)
	layout.Arch = filterExcludedDiskPathsOnHost(h, layout.Arch, excludes)
	if dropped := beforeSys + beforeData + beforeArch - len(layout.Sys) - len(layout.Data) - len(layout.Arch); dropped > 0 {
		logger.Info("Node %s: excluded %d disk path(s) via --yac-exclude-disks", h.Host, dropped)
	}
	return layout
}

func filterExcludedDiskPathsOnHost(h HostExec, paths []string, excludes []string) []string {
	if len(excludes) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if yacDiskPathExcludedOnHost(h, p, excludes) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func yacDiskPathExcludedOnHost(h HostExec, diskPath string, excludes []string) bool {
	if commonos.IsDiskPathExcluded(diskPath, excludes) {
		return true
	}
	canon := resolveDiskCanonicalOnHost(h, diskPath)
	return commonos.IsDiskPathExcluded(canon, excludes)
}

func resolveDiskCanonicalOnHost(h HostExec, diskPath string) string {
	diskQ := commonos.ShellSingleQuote(diskPath)
	result, err := h.Executor.Execute(fmt.Sprintf("readlink -f %s 2>/dev/null || true", diskQ), false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return diskPath
	}
	canon := strings.TrimSpace(result.GetStdout())
	if canon == "" {
		return diskPath
	}
	return canon
}

func listYfsLayout(h HostExec, root string) (yacAliasLayout, error) {
	rootQ := commonos.ShellSingleQuote(strings.TrimRight(root, "/"))
	cmd := fmt.Sprintf(`ls -1 %s 2>/dev/null || true`, rootQ)
	result, err := h.Executor.Execute(cmd, false)
	if err != nil {
		return yacAliasLayout{}, err
	}
	var names []string
	if result != nil && result.GetStdout() != "" {
		for _, line := range strings.Split(result.GetStdout(), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				names = append(names, line)
			}
		}
	}
	return namesToLayout(root, names), nil
}

func listMapperLayout(h HostExec, root string) (yacAliasLayout, error) {
	cmd := fmt.Sprintf(`ls -1 %s 2>/dev/null || true`, commonos.ShellSingleQuote(strings.TrimRight(root, "/")))
	result, err := h.Executor.Execute(cmd, false)
	if err != nil {
		return yacAliasLayout{}, err
	}
	var names []string
	if result != nil && result.GetStdout() != "" {
		for _, line := range strings.Split(result.GetStdout(), "\n") {
			name := strings.TrimSpace(line)
			if name == "" || name == "control" {
				continue
			}
			if reAliasSys.MatchString(name) || reAliasData.MatchString(name) || reAliasArch.MatchString(name) {
				names = append(names, name)
			}
		}
	}
	return namesToLayout(root, names), nil
}

func namesToLayout(root string, names []string) yacAliasLayout {
	root = strings.TrimRight(root, "/")
	var layout yacAliasLayout
	for _, name := range names {
		full := path.Join(root, name)
		switch {
		case reAliasSys.MatchString(name):
			layout.Sys = append(layout.Sys, full)
		case reAliasData.MatchString(name):
			layout.Data = append(layout.Data, full)
		case reAliasArch.MatchString(name):
			layout.Arch = append(layout.Arch, full)
		}
	}
	sortPathsByAliasNumber(layout.Sys, reAliasSys)
	sortPathsByAliasNumber(layout.Data, reAliasData)
	sortPathsByAliasNumber(layout.Arch, reAliasArch)
	return layout
}

func sortPathsByAliasNumber(paths []string, re *regexp.Regexp) {
	if len(paths) <= 1 {
		return
	}
	sort.Slice(paths, func(i, j int) bool {
		return aliasIndex(paths[i], re) < aliasIndex(paths[j], re)
	})
}

func aliasIndex(fullPath string, re *regexp.Regexp) int {
	base := path.Base(fullPath)
	m := re.FindStringSubmatch(base)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func validateCrossNodeAliasLayouts(perHost map[string]yacAliasLayout) error {
	var refHost string
	var ref yacAliasLayout
	for host, layout := range perHost {
		refHost = host
		ref = layout
		break
	}
	refKey := aliasSetKey(ref)
	for host, layout := range perHost {
		if host == refHost {
			continue
		}
		if aliasSetKey(layout) != refKey {
			return fmt.Errorf("disk alias set mismatch: node %s has %s, node %s has %s",
				refHost, refKey, host, aliasSetKey(layout))
		}
	}
	return nil
}

func aliasSetKey(l yacAliasLayout) string {
	return fmt.Sprintf("sys:%s|data:%s|arch:%s",
		strings.Join(basenames(l.Sys), ","),
		strings.Join(basenames(l.Data), ","),
		strings.Join(basenames(l.Arch), ","))
}

func basenames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = path.Base(p)
	}
	return out
}

func validateSystemDiskCount(n int) error {
	switch n {
	case 1, 3, 5:
		return nil
	default:
		return fmt.Errorf("invalid system disk count %d (YAC requires 1, 3, or 5 sys disks)", n)
	}
}

func verifyBlockDevicesOnAllHosts(hosts []HostExec, ref yacAliasLayout, archEnable bool) error {
	var paths []string
	paths = append(paths, ref.Sys...)
	paths = append(paths, ref.Data...)
	if archEnable {
		paths = append(paths, ref.Arch...)
	}
	for _, h := range hosts {
		for _, disk := range paths {
			diskQ := commonos.ShellSingleQuote(disk)
			result, err := h.Executor.Execute(fmt.Sprintf("test -b %s || test -L %s", diskQ, diskQ), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("disk %s not available on node %s", disk, h.Host)
			}
		}
	}
	return nil
}

func generateDiskGroupParamFromPaths(dgName string, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%s", dgName, strings.Join(paths, ","))
}

// validateYACDiskGroupsExcludeDisks 若 diskgroup 仍含 --yac-exclude-disks 中的盘则失败（C-012 兜底）。
func validateYACDiskGroupsExcludeDisks(ctx *runner.StepContext) error {
	excludes := commonos.ParseYACExcludeDisks(ctx.GetParamString("yac_exclude_disks", ""))
	if len(excludes) == 0 {
		return nil
	}
	for _, dgStr := range []string{
		ctx.GetParamString("yac_systemdg", ""),
		ctx.GetParamString("yac_datadg", ""),
		ctx.GetParamString("yac_archdg", ""),
	} {
		if dgStr == "" {
			continue
		}
		for _, p := range diskPathsFromDiskGroupParam(dgStr) {
			if commonos.IsDiskPathExcluded(p, excludes) {
				return fmt.Errorf("disk %q is in --yac-exclude-disks but still present in diskgroup %q", p, dgStr)
			}
		}
	}
	return nil
}

func diskPathsFromDiskGroupParam(dgStr string) []string {
	parts := strings.SplitN(dgStr, ":", 2)
	if len(parts) != 2 {
		return nil
	}
	var paths []string
	for _, d := range strings.Split(parts[1], ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			paths = append(paths, d)
		}
	}
	return paths
}

// classifyYfsAliasNames 将 ls 输出的名称分为 sys/data/arch（单测用）。
func classifyYfsAliasNames(names []string) (sys, data, arch []string) {
	layout := namesToLayout(yfsDiscoverRootDefault, names)
	return basenames(layout.Sys), basenames(layout.Data), basenames(layout.Arch)
}

// AllDiskPathsUnderYfs 判断路径列表是否均在 /dev/yfs/ 下（C-012 短路用）。
func AllDiskPathsUnderYfs(diskPaths []string) bool {
	if len(diskPaths) == 0 {
		return false
	}
	for _, p := range diskPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, yfsDiscoverRootDefault+"/") && p != yfsDiscoverRootDefault {
			return false
		}
	}
	return true
}
