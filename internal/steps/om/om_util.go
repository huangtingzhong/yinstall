// om_util.go - OM 迁移纯函数: 端口/status 解析/hosts.toml 修补/参数与同步判定
package om

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// YasomListenPort 由 DB begin-port 推导 yasom 监听端口 (begin-13)。
func YasomListenPort(beginPort int) int {
	if beginPort <= 13 {
		return 0
	}
	return beginPort - 13
}

// YasomAgentListenPort 由 begin-port 推导 yasagent 端口 (begin-12)。
func YasomAgentListenPort(beginPort int) int {
	if beginPort <= 12 {
		return 0
	}
	return beginPort - 12
}

// YasomListenAddr 返回 ip:yasomPort。
func YasomListenAddr(ip string, beginPort int) (string, error) {
	ip = strings.TrimSpace(ip)
	p := YasomListenPort(beginPort)
	if ip == "" || p <= 0 {
		return "", fmt.Errorf("invalid om listen addr: ip=%q beginPort=%d", ip, beginPort)
	}
	return fmt.Sprintf("%s:%d", ip, p), nil
}

// ResolveOMMigrateCurrent 解析迁主源 OM：--om-current 优先，空则用全局 -M/--om。
func ResolveOMMigrateCurrent(current, omIP string) string {
	if c := strings.TrimSpace(current); c != "" {
		return c
	}
	return strings.TrimSpace(omIP)
}

// ValidateOMMigrateParams 校验迁主参数。
// 仅当出现 --om-new（或显式 --om-current 而无 new）时进入迁主校验；
// 仅全局 -M/--om（无 --om-new）不视为迁主，避免与日常 standby/clean 冲突。
// 返回 migrate=true 表示应执行迁主。
func ValidateOMMigrateParams(current, newIP, omIP string) (migrate bool, err error) {
	explicitCurrent := strings.TrimSpace(current)
	newIP = strings.TrimSpace(newIP)
	omIP = strings.TrimSpace(omIP)

	if explicitCurrent == "" && newIP == "" {
		return false, nil
	}
	if newIP == "" {
		return false, fmt.Errorf("migrate requires --om-new with --om-current or global -M/--om; or omit --om-current to skip migrate")
	}
	current = ResolveOMMigrateCurrent(explicitCurrent, omIP)
	if current == "" {
		return false, fmt.Errorf("migrate requires source OM (--om-current or global -M/--om) together with --om-new")
	}
	if current == newIP {
		return false, fmt.Errorf("source OM (%s) and --om-new must differ", current)
	}
	if omIP != "" && omIP != current {
		return false, fmt.Errorf("-M/--om %s must equal --om-current %s when both are set", omIP, current)
	}
	return true, nil
}

// YasomHostRow 解析自 process yasom status 表行。
type YasomHostRow struct {
	HostID         string
	PID            string
	IPAddr         string
	Primary        string
	Secondary      string
	LocalYasomAddr string
	Role           string
	BackupNum      int
	MaxSeq         int
	AutoRepair     string
}

// ParseYasomStatus 解析 yasboot process yasom status 表格输出。
func ParseYasomStatus(out string) []YasomHostRow {
	var rows []YasomHostRow
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "hostid") || strings.HasPrefix(line, "|-") || strings.HasPrefix(line, "+") {
			continue
		}
		fields := splitTableFields(line)
		if len(fields) < 8 {
			continue
		}
		if !strings.HasPrefix(fields[0], "host") {
			continue
		}
		bn, _ := strconv.Atoi(fields[7])
		ms := 0
		if len(fields) > 8 {
			ms, _ = strconv.Atoi(fields[8])
		}
		ar := ""
		if len(fields) > 9 {
			ar = fields[9]
		}
		rows = append(rows, YasomHostRow{
			HostID:         fields[0],
			PID:            fields[1],
			IPAddr:         fields[2],
			Primary:        fields[3],
			Secondary:      fields[4],
			LocalYasomAddr: fields[5],
			Role:           fields[6],
			BackupNum:      bn,
			MaxSeq:         ms,
			AutoRepair:     ar,
		})
	}
	return rows
}

func splitTableFields(line string) []string {
	parts := strings.Split(line, "|")
	var fields []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields = append(fields, p)
	}
	return fields
}

// FindRowByIP 按 ipaddr 查找行。
func FindRowByIP(rows []YasomHostRow, ip string) *YasomHostRow {
	ip = strings.TrimSpace(ip)
	for i := range rows {
		if rows[i].IPAddr == ip {
			return &rows[i]
		}
	}
	return nil
}

// FindPrimaryRow 返回 role=primary 的行。
func FindPrimaryRow(rows []YasomHostRow) *YasomHostRow {
	for i := range rows {
		if strings.EqualFold(rows[i].Role, "primary") {
			return &rows[i]
		}
	}
	return nil
}

// HostInCluster 判断 IP 是否已在 status 主机列表中 (M1)。
func HostInCluster(rows []YasomHostRow, ip string) bool {
	return FindRowByIP(rows, ip) != nil
}

// SecondaryListContains 判断 secondary 列是否包含 listen 地址。
func SecondaryListContains(secondaryCol, listenAddr string) bool {
	secondaryCol = strings.TrimSpace(secondaryCol)
	listenAddr = strings.TrimSpace(listenAddr)
	if secondaryCol == "" || listenAddr == "" || secondaryCol == "[]" || secondaryCol == "-" {
		return false
	}
	return strings.Contains(secondaryCol, listenAddr)
}

// IsPIDRunning 判定 status 中的 pid 是否表示进程在跑。
func IsPIDRunning(pid string) bool {
	pid = strings.TrimSpace(pid)
	if pid == "" || pid == "-" || strings.EqualFold(pid, "off") {
		return false
	}
	_, err := strconv.Atoi(pid)
	return err == nil
}

// SecondarySynced 升主前门禁: NEW 为已同步 secondary。
func SecondarySynced(rows []YasomHostRow, newIP, newListen string) error {
	pri := FindPrimaryRow(rows)
	if pri == nil {
		return fmt.Errorf("no primary yasom in status")
	}
	nw := FindRowByIP(rows, newIP)
	if nw == nil {
		return fmt.Errorf("target OM host %s not in yasom status", newIP)
	}
	if !strings.EqualFold(nw.Role, "secondary") {
		return fmt.Errorf("target OM %s role=%s, want secondary", newIP, nw.Role)
	}
	if !IsPIDRunning(nw.PID) {
		return fmt.Errorf("target OM %s secondary pid not running (%s)", newIP, nw.PID)
	}
	if newListen != "" && nw.LocalYasomAddr != "-" && nw.LocalYasomAddr != "" && nw.LocalYasomAddr != newListen {
		return fmt.Errorf("target OM local_yasom_addr=%s, want %s", nw.LocalYasomAddr, newListen)
	}
	if !SecondaryListContains(pri.Secondary, newListen) && !SecondaryListContains(nw.Secondary, newListen) {
		return fmt.Errorf("secondary list does not contain %s", newListen)
	}
	if nw.MaxSeq != pri.MaxSeq {
		return fmt.Errorf("target max_seq=%d not aligned with primary max_seq=%d", nw.MaxSeq, pri.MaxSeq)
	}
	return nil
}

// DefaultSyncWait 升主前等待 secondary 对齐的默认超时/间隔。
const (
	DefaultSyncWaitTimeout  = 60 * time.Second
	DefaultSyncWaitInterval = 2 * time.Second
)

// PatchHostsTomlOM 仅替换 [om] 段内 hostid 与 LISTEN_ADDR。
func PatchHostsTomlOM(tomlText, hostID, listenAddr string) (string, error) {
	hostID = strings.TrimSpace(hostID)
	listenAddr = strings.TrimSpace(listenAddr)
	if hostID == "" || listenAddr == "" {
		return "", fmt.Errorf("hostID and listenAddr are required")
	}
	lines := strings.Split(tomlText, "\n")
	inOM := false
	hostDone := false
	listenDone := false
	omHostRe := regexp.MustCompile(`^(\s*hostid\s*=\s*)"[^"]*"(.*)$`)
	listenRe := regexp.MustCompile(`^(\s*LISTEN_ADDR\s*=\s*)"[^"]*"(.*)$`)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "[om]" {
			inOM = true
			continue
		}
		if inOM && strings.HasPrefix(trim, "[") {
			// [om.config] 等子表仍属 OM 段; 其它 [section] 结束
			if strings.HasPrefix(trim, "[om.") {
				continue
			}
			inOM = false
			continue
		}
		if !inOM {
			continue
		}
		if !hostDone && omHostRe.MatchString(line) {
			lines[i] = omHostRe.ReplaceAllString(line, fmt.Sprintf(`${1}"%s"${2}`, hostID))
			hostDone = true
			continue
		}
		if !listenDone && listenRe.MatchString(line) {
			lines[i] = listenRe.ReplaceAllString(line, fmt.Sprintf(`${1}"%s"${2}`, listenAddr))
			listenDone = true
		}
	}
	if !hostDone || !listenDone {
		return "", fmt.Errorf("failed to patch [om] hostid/LISTEN_ADDR (hostDone=%v listenDone=%v)", hostDone, listenDone)
	}
	return strings.Join(lines, "\n"), nil
}

// ListClusterHostIPs 返回 status 中全部主机 IP（去重保序）。
func ListClusterHostIPs(rows []YasomHostRow) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range rows {
		ip := strings.TrimSpace(r.IPAddr)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}

// ListSecondaryCandidateIPs 返回可部署备 OM 的主机 IP（排除 primary）。
func ListSecondaryCandidateIPs(rows []YasomHostRow) []string {
	pri := FindPrimaryRow(rows)
	priIP := ""
	if pri != nil {
		priIP = pri.IPAddr
	}
	var out []string
	for _, ip := range ListClusterHostIPs(rows) {
		if priIP != "" && ip == priIP {
			continue
		}
		out = append(out, ip)
	}
	return out
}

// MigrateModeFromStatus 根据 NEW 是否已在集群返回 m1/m2。
func MigrateModeFromStatus(rows []YasomHostRow, newIP string) string {
	if HostInCluster(rows, newIP) {
		return "m1"
	}
	return "m2"
}

// IsOMStageListingReady stage 目录 listing 是否具备扩备所需文件 (hosts.toml + 安装包)。
func IsOMStageListingReady(names []string) bool {
	return IsOMStageTomlReady(names) && IsOMStagePackageReady(names)
}

// IsOMStageTomlReady 是否已有 hosts.toml。
func IsOMStageTomlReady(names []string) bool {
	for _, n := range names {
		if stageListingBase(n) == "hosts.toml" {
			return true
		}
	}
	return false
}

// IsOMStagePackageReady 是否已有安装包 (tar.gz / database-*)。
func IsOMStagePackageReady(names []string) bool {
	for _, n := range names {
		base := stageListingBase(n)
		low := strings.ToLower(base)
		if strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz") ||
			strings.HasPrefix(low, "database-") || strings.Contains(low, "yashandb") {
			return true
		}
	}
	return false
}

func stageListingBase(n string) string {
	n = strings.TrimSpace(n)
	if i := strings.LastIndex(n, "/"); i >= 0 {
		return n[i+1:]
	}
	return n
}

// OMStageSyncMode 阶段目录同步范围。
type OMStageSyncMode string

const (
	// OMStageSyncFull 备 OM 部署: 同步 hosts.toml + 安装包等完整 stage。
	OMStageSyncFull OMStageSyncMode = "full"
	// OMStageSyncTOML 迁主: 仅同步 toml (hosts.toml 等), 安装包应已在备 OM 阶段到位。
	OMStageSyncTOML OMStageSyncMode = "toml"
)

// FilterOMStageNames 按同步模式过滤要拷贝的文件名。
func FilterOMStageNames(names []string, mode OMStageSyncMode) []string {
	var out []string
	for _, n := range names {
		base := stageListingBase(n)
		if base == "" || base == "." || base == ".." {
			continue
		}
		low := strings.ToLower(base)
		switch mode {
		case OMStageSyncTOML:
			if base == "hosts.toml" || strings.HasSuffix(low, ".toml") {
				out = append(out, base)
			}
		default: // full
			if base == "hosts.toml" ||
				strings.HasSuffix(low, ".toml") ||
				strings.HasSuffix(low, ".tar.gz") ||
				strings.HasSuffix(low, ".tgz") ||
				strings.HasPrefix(low, "database-") {
				out = append(out, base)
			}
		}
	}
	return out
}

// ParseLSNames 解析 ls -1 输出为文件名列表。
func ParseLSNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	return names
}

// OMMigrateGateKind 迁主门禁分类 (幂等/防双主)。
type OMMigrateGateKind int

const (
	// OMMigrateProceed 需继续迁主流程。
	OMMigrateProceed OMMigrateGateKind = iota
	// OMMigrateAlreadyDone --om-new 已是唯一 running primary, 整段可视为完成。
	OMMigrateAlreadyDone
	// OMMigrateDualPrimary CUR 与 NEW 同时为 running primary, 拒绝。
	OMMigrateDualPrimary
	// OMMigrateCurNotPrimary 源 OM 不是当前 primary (且 NEW 也尚未升主)。
	OMMigrateCurNotPrimary
	// OMMigrateNoPrimary status 中无 primary 角色。
	OMMigrateNoPrimary
)

// ClassifyOMMigrateStatus 根据 yasom status 判定迁主门禁状态。
// already-done: NEW 为 running primary, 且 CUR 不是 running primary (迁主已完成拓扑)。
func ClassifyOMMigrateStatus(rows []YasomHostRow, curIP, newIP string) OMMigrateGateKind {
	curIP = strings.TrimSpace(curIP)
	newIP = strings.TrimSpace(newIP)
	cur := FindRowByIP(rows, curIP)
	nw := FindRowByIP(rows, newIP)

	curRunningPri := cur != nil && strings.EqualFold(cur.Role, "primary") && IsPIDRunning(cur.PID)
	nwRunningPri := nw != nil && strings.EqualFold(nw.Role, "primary") && IsPIDRunning(nw.PID)

	if curRunningPri && nwRunningPri && curIP != newIP {
		return OMMigrateDualPrimary
	}
	if nwRunningPri {
		return OMMigrateAlreadyDone
	}

	var runningPri *YasomHostRow
	for i := range rows {
		if strings.EqualFold(rows[i].Role, "primary") && IsPIDRunning(rows[i].PID) {
			runningPri = &rows[i]
			break
		}
	}
	if runningPri != nil {
		if runningPri.IPAddr != curIP {
			return OMMigrateCurNotPrimary
		}
		return OMMigrateProceed
	}

	pri := FindPrimaryRow(rows)
	if pri == nil {
		return OMMigrateNoPrimary
	}
	// stop 之后: CUR 仍可能 role=primary 但 pid 已停, 允许续跑升主
	if pri.IPAddr == curIP {
		return OMMigrateProceed
	}
	return OMMigrateCurNotPrimary
}

// omMigrateAlreadyDone 是否已在 Gate 判定迁主拓扑完成。
func omMigrateAlreadyDone(ctx *runner.StepContext) bool {
	if ctx == nil || ctx.Results == nil {
		return false
	}
	v, _ := ctx.Results["om_migrate_already_done"].(bool)
	return v
}

// skipIfOMMigrateAlreadyDone 成功后再跑时跳过破坏性/状态机步骤。
func skipIfOMMigrateAlreadyDone(ctx *runner.StepContext) error {
	if omMigrateAlreadyDone(ctx) {
		return runner.NewStepSkippedError("OM migrate already complete on --om-new")
	}
	return nil
}

func omProductUser(ctx *runner.StepContext) string {
	u := strings.TrimSpace(ctx.GetParamString("primary_os_user", ""))
	if u == "" {
		u = strings.TrimSpace(ctx.GetParamString("os_user", ""))
	}
	if u == "" {
		return "yashan"
	}
	return u
}

func omClusterName(ctx *runner.StepContext) string {
	return commonos.ResolveDBClusterName(ctx.GetParamString("db_cluster_name", ""), omBeginPort(ctx))
}

func omBeginPort(ctx *runner.StepContext) int {
	p := ctx.GetParamInt("db_begin_port", 0)
	if p <= 0 {
		p = ctx.GetParamInt("db_port", 1688)
	}
	if p <= 0 {
		return 1688
	}
	return p
}

func omEnvFile(ctx *runner.StepContext) string {
	if f := strings.TrimSpace(ctx.GetParamString("primary_env_file", "")); f != "" {
		return f
	}
	user := omProductUser(ctx)
	cluster := omClusterName(ctx)
	return fmt.Sprintf("/home/%s/.yasboot/%s.env", user, cluster)
}

func omStageDir(ctx *runner.StepContext) string {
	// 与 yinstall db/standby 共用 ResolveConventionStageDir（1688→install，否则 install_<port>）
	return commonos.ResolveConventionStageDir(ctx.GetParamString("db_stage_dir", ""), omProductUser(ctx), omBeginPort(ctx))
}

func omMigrateMode(ctx *runner.StepContext) string {
	if v, ok := ctx.Results["om_migrate_mode"].(string); ok && v != "" {
		return v
	}
	return strings.TrimSpace(ctx.GetParamString("om_migrate_mode", ""))
}
