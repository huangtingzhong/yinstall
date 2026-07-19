// standby_expansion_summary.go - 备库扩容完成摘要与 mounted 轮询
// 在 E-019 末尾输出与 db Install Summary 对齐的 Standby Expansion Summary

package standby

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
	omsteps "github.com/yinstall/internal/steps/om"
)

const (
	defaultStandbyOpenPollAttempts = 5
	defaultStandbyOpenPollInterval = 5 * time.Second
)

// PendingStandbyOpenNodes 返回尚未 open 的备库节点（mounted/started/角色未定等）。
// standbyIPs 非空时：listen_address 命中这些 IP 的行也参与判定（覆盖 role 尚为空的扩容中节点）。
func PendingStandbyOpenNodes(rows []dbsteps.ClusterStatusRow, standbyIPs []string) []string {
	ipSet := map[string]struct{}{}
	for _, ip := range standbyIPs {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			ipSet[ip] = struct{}{}
		}
	}
	var out []string
	for _, r := range rows {
		if !rowLooksLikeStandbyCandidate(r, ipSet) {
			continue
		}
		if standbyInstanceFullyOpen(r) {
			continue
		}
		id := strings.TrimSpace(r.Nodeid)
		if id == "" {
			id = strings.TrimSpace(r.ListenAddress)
		}
		out = append(out, id)
	}
	return out
}

func listenHost(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" || listen == "-" {
		return ""
	}
	if i := strings.LastIndex(listen, ":"); i > 0 {
		return listen[:i]
	}
	return listen
}

func rowLooksLikeStandbyCandidate(r dbsteps.ClusterStatusRow, standbyIPs map[string]struct{}) bool {
	if isStandbyRole(r.DatabaseRole) {
		return true
	}
	if isPrimaryRole(r.DatabaseRole) {
		return false
	}
	host := listenHost(r.ListenAddress)
	if host == "" {
		return false
	}
	_, ok := standbyIPs[host]
	return ok
}

func isPrimaryRole(role string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(role)), "primary")
}

func standbyInstanceFullyOpen(r dbsteps.ClusterStatusRow) bool {
	inst := strings.ToLower(strings.TrimSpace(r.InstanceStatus))
	dbStat := strings.ToLower(strings.TrimSpace(r.DatabaseStatus))
	dbOK := dbStat == "normal" || dbStat == "open"
	return inst == "open" && dbOK && isStandbyRole(r.DatabaseRole)
}

// StandbyRowHealthLabel 返回 OK / WARN / FAIL。
// open+standby=OK；mounted/started 等过渡态=WARN；其它=FAIL。
func StandbyRowHealthLabel(r dbsteps.ClusterStatusRow) string {
	inst := strings.ToLower(strings.TrimSpace(r.InstanceStatus))
	dbStat := strings.ToLower(strings.TrimSpace(r.DatabaseStatus))
	dbOK := dbStat == "normal" || dbStat == "open"
	if isPrimaryRole(r.DatabaseRole) || (!isStandbyRole(r.DatabaseRole) && (inst == "open" && dbOK)) {
		if inst == "open" && dbOK {
			return "OK"
		}
		return "FAIL"
	}
	// standby 或扩容中（角色未明）节点
	if standbyInstanceFullyOpen(r) {
		return "OK"
	}
	switch inst {
	case "mounted", "started":
		return "WARN"
	case "open":
		if dbOK && isStandbyRole(r.DatabaseRole) {
			return "OK"
		}
		return "WARN"
	case "", "-":
		return "WARN"
	default:
		if inst != "open" {
			return "WARN"
		}
		return "FAIL"
	}
}

func isStandbyRole(role string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(role)), "standby")
}

// PollStandbyOpenUntilReadyForTargets 对尚未 open 的备库节点轮询 status；standbyIPs 用于识别扩容中节点。
// sleepFn 为 nil 时用默认 interval。返回最终 status、仍未就绪 nodeid、是否超时。
func PollStandbyOpenUntilReadyForTargets(
	initialOut string,
	standbyIPs []string,
	fetch func() (string, error),
	attempts int,
	sleepFn func(time.Duration),
	interval time.Duration,
	onWait func(attempt, maxAttempts int, pendingNodes []string),
) (finalOut string, stillPending []string, timedOut bool, err error) {
	if attempts <= 0 {
		attempts = defaultStandbyOpenPollAttempts
	}
	if interval <= 0 {
		interval = defaultStandbyOpenPollInterval
	}
	if sleepFn == nil {
		sleepFn = time.Sleep
	}
	finalOut = initialOut
	stillPending = PendingStandbyOpenNodes(dbsteps.ParseClusterStatusTable(finalOut), standbyIPs)
	if len(stillPending) == 0 {
		return finalOut, nil, false, nil
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if onWait != nil {
			onWait(attempt, attempts, stillPending)
		}
		sleepFn(interval)
		if fetch == nil {
			return finalOut, stillPending, true, fmt.Errorf("status fetch function is nil")
		}
		out, fetchErr := fetch()
		if fetchErr != nil {
			return finalOut, stillPending, true, fetchErr
		}
		finalOut = out
		stillPending = PendingStandbyOpenNodes(dbsteps.ParseClusterStatusTable(finalOut), standbyIPs)
		if len(stillPending) == 0 {
			return finalOut, nil, false, nil
		}
	}
	return finalOut, stillPending, true, nil
}

func standbyOpenPollConfig(ctx *runner.StepContext) (attempts int, interval time.Duration) {
	attempts = defaultStandbyOpenPollAttempts
	interval = defaultStandbyOpenPollInterval
	if ctx == nil {
		return attempts, interval
	}
	if v := ctx.GetParamInt("standby_open_poll_attempts", 0); v > 0 {
		attempts = v
	}
	if v := ctx.GetParamInt("standby_open_poll_interval_ms", 0); v > 0 {
		interval = time.Duration(v) * time.Millisecond
	} else if v := ctx.GetParamInt("standby_open_poll_interval_sec", 0); v > 0 {
		interval = time.Duration(v) * time.Second
	}
	return attempts, interval
}

func formatStandbyYasqlExample(ctx *runner.StepContext, connectHost, dbName string, port int) string {
	pwd := strings.TrimSpace(ctx.GetParamString("db_admin_password", ""))
	return "yasql " + commonsql.BuildYasqlTCPConnect(connectHost, "sys", pwd, port, dbName)
}

// ListenPortFromAddress 从 listen_address（如 10.10.10.130:6688）解析端口；失败返回 0。
func ListenPortFromAddress(listen string) int {
	p, err := PortFromListenAddr(listen)
	if err != nil {
		return 0
	}
	return p
}

// PrimaryListenPortFromStatus 从 cluster status 行解析主库 listen 端口。
// 优先 role=primary；其次 primaryIP 匹配；都失败则用 fallback（通常为 --db-port）。
func PrimaryListenPortFromStatus(rows []dbsteps.ClusterStatusRow, primaryIP string, fallback int) int {
	primaryIP = strings.TrimSpace(primaryIP)
	var byIP int
	for _, r := range rows {
		port := ListenPortFromAddress(r.ListenAddress)
		if port <= 0 {
			continue
		}
		if isPrimaryRole(r.DatabaseRole) {
			return port
		}
		if primaryIP != "" && byIP == 0 {
			if ip := ListenIPFromAddress(r.ListenAddress); ip != "" && SameHostIP(ip, primaryIP) {
				byIP = port
			}
		}
	}
	if byIP > 0 {
		return byIP
	}
	if fallback > 0 {
		return fallback
	}
	return 1688
}

func resolveStandbyEnvFileForSummary(ctx *runner.StepContext) string {
	if v, ok := ctx.Results["env_file"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return ""
	}
	return envFile
}

// printStandbyExpansionSummary 向终端输出备库扩容摘要（dry-run/precheck 跳过）。
func printStandbyExpansionSummary(ctx *runner.StepContext, stepID, clusterStatusOut string, stillMounted []string) {
	if ctx == nil || ctx.Logger == nil || ctx.DryRun || ctx.Precheck {
		return
	}

	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	standbyBeginPort := ctx.GetParamInt("db_begin_port", 1688)
	osUser := GetPrimaryOSUser(ctx)
	primaryIP := strings.TrimSpace(ctx.GetParamString("primary_ip", ""))
	if primaryIP == "" && ctx.Executor != nil {
		primaryIP = strings.TrimSpace(ctx.Executor.Host())
	}
	if primaryIP == "" {
		primaryIP = "localhost"
	}
	standbys := ctx.GetParamStringSlice("standby_targets")
	if len(standbys) == 0 {
		if s := strings.TrimSpace(ctx.GetParamString("standby_targets_str", "")); s != "" {
			standbys = strings.Split(s, ",")
		}
	}
	for i := range standbys {
		standbys[i] = strings.TrimSpace(standbys[i])
	}
	envFile := resolveStandbyEnvFileForSummary(ctx)
	statusRows := dbsteps.ParseClusterStatusTable(clusterStatusOut)
	primaryPort := PrimaryListenPortFromStatus(statusRows, primaryIP, standbyBeginPort)

	notice := func(msg string) {
		ctx.Logger.ConsoleNotice(stepID, msg)
	}

	notice(fmt.Sprintf("========== YashanDB Standby Expansion Summary (primary %s) ==========", primaryIP))
	notice("[Expansion]")
	notice(fmt.Sprintf("  mode=standby-add  primary=%s  standbys=%s", primaryIP, strings.Join(standbys, ",")))
	notice(fmt.Sprintf("  cluster=%s  standby_begin_port=%d  os_user=%s", clusterName, standbyBeginPort, osUser))
	if pkg := strings.TrimSpace(ctx.GetParamString("db_package", "")); pkg != "" {
		notice(fmt.Sprintf("  package=%s", filepath.Base(pkg)))
	}

	notice("[Primary]")
	notice(fmt.Sprintf("  host=%s  port=%d  dbname=%s  login=sys", primaryIP, primaryPort, clusterName))
	notice(fmt.Sprintf("  password=%s", dbsteps.DisplaySysPassword(ctx)))
	notice(fmt.Sprintf("  yasql_example=%s", formatStandbyYasqlExample(ctx, primaryIP, clusterName, primaryPort)))
	if envFile != "" {
		notice(fmt.Sprintf("  sysdba_local=source %s && yasql / as sysdba", envFile))
	}

	notice("[Standby]")
	printed := false
	standbyIPSet := map[string]struct{}{}
	for _, ip := range standbys {
		if ip != "" {
			standbyIPSet[ip] = struct{}{}
		}
	}
	for _, r := range statusRows {
		if !rowLooksLikeStandbyCandidate(r, standbyIPSet) && !isStandbyRole(r.DatabaseRole) {
			continue
		}
		printed = true
		dataPart := ""
		if r.DataPath != "" && r.DataPath != "-" {
			dataPart = "  data=" + r.DataPath
		}
		role := r.DatabaseRole
		if strings.TrimSpace(role) == "" || role == "-" {
			role = "(pending)"
		}
		notice(fmt.Sprintf("  %s  node=%s  role=%s%s",
			r.ListenAddress, r.Nodeid, role, dataPart))
	}
	if !printed {
		for _, ip := range standbys {
			notice(fmt.Sprintf("  %s:%d", ip, standbyBeginPort))
		}
	}

	printStandbyOMSummary(ctx, notice, clusterName)

	notice("[Paths]")
	notice(fmt.Sprintf("  db_home=%s", ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")))
	notice(fmt.Sprintf("  db_data=%s", ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")))
	notice(fmt.Sprintf("  db_log=%s", ctx.GetParamString("db_log_path", "/data/yashan/log")))
	notice(fmt.Sprintf("  stage_dir=%s", ctx.GetParamString("db_stage_dir", "/home/yashan/install")))
	if envFile != "" {
		notice(fmt.Sprintf("  env_file=%s", envFile))
	}

	notice("[Cluster Health]")
	hasWarn := false
	if len(statusRows) == 0 {
		notice("  (cluster status table not parsed; see debug log for raw output)")
	} else {
		for _, r := range statusRows {
			label := StandbyRowHealthLabel(r)
			if label == "WARN" {
				hasWarn = true
			}
			pidPart := ""
			if pid := strings.TrimSpace(r.Pid); pid != "" && pid != "0" {
				pidPart = fmt.Sprintf(" pid=%s", pid)
			}
			notice(fmt.Sprintf("  %s node=%s%s listen=%s instance=%s database=%s role=%s %s",
				r.Hostid, r.Nodeid, pidPart, r.ListenAddress,
				r.InstanceStatus, r.DatabaseStatus, r.DatabaseRole, label))
		}
	}

	yasomOK, yasomPIDs := probeStandbyClusterProcess(ctx, clusterName, standbyBeginPort, "yasom")
	yasagentOK, yasagentPIDs := probeStandbyClusterProcess(ctx, clusterName, standbyBeginPort, "yasagent")
	yasdbOK, yasdbPIDs := probeStandbyClusterProcess(ctx, clusterName, standbyBeginPort, "yasdb")
	if len(yasdbPIDs) == 0 {
		yasdbPIDs = yasdbPIDsFromStandbyStatus(statusRows, 0)
		yasdbOK = len(yasdbPIDs) > 0
	}
	notice(fmt.Sprintf("  Processes: yasom=%s yasagent=%s yasdb=%s (cluster=%s)",
		dbsteps.SummaryOKLabel(yasomOK), dbsteps.SummaryOKLabel(yasagentOK), dbsteps.SummaryOKLabel(yasdbOK), clusterName))
	if yasomOK || yasagentOK || yasdbOK {
		notice(fmt.Sprintf("  Process PIDs: yasom=%s yasagent=%s yasdb=%s",
			formatStandbyPIDList(yasomPIDs), formatStandbyPIDList(yasagentPIDs), formatStandbyPIDList(yasdbPIDs)))
	}
	printStandbyPortListenSummary(ctx, notice, statusRows, primaryIP, standbyBeginPort)

	yasdbCount := len(yasdbPIDs)
	if yasdbCount == 0 {
		yasdbCount = 1
	}
	serviceName, _ := commonos.DetermineServiceName(yasdbCount, standbyBeginPort)
	notice("[Service]")
	notice(fmt.Sprintf("  cluster_cmd=yasboot cluster start|stop -c %s", clusterName))
	notice(fmt.Sprintf("  status_cmd=yasboot cluster status -c %s -d", clusterName))
	notice(fmt.Sprintf("  monit_script=%s", commonos.ScriptPath))
	active, enabled := probeStandbySystemd(ctx, serviceName)
	if active != "unknown" || enabled != "unknown" {
		notice(fmt.Sprintf("  systemd=%s  active=%s  enabled=%s", serviceName, active, enabled))
	} else {
		notice(fmt.Sprintf("  systemd=%s  (not configured or autostart skipped)", serviceName))
	}

	if hasWarn || len(stillMounted) > 0 {
		notice("[Next Steps]")
		notice("  One or more standby instances are not fully open yet (mounted/started/pending role).")
		notice("  This can be normal shortly after expansion; please watch until open:")
		if envFile != "" {
			notice(fmt.Sprintf("    source %s && yasboot cluster status -c %s -d", envFile, clusterName))
		} else {
			notice(fmt.Sprintf("    yasboot cluster status -c %s -d", clusterName))
		}
		if len(stillMounted) > 0 {
			notice(fmt.Sprintf("  still_pending_nodes=%s", strings.Join(stillMounted, ",")))
		}
		notice("  Expected healthy state: instance_status=open, database_status=normal, role=standby")
	}
	notice("====================================================")
}

// printStandbyOMSummary 输出主/备 yasom 摘要（复用 om.YasomStatus）。
func printStandbyOMSummary(ctx *runner.StepContext, notice func(string), clusterName string) {
	if ctx == nil || notice == nil {
		return
	}
	notice("[OM]")
	omSecondary := ctx.GetParamBool("om_deploy_secondary", true)
	notice(fmt.Sprintf("  om_secondary=%v", omSecondary))
	if omIP := strings.TrimSpace(ctx.GetParamString("om_ip", "")); omIP != "" {
		notice(fmt.Sprintf("  om_ip=%s", omIP))
	}
	rows, _, err := omsteps.YasomStatus(ctx)
	if err != nil {
		notice(fmt.Sprintf("  yasom_status=unavailable (%v)", err))
		notice(fmt.Sprintf("  status_cmd=yasboot process yasom status -c %s", clusterName))
		return
	}
	for _, line := range FormatYasomSummaryLines(rows) {
		notice("  " + line)
	}
	notice(fmt.Sprintf("  status_cmd=yasboot process yasom status -c %s", clusterName))
}

// FormatYasomSummaryLines 将 yasom status 行格式化为 Summary 英文行（不含缩进）。
func FormatYasomSummaryLines(rows []omsteps.YasomHostRow) []string {
	if len(rows) == 0 {
		return []string{"yasom_status=(empty)"}
	}
	var lines []string
	var primaries, secondaries, others []omsteps.YasomHostRow
	for _, r := range rows {
		role := strings.ToLower(strings.TrimSpace(r.Role))
		switch role {
		case "primary":
			primaries = append(primaries, r)
		case "secondary":
			secondaries = append(secondaries, r)
		default:
			others = append(others, r)
		}
	}
	for _, r := range primaries {
		lines = append(lines, formatYasomRoleLine("primary", r))
	}
	for _, r := range secondaries {
		lines = append(lines, formatYasomRoleLine("secondary", r))
	}
	for _, r := range others {
		listen := strings.TrimSpace(r.LocalYasomAddr)
		if listen == "" {
			listen = "-"
		}
		role := strings.TrimSpace(r.Role)
		if role == "" {
			role = "-"
		}
		pid := strings.TrimSpace(r.PID)
		if pid == "" {
			pid = "-"
		}
		label := "-"
		if omsteps.IsPIDRunning(r.PID) {
			label = "OK"
		}
		lines = append(lines, fmt.Sprintf("host=%s  hostid=%s  role=%s  local=%s  pid=%s  %s",
			r.IPAddr, r.HostID, role, listen, pid, label))
	}
	return lines
}

func formatYasomRoleLine(kind string, r omsteps.YasomHostRow) string {
	listen := strings.TrimSpace(r.LocalYasomAddr)
	if listen == "" || listen == "-" {
		if kind == "primary" {
			listen = strings.TrimSpace(r.Primary)
		}
	}
	if listen == "" {
		listen = "-"
	}
	pid := strings.TrimSpace(r.PID)
	if pid == "" {
		pid = "-"
	}
	label := "WARN"
	if omsteps.IsPIDRunning(r.PID) {
		label = "OK"
	}
	return fmt.Sprintf("%s=%s  host=%s  hostid=%s  pid=%s  %s",
		kind, listen, r.IPAddr, r.HostID, pid, label)
}

// printStandbyCEGroupSummary 在 CE 路径输出 ceg 角色分组摘要。
func printStandbyCEGroupSummary(ctx *runner.StepContext, stepID, groupStatusOut string) {
	if ctx == nil || ctx.Logger == nil || ctx.DryRun || ctx.Precheck {
		return
	}
	if !ctx.GetParamBool("standby_ce_path", false) && !ctx.GetParamBool("primary_is_ce", false) {
		return
	}
	lines := FormatCEGroupRoleSummary(groupStatusOut)
	if len(lines) == 0 {
		return
	}
	notice := func(msg string) {
		ctx.Logger.ConsoleNotice(stepID, msg)
	}
	notice("[CE Groups]")
	for _, line := range lines {
		notice("  " + line)
	}
}

func yasdbPIDsFromStandbyStatus(rows []dbsteps.ClusterStatusRow, beginPort int) []string {
	portNeedle := ""
	if beginPort > 0 {
		portNeedle = fmt.Sprintf(":%d", beginPort)
	}
	var pids []string
	for _, r := range rows {
		pid := strings.TrimSpace(r.Pid)
		if pid == "" || pid == "0" {
			continue
		}
		if portNeedle != "" {
			listen := strings.TrimSpace(r.ListenAddress)
			if listen != "" && !strings.Contains(listen, portNeedle) {
				continue
			}
		}
		pids = append(pids, pid)
	}
	return pids
}

func formatStandbyPIDList(pids []string) string {
	if len(pids) == 0 {
		return "-"
	}
	return strings.Join(pids, ",")
}

// printStandbyPortListenSummary 按 cluster status 的各 listen 报告端口；仅对本机 IP 做 ss 探测，远端标 remote。
func printStandbyPortListenSummary(ctx *runner.StepContext, notice func(string), rows []dbsteps.ClusterStatusRow, primaryIP string, standbyBeginPort int) {
	localHost := ""
	if ctx != nil && ctx.Executor != nil {
		localHost = strings.TrimSpace(ctx.Executor.Host())
	}
	if localHost == "" {
		localHost = strings.TrimSpace(primaryIP)
	}

	type portLine struct {
		port   int
		role   string
		listen string
		local  bool
	}
	seen := map[int]struct{}{}
	var lines []portLine
	for _, r := range rows {
		listen := strings.TrimSpace(r.ListenAddress)
		port := ListenPortFromAddress(listen)
		if port <= 0 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		ip := ListenIPFromAddress(listen)
		role := strings.TrimSpace(r.DatabaseRole)
		if role == "" || role == "-" {
			role = "db"
		}
		lines = append(lines, portLine{
			port:   port,
			role:   role,
			listen: listen,
			local:  localHost != "" && ip != "" && SameHostIP(ip, localHost),
		})
	}
	if len(lines) == 0 && standbyBeginPort > 0 {
		lines = append(lines, portLine{port: standbyBeginPort, role: "standby", listen: fmt.Sprintf(":%d", standbyBeginPort), local: true})
	}
	for _, ln := range lines {
		if ln.local {
			ok := probeStandbyPortListening(ctx, ln.port)
			notice(fmt.Sprintf("  Port %d (%s %s): %s", ln.port, ln.role, ln.listen, dbsteps.SummaryOKLabel(ok)))
			continue
		}
		notice(fmt.Sprintf("  Port %d (%s %s): remote", ln.port, ln.role, ln.listen))
	}
}

func probeStandbyClusterProcess(ctx *runner.StepContext, clusterName string, beginPort int, procName string) (bool, []string) {
	_ = clusterName
	_ = beginPort
	procName = strings.TrimSpace(procName)
	if procName == "" || ctx == nil {
		return false, nil
	}
	result, _ := ctx.Execute(fmt.Sprintf("pgrep -x %s 2>/dev/null || true", procName), false)
	if result == nil {
		return false, nil
	}
	var pids []string
	for _, line := range strings.Split(result.GetStdout(), "\n") {
		pid := strings.TrimSpace(line)
		if pid == "" || pid == "0" {
			continue
		}
		pids = append(pids, pid)
	}
	return len(pids) > 0, pids
}

func probeStandbyPortListening(ctx *runner.StepContext, port int) bool {
	if ctx == nil || port <= 0 {
		return false
	}
	result, _ := ctx.Execute(fmt.Sprintf("ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)'", port, port), false)
	return result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != ""
}

func probeStandbySystemd(ctx *runner.StepContext, serviceName string) (active, enabled string) {
	active, enabled = "unknown", "unknown"
	if ctx == nil || strings.TrimSpace(serviceName) == "" {
		return active, enabled
	}
	if r, _ := ctx.Execute(fmt.Sprintf("systemctl is-active %s 2>/dev/null", serviceName), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		active = strings.TrimSpace(r.GetStdout())
	}
	if r, _ := ctx.Execute(fmt.Sprintf("systemctl is-enabled %s 2>/dev/null", serviceName), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		enabled = strings.TrimSpace(r.GetStdout())
	}
	return active, enabled
}

func logClusterStatusLines(ctx *runner.StepContext, output string) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ctx.Logger.Info("cluster-status| %s", line)
		}
	}
}
