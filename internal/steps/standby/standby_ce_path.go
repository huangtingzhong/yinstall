// standby_ce_path.go - CE 备集群路径判定、参数校验与 group gen 命令拼装
// 供 E-011/E-013 等步骤在 --yac 且主库为 CE 时复用

package standby

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

// paramStandbyCEPathResolved 标记 standby_ce_path 已由 E-002 或 EnsureStandbyCEPath 解析，避免跳过 E-002 时误走 SE。
const paramStandbyCEPathResolved = "standby_ce_path_resolved"

// EnsureStandbyCEPath 解析并写入 standby_ce_path / primary_is_ce；已解析则直接返回。
// statusHint 非空时复用已有 cluster status 输出，避免 E-002 内重复查询。
// 跳过 E-002 单步跑 E-004/E-011/E-013 时必须先调用，防止 --yac 下误走 node gen/add。
func EnsureStandbyCEPath(ctx *runner.StepContext, statusHint string) error {
	if ctx == nil {
		return fmt.Errorf("step context is nil")
	}
	if ctx.GetParamBool(paramStandbyCEPathResolved, false) {
		return nil
	}

	primaryUser := GetPrimaryOSUser(ctx)
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("resolve CE path: primary env: %w", err)
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		return err
	}
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	if err := EnsurePrimaryStageDirParam(ctx); err != nil {
		return err
	}
	stageDir := strings.TrimSpace(ctx.GetParamString("db_stage_dir", ""))
	if stageDir == "" {
		return fmt.Errorf("resolve CE path: db_stage_dir is empty")
	}

	statusOut := strings.TrimSpace(statusHint)
	if statusOut == "" {
		statusRes, sErr := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile,
			fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
		if sErr != nil {
			return fmt.Errorf("resolve CE path: cluster status: %w", sErr)
		}
		statusOut = statusRes.GetStdout()
	}

	probe := statusOut
	groupOut := ""
	groupRes, gErr := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile,
		fmt.Sprintf("yasboot cluster status -c %s -b group -d", clusterName), true)
	if gErr == nil && groupRes != nil && groupRes.GetExitCode() == 0 {
		groupOut = groupRes.GetStdout()
		probe = probe + "\n" + groupOut
	}
	for _, name := range []string{"hosts.toml", clusterName + ".toml"} {
		catCmd := fmt.Sprintf("test -f %s/%s && cat %s/%s || true", stageDir, name, stageDir, name)
		if catRes, _ := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, catCmd, true); catRes != nil {
			probe = probe + "\n" + catRes.GetStdout()
		}
	}

	primaryIsCE := PrimaryLooksLikeCE(probe)
	yacMode := ctx.GetParamBool("yac_mode", false)
	useCE, pathErr := ResolveStandbyCEPath(yacMode, primaryIsCE)
	if pathErr != nil {
		return pathErr
	}
	ctx.Params["primary_is_ce"] = primaryIsCE
	ctx.Params["standby_ce_path"] = useCE
	ctx.Params[paramStandbyCEPathResolved] = true

	if useCE {
		if ctx.Logger != nil {
			ctx.Logger.Info("Standby expansion path: CE group (config group gen -t ce / group add)")
		}
		if groupOut != "" {
			RecordCEGroupBaseline(ctx, groupOut)
		}
		if err := RequireCEAdminPassword(ctx.GetParamString("db_admin_password", "")); err != nil {
			return err
		}
		if strings.TrimSpace(ctx.GetParamString("yac_public_network", "")) == "" {
			if pn := PublicNetworkFromTOML(probe); pn != "" {
				ctx.Params["yac_public_network"] = pn
				if ctx.Logger != nil {
					ctx.Logger.Info("Inherited yac_public_network=%s from primary toml", pn)
				}
			}
		}
		n := ctx.GetParamInt("standby_node_count", 1)
		if n < 1 {
			n = len(ctx.GetParamStringSlice("standby_targets"))
		}
		if n < 1 {
			n = 1
		}
		if err := ValidateStandbyCEParams(
			ctx.GetParamString("yac_inter_cidr", ""),
			ctx.GetParamString("yac_systemdg", ""),
			ctx.GetParamString("yac_datadg", ""),
			ctx.GetParamStringSlice("yac_vips"),
			n,
		); err != nil {
			return err
		}
	} else if ctx.Logger != nil {
		ctx.Logger.Info("Standby expansion path: SE node (config node gen / node add)")
	}
	return nil
}

// StandbyCEGroupGenParams 拼装 yasboot config group gen -t ce 的参数。
type StandbyCEGroupGenParams struct {
	StageDir      string
	ClusterName   string
	User          string
	Password      string // 已做 shell 单引号转义后的值
	IPs           string
	SSHPort       int
	InstallPath   string
	DataPath      string
	LogPath       string
	BeginPort     int
	NodeCount     int
	SystemDisks   string
	DataDisks     string
	DiskFoundPath string
	VIPs          []string // 裸 IP 或 ip/prefix；拼装时走 db.FormatVIPListForCeGen
	PublicNetwork string
	InterCIDR     string
	ExtraArgs     string
}

// ResolveStandbyCEPath 根据 CLI --yac 与主库是否 CE 决定是否走 CE 备集群路径。
func ResolveStandbyCEPath(yacMode, primaryIsCE bool) (useCE bool, err error) {
	switch {
	case primaryIsCE && yacMode:
		return true, nil
	case primaryIsCE && !yacMode:
		return false, fmt.Errorf("primary is CE (YAC); CE standby expansion requires --yac (do not use node add path)")
	case !primaryIsCE && yacMode:
		return false, fmt.Errorf("primary is SE; --yac CE standby path is not supported (omit --yac for SE node expansion)")
	default:
		return false, nil
	}
}

// ValidateStandbyCEParams 校验 CE 备路径强制参数。
func ValidateStandbyCEParams(interCIDR, systemdg, datadg string, vips []string, nodeCount int) error {
	if nodeCount < 1 {
		return fmt.Errorf("standby node count must be >= 1")
	}
	if strings.TrimSpace(interCIDR) == "" {
		return fmt.Errorf("--yac-inter-cidr is required for CE standby path")
	}
	if _, _, err := net.ParseCIDR(strings.TrimSpace(interCIDR)); err != nil {
		return fmt.Errorf("invalid --yac-inter-cidr %q: %w", interCIDR, err)
	}
	if strings.TrimSpace(systemdg) == "" {
		return fmt.Errorf("--yac-systemdg is required for CE standby path")
	}
	if strings.TrimSpace(datadg) == "" {
		return fmt.Errorf("--yac-datadg is required for CE standby path")
	}
	if _, err := dbsteps.ParseYACDiskGroup(systemdg); err != nil {
		return fmt.Errorf("invalid --yac-systemdg: %w", err)
	}
	if _, err := dbsteps.ParseYACDiskGroup(datadg); err != nil {
		return fmt.Errorf("invalid --yac-datadg: %w", err)
	}
	if err := dbsteps.ValidateYACVIPList(vips, nodeCount); err != nil {
		return fmt.Errorf("CE standby path: %w", err)
	}
	return nil
}

// BuildConfigGroupGenCmd 生成 yasboot config group gen -t ce 命令（隐藏 -t ce）。
func BuildConfigGroupGenCmd(p StandbyCEGroupGenParams) string {
	diskFound := strings.TrimSpace(p.DiskFoundPath)
	if diskFound == "" {
		diskFound = dbsteps.DefaultYACDiskFoundPath
	}
	vipStr := dbsteps.FormatVIPListForCeGen(p.VIPs, p.PublicNetwork, p.InterCIDR)
	cmd := fmt.Sprintf(
		"cd %s && yasboot config group gen -c %s -f -u %s -p %s --ip %s --port %d -i %s --data-path %s --log-path %s --begin-port %d --node %d --system-data %s --data %s --disk-found-path %s --vips %s -t ce",
		p.StageDir,
		p.ClusterName,
		p.User,
		p.Password,
		p.IPs,
		p.SSHPort,
		p.InstallPath,
		p.DataPath,
		p.LogPath,
		p.BeginPort,
		p.NodeCount,
		p.SystemDisks,
		p.DataDisks,
		diskFound,
		vipStr,
	)
	extra := strings.TrimSpace(p.ExtraArgs)
	if extra != "" {
		cmd = dbsteps.AppendYasbootGenExtraArgs(cmd, extra)
	}
	return cmd
}

// RequireCEAdminPassword CE 备路径需要 sys 密码（逐实例 REPLICATION_ADDR + group add）。
func RequireCEAdminPassword(sysPass string) error {
	if strings.TrimSpace(sysPass) == "" {
		return fmt.Errorf("--db-admin-password is required for CE standby path (per-instance REPLICATION_ADDR and group add)")
	}
	return nil
}

// FormatCEGroupRoleSummary 从 yasboot cluster status -b group 输出生成 ceg 角色摘要行。
// 兼容 group_name 合并单元格（续行第一列为空时沿用上一组名）。
func FormatCEGroupRoleSummary(groupStatusOut string) []string {
	type agg struct {
		primary, standby, other int
	}
	groups := map[string]*agg{}
	var order []string
	lastName := ""
	for _, line := range strings.Split(groupStatusOut, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "group_name") && strings.Contains(low, "nodeid") {
			continue
		}
		name := ""
		for _, f := range strings.Split(line, "|") {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(strings.ToLower(f), "ceg") {
				name = f
				break
			}
		}
		if name == "" {
			name = lastName
		}
		if name == "" || strings.EqualFold(name, "name") || strings.EqualFold(name, "group_name") {
			continue
		}
		lastName = name
		a := groups[name]
		if a == nil {
			a = &agg{}
			groups[name] = a
			order = append(order, name)
		}
		switch {
		case strings.Contains(low, "primary"):
			a.primary++
		case strings.Contains(low, "standby"):
			a.standby++
		default:
			a.other++
		}
	}
	var lines []string
	for _, name := range order {
		a := groups[name]
		role := "mixed"
		switch {
		case a.standby > 0 && a.primary == 0:
			role = "standby"
		case a.primary > 0 && a.standby == 0:
			role = "primary"
		}
		lines = append(lines, fmt.Sprintf("%s=%s (primary_rows=%d standby_rows=%d)", name, role, a.primary, a.standby))
	}
	return lines
}

// NormalizeCEGroupID 将 ceg2 / 2 归一为 yasboot --group-ids 可用的数字 ID。
func NormalizeCEGroupID(g string) (string, error) {
	g = strings.TrimSpace(g)
	if g == "" {
		return "", fmt.Errorf("empty group id")
	}
	low := strings.ToLower(g)
	if strings.HasPrefix(low, "ceg") {
		n := strings.TrimPrefix(low, "ceg")
		if n == "" {
			return "", fmt.Errorf("invalid group id %q", g)
		}
		for _, ch := range n {
			if ch < '0' || ch > '9' {
				return "", fmt.Errorf("invalid group id %q", g)
			}
		}
		return n, nil
	}
	for _, ch := range g {
		if ch < '0' || ch > '9' {
			return "", fmt.Errorf("invalid group id %q (want numeric or cegN)", g)
		}
	}
	return g, nil
}

// BuildSafeCECleanupCommands 仅针对备 group 生成 group remove 命令；若命中主 group 则报错。
// yasboot --group-ids 要数字（ceg2 -> 2）。
// forFailedScale=true：扩备失败残留，用 --clean；false：成功拆除用 --purge --with-host。
func BuildSafeCECleanupCommands(clusterName string, standbyGroupIDs, primaryGroupIDs []string, forFailedScale bool) ([]string, error) {
	prim := map[string]struct{}{}
	for _, g := range primaryGroupIDs {
		id, err := NormalizeCEGroupID(g)
		if err != nil {
			continue
		}
		prim[id] = struct{}{}
	}
	var cmds []string
	for _, g := range standbyGroupIDs {
		id, err := NormalizeCEGroupID(g)
		if err != nil {
			return nil, err
		}
		if _, bad := prim[id]; bad {
			return nil, fmt.Errorf("refusing cleanup of primary group %q", g)
		}
		// 主组红线：ceg1 / 1
		if id == "1" {
			return nil, fmt.Errorf("refusing cleanup of primary group %q", g)
		}
		if forFailedScale {
			cmds = append(cmds, fmt.Sprintf("yasboot group remove -c %s --group-ids %s --clean --ce -f", clusterName, id))
		} else {
			cmds = append(cmds, fmt.Sprintf("yasboot group remove -c %s --group-ids %s --purge --ce --with-host -f", clusterName, id))
		}
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("no standby group id to clean; pass failed/new group id only (never ceg1/1 or pre-existing standby groups)")
	}
	return cmds, nil
}

// FormatCEExpansionFailureRemediation 扩备失败时的用户可见解决方案（英文 ASCII）。
func FormatCEExpansionFailureRemediation(clusterName string, autoCleanupEnabled bool) string {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		clusterName = "yashandb"
	}
	var b strings.Builder
	b.WriteString("CE standby expansion failed. Remediation:\n")
	b.WriteString("  1) Inspect: yasboot cluster status -c " + clusterName + " -b group -d\n")
	b.WriteString("  2) Remove FAILED standby group only (never group-id 1 / ceg1):\n")
	b.WriteString("       yasboot group remove -c " + clusterName + " --group-ids <N> --clean --ce -f\n")
	b.WriteString("     (successful teardown instead uses --purge --ce --with-host)\n")
	b.WriteString("  3) Re-run yinstall standby after cleanup.\n")
	if autoCleanupEnabled {
		b.WriteString("  Auto-cleanup was requested (--standby-cleanup-on-failure or -F); see cleanup log above/below.\n")
	} else {
		b.WriteString("  Tip: pass --standby-cleanup-on-failure (or -F) to let yinstall auto-run safe cleanup after failure.\n")
	}
	return b.String()
}

// ParseCEGroupNamesByRole 从 -b group 输出粗分 primary/standby 组名。
func ParseCEGroupNamesByRole(groupStatusOut string) (primaryGroups, standbyGroups []string) {
	for _, line := range FormatCEGroupRoleSummary(groupStatusOut) {
		// name=role (...)
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		name := line[:eq]
		rest := line[eq+1:]
		switch {
		case strings.HasPrefix(rest, "primary"):
			primaryGroups = append(primaryGroups, name)
		case strings.HasPrefix(rest, "standby"):
			standbyGroups = append(standbyGroups, name)
		}
	}
	return primaryGroups, standbyGroups
}

// ListAllCEGroupNames 从 -b group 输出列出全部 ceg 名（保序去重）。
func ListAllCEGroupNames(groupStatusOut string) []string {
	var all []string
	seen := map[string]struct{}{}
	for _, line := range FormatCEGroupRoleSummary(groupStatusOut) {
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		name := line[:eq]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		all = append(all, name)
	}
	return all
}

// MaxCEGroupIndex 从 cegN 列表取最大 N；无则 0。
func MaxCEGroupIndex(groupNames []string) int {
	max := 0
	for _, g := range groupNames {
		id, err := NormalizeCEGroupID(g)
		if err != nil {
			continue
		}
		n := 0
		for _, ch := range id {
			n = n*10 + int(ch-'0')
		}
		if n > max {
			max = n
		}
	}
	return max
}

// PredictNextCEGroupName 根据已有 ceg 预测下一组名（通常 ceg{max+1}）。
func PredictNextCEGroupName(existingGroups []string) string {
	max := MaxCEGroupIndex(existingGroups)
	if max < 1 {
		max = 1 // 至少已有主组 ceg1 的常见起点
	}
	return fmt.Sprintf("ceg%d", max+1)
}

// GroupNameFromAddTOML 从 *_add.toml 提取 [[group]] name。
func GroupNameFromAddTOML(toml string) string {
	re := regexp.MustCompile(`(?im)^\s*name\s*=\s*"(ceg\d+)"\s*$`)
	if m := re.FindStringSubmatch(toml); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// RecordCEGroupBaseline 记录扩前已有 group，并日志打印 next group 预期。
func RecordCEGroupBaseline(ctx *runner.StepContext, groupStatusOut string) {
	if ctx == nil {
		return
	}
	all := ListAllCEGroupNames(groupStatusOut)
	_, stbys := ParseCEGroupNamesByRole(groupStatusOut)
	next := PredictNextCEGroupName(all)
	ctx.Params["ce_baseline_groups"] = strings.Join(all, ",")
	ctx.Params["ce_baseline_standby_groups"] = strings.Join(stbys, ",")
	ctx.Params["ce_expected_new_group"] = next
	if ctx.Logger != nil {
		ctx.Logger.Info("CE groups before expansion: all=[%s] standby=[%s]", strings.Join(all, ","), strings.Join(stbys, ","))
		ctx.Logger.Info("CE expected new group for this expansion: %s", next)
	}
}

// SelectCEGroupsForFailedCleanup 只选本次新增备组；禁止清基线中已存在的备组。
// baselineStandby：扩前 standby 组；currentStandby：当前 standby 组；expectedNew：扩前预测或 add.toml 中的新组名。
func SelectCEGroupsForFailedCleanup(baselineStandby, currentStandby []string, expectedNew string) []string {
	base := map[string]struct{}{}
	for _, g := range baselineStandby {
		g = strings.TrimSpace(g)
		if g != "" {
			base[g] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(g string) {
		g = strings.TrimSpace(g)
		if g == "" {
			return
		}
		if _, protected := base[g]; protected {
			return // 禁止清已存在备组
		}
		if strings.EqualFold(g, "ceg1") {
			return
		}
		if id, err := NormalizeCEGroupID(g); err == nil && id == "1" {
			return
		}
		if _, ok := seen[g]; ok {
			return
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	for _, g := range currentStandby {
		if _, ok := base[g]; !ok {
			add(g)
		}
	}
	add(expectedNew)
	return out
}

// SplitCSVParam 拆分 params 里逗号分隔的组名列表。
func SplitCSVParam(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// PrimaryLooksLikeCE 根据 cluster status / toml 片段判断主库是否为 CE。
func PrimaryLooksLikeCE(statusOrToml string) bool {
	s := strings.ToLower(statusOrToml)
	if s == "" {
		return false
	}
	// toml / status 常见信号
	if strings.Contains(s, `group_type = "ce"`) || strings.Contains(s, "group_type = 'ce'") {
		return true
	}
	if strings.Contains(s, "group_type=ce") {
		return true
	}
	// yasboot cluster status -b group 常见 ceg1/ceg2
	if strings.Contains(s, "ceg1") || strings.Contains(s, "ceg2") || strings.Contains(s, "| ceg") {
		return true
	}
	if strings.Contains(s, "package ce") {
		return true
	}
	return false
}
