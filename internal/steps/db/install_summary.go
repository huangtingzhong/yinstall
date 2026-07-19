package db

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// ClusterStatusRow 解析 yasboot cluster status -d 表格行（供 standby 等复用）。
type ClusterStatusRow struct {
	Hostid         string
	Nodeid         string
	NodeType       string
	Pid            string
	InstanceStatus string
	ListenAddress  string
	DatabaseStatus string
	DatabaseRole   string
	DataPath       string
}

// ParseClusterStatusTable 解析 yasboot cluster status -d 输出表格。
func ParseClusterStatusTable(output string) []ClusterStatusRow {
	var rows []ClusterStatusRow
	hostCol, nodeCol, typeCol, pidCol, instCol, listenCol, dbStatCol, roleCol, dataCol := -1, -1, -1, -1, -1, -1, -1, -1, -1

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.Split(line, "|")
		fields := make([]string, 0, len(parts))
		for _, p := range parts {
			fields = append(fields, strings.TrimSpace(p))
		}
		if hostCol < 0 || nodeCol < 0 || instCol < 0 || listenCol < 0 {
			for i, f := range fields {
				switch f {
				case "hostid":
					hostCol = i
				case "nodeid":
					nodeCol = i
				case "node_type":
					typeCol = i
				case "pid":
					pidCol = i
				case "instance_status":
					instCol = i
				case "listen_address", "listen_addr":
					listenCol = i
				case "database_status", "pdb_status":
					// CDB status 用 pdb_status；普通实例用 database_status
					if dbStatCol < 0 {
						dbStatCol = i
					}
				case "database_role", "pdb_role":
					// CDB status 用 pdb_role；普通实例用 database_role
					if roleCol < 0 {
						roleCol = i
					}
				case "data_path":
					dataCol = i
				}
			}
			if hostCol >= 0 && nodeCol >= 0 && instCol >= 0 && listenCol >= 0 {
				continue
			}
		}
		if hostCol < 0 || nodeCol < 0 || instCol < 0 || listenCol < 0 {
			continue
		}
		if len(fields) <= nodeCol || len(fields) <= instCol {
			continue
		}
		hostid := fields[hostCol]
		nodeid := fields[nodeCol]
		if hostid == "" || strings.EqualFold(hostid, "hostid") {
			continue
		}
		if nodeid == "" || strings.EqualFold(nodeid, "nodeid") {
			continue
		}
		r := ClusterStatusRow{
			Hostid:         hostid,
			Nodeid:         nodeid,
			InstanceStatus: fields[instCol],
			ListenAddress:  fields[listenCol],
		}
		if typeCol >= 0 && len(fields) > typeCol {
			r.NodeType = fields[typeCol]
		}
		if pidCol >= 0 && len(fields) > pidCol {
			r.Pid = fields[pidCol]
		}
		if dbStatCol >= 0 && len(fields) > dbStatCol {
			r.DatabaseStatus = fields[dbStatCol]
		}
		if roleCol >= 0 && len(fields) > roleCol {
			r.DatabaseRole = fields[roleCol]
		}
		if dataCol >= 0 && len(fields) > dataCol {
			r.DataPath = fields[dataCol]
		}
		rows = append(rows, r)
	}
	return rows
}

// ClusterRowOK 判断单行是否为健康 open 态（主库安装 summary）。
func ClusterRowOK(r ClusterStatusRow) bool {
	inst := strings.ToLower(strings.TrimSpace(r.InstanceStatus))
	db := strings.ToLower(strings.TrimSpace(r.DatabaseStatus))
	if inst != "open" {
		return false
	}
	return db == "normal" || db == "open"
}

// SummaryOKLabel 返回 OK/FAIL。
func SummaryOKLabel(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAIL"
}

// DisplaySysPassword 供终端 summary 显示 SYS 密码。
func DisplaySysPassword(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	pwd := strings.TrimSpace(ctx.GetParamString("db_admin_password", ""))
	if pwd == "" {
		return "(not configured)"
	}
	return pwd
}

func dbSummaryHostLabel(ctx *runner.StepContext, primaryHost string) string {
	hosts := ctx.HostsToRun()
	if len(hosts) <= 1 {
		return primaryHost
	}
	ips := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if ip := strings.TrimSpace(h.Host); ip != "" {
			ips = append(ips, ip)
		}
	}
	return fmt.Sprintf("%s (cluster %d nodes: %s)", primaryHost, len(ips), strings.Join(ips, ", "))
}

func formatDBYasqlExample(ctx *runner.StepContext, connectHost, dbName string, port int) string {
	pwd := strings.TrimSpace(ctx.GetParamString("db_admin_password", ""))
	return "yasql " + commonsql.BuildYasqlTCPConnect(connectHost, "sys", pwd, port, dbName)
}

func resolveDBEnvFileForSummary(ctx, hctx *runner.StepContext) (string, error) {
	if v, ok := ctx.Results["env_file"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), nil
	}
	return resolveDBEnvFile(ctx, hctx)
}

func dbYasbootHomeMarker(clusterName string) string {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		return ""
	}
	return fmt.Sprintf(".yasboot/%s_yasdb_home", clusterName)
}

var reYasdbHomePortSuffix = regexp.MustCompile(`yasdb_home_\d+`)

// processCmdlineMatchesCluster 判断进程命令行是否属于当前集群/端口实例（排除同机其它 yasdb_home_* 实例）。
func processCmdlineMatchesCluster(cmdline, clusterName string, beginPort int) bool {
	cmdline = strings.TrimSpace(cmdline)
	clusterName = strings.TrimSpace(clusterName)
	if cmdline == "" {
		return false
	}
	if clusterName != "" {
		if strings.Contains(cmdline, clusterName) {
			return true
		}
		if marker := dbYasbootHomeMarker(clusterName); marker != "" && strings.Contains(cmdline, marker) {
			return true
		}
	}
	if beginPort <= 0 {
		return false
	}
	if strings.Contains(cmdline, fmt.Sprintf("_%d", beginPort)) ||
		strings.Contains(cmdline, fmt.Sprintf(".port%d", beginPort)) ||
		strings.Contains(cmdline, fmt.Sprintf(".%d", beginPort)) {
		return true
	}
	if cmdlineContainsListenPort(cmdline, beginPort) {
		return true
	}
	if beginPort == 1688 && strings.Contains(cmdline, "yasdb_home") && !reYasdbHomePortSuffix.MatchString(cmdline) {
		return true
	}
	return false
}

func cmdlineContainsListenPort(cmdline string, port int) bool {
	needle := fmt.Sprintf(":%d", port)
	idx := strings.Index(cmdline, needle)
	if idx < 0 {
		return false
	}
	after := idx + len(needle)
	if after >= len(cmdline) {
		return true
	}
	c := cmdline[after]
	return c < '0' || c > '9'
}

func parsePgrepPIDLine(line string) (pid, cmdline string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", "", false
	}
	pid = strings.TrimSpace(fields[0])
	if pid == "" || pid == "0" {
		return "", "", false
	}
	if len(fields) > 1 {
		cmdline = strings.Join(fields[1:], " ")
	} else {
		cmdline = line
	}
	return pid, cmdline, true
}

// probeDBClusterProcess 列出 procName 进程后按集群/端口过滤，避免 pgrep -f 路径前缀误匹配其它实例。
func probeDBClusterProcess(hctx *runner.StepContext, clusterName string, beginPort int, procName string) (running bool, pids []string) {
	procName = strings.TrimSpace(procName)
	if procName == "" {
		return false, nil
	}
	cmd := fmt.Sprintf(`pgrep -a -x %s 2>/dev/null || true`, procName)
	result, _ := hctx.Execute(cmd, false)
	if result == nil {
		return false, nil
	}
	pidSet := map[string]struct{}{}
	for _, line := range strings.Split(result.GetStdout(), "\n") {
		pid, cmdline, ok := parsePgrepPIDLine(line)
		if !ok {
			continue
		}
		if !processCmdlineMatchesCluster(cmdline, clusterName, beginPort) {
			continue
		}
		pidSet[pid] = struct{}{}
	}
	pids = make([]string, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	sort.Strings(pids)
	return len(pids) > 0, pids
}

func yasdbPIDsFromClusterStatus(rows []ClusterStatusRow, beginPort int) []string {
	portNeedle := fmt.Sprintf(":%d", beginPort)
	var pids []string
	for _, r := range rows {
		pid := strings.TrimSpace(r.Pid)
		if pid == "" || pid == "0" {
			continue
		}
		if beginPort > 0 {
			listen := strings.TrimSpace(r.ListenAddress)
			if listen != "" && !strings.Contains(listen, portNeedle) {
				continue
			}
		}
		pids = append(pids, pid)
	}
	return mergeUniquePIDs(nil, pids...)
}

func mergeUniquePIDs(existing []string, add ...string) []string {
	seen := map[string]struct{}{}
	for _, p := range existing {
		p = strings.TrimSpace(p)
		if p == "" || p == "0" {
			continue
		}
		seen[p] = struct{}{}
	}
	for _, p := range add {
		p = strings.TrimSpace(p)
		if p == "" || p == "0" {
			continue
		}
		seen[p] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func formatPIDList(pids []string) string {
	if len(pids) == 0 {
		return "-"
	}
	return strings.Join(pids, ",")
}

func probeDBPortListening(hctx *runner.StepContext, port int) bool {
	result, _ := hctx.Execute(fmt.Sprintf("ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)'", port, port), false)
	return result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != ""
}

func probeDBSystemd(hctx *runner.StepContext, serviceName string) (active, enabled string) {
	active, enabled = "unknown", "unknown"
	if strings.TrimSpace(serviceName) == "" {
		return active, enabled
	}
	if r, _ := hctx.Execute(fmt.Sprintf("systemctl is-active %s 2>/dev/null", serviceName), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		active = strings.TrimSpace(r.GetStdout())
	}
	if r, _ := hctx.Execute(fmt.Sprintf("systemctl is-enabled %s 2>/dev/null", serviceName), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		enabled = strings.TrimSpace(r.GetStdout())
	}
	return active, enabled
}

// SummarizeDBGroupStatus 将 yasboot cluster status -b group 表格行转为摘要文本。
// 按列重排并保留行首 "|"：ConsoleNotice 会 TrimSpace，若仅靠空 group_name 前导空格会对齐失效。
func SummarizeDBGroupStatus(output string) []string {
	var rows [][]string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+") || !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 2 && parts[0] == "" {
			parts = parts[1:]
		}
		if len(parts) >= 1 && parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		cells := make([]string, 0, len(parts))
		allEmpty := true
		for _, p := range parts {
			c := strings.TrimSpace(p)
			cells = append(cells, c)
			if c != "" {
				allEmpty = false
			}
		}
		if len(cells) == 0 || allEmpty {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil
	}

	ncols := 0
	for _, r := range rows {
		if len(r) > ncols {
			ncols = len(r)
		}
	}
	widths := make([]int, ncols)
	for _, r := range rows {
		for i := 0; i < ncols; i++ {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			if n := len(v); n > widths[i] {
				widths[i] = n
			}
		}
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		var b strings.Builder
		for i := 0; i < ncols; i++ {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			b.WriteString("| ")
			b.WriteString(v)
			for pad := widths[i] - len(v); pad > 0; pad-- {
				b.WriteByte(' ')
			}
			b.WriteByte(' ')
		}
		b.WriteByte('|')
		out = append(out, b.String())
	}
	return out
}

// printDBInstallSummary 在 C-034 末尾向终端输出安装摘要（dry-run/precheck 跳过）。
func printDBInstallSummary(ctx *runner.StepContext, hctx *runner.StepContext, stepID, clusterStatusOut, groupStatusOut string) {
	if ctx == nil || hctx == nil || ctx.Logger == nil || ctx.DryRun || ctx.Precheck {
		return
	}

	clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
	beginPort := hctx.GetParamInt("db_begin_port", 1688)
	osUser := hctx.GetParamString("os_user", "yashan")
	isYAC := hctx.GetParamBool("yac_mode", false) || len(ctx.HostsToRun()) > 1
	connectHost := commonsql.YasqlConnectHost(hctx)
	if connectHost == "" {
		connectHost = strings.TrimSpace(hctx.Executor.Host())
	}
	if connectHost == "" {
		connectHost = "localhost"
	}
	primaryHost := connectHost
	if hctx.Executor != nil {
		if h := strings.TrimSpace(hctx.Executor.Host()); h != "" {
			primaryHost = h
		}
	}

	envFile, _ := resolveDBEnvFileForSummary(ctx, hctx)
	statusRows := ParseClusterStatusTable(clusterStatusOut)

	yasdbPIDs := yasdbPIDsFromClusterStatus(statusRows, beginPort)
	if len(yasdbPIDs) == 0 {
		_, yasdbPIDs = probeDBClusterProcess(hctx, clusterName, beginPort, "yasdb")
	}
	yasdbCount := len(yasdbPIDs)
	if yasdbCount == 0 {
		if v, ok := ctx.Results["yasdb_count"].(int); ok && v > 0 {
			yasdbCount = v
		}
	}
	serviceName, _ := commonos.DetermineServiceName(yasdbCount, beginPort)

	notice := func(msg string) {
		ctx.Logger.ConsoleNotice(stepID, msg)
	}

	notice(fmt.Sprintf("========== YashanDB Install Summary (%s) ==========", dbSummaryHostLabel(ctx, primaryHost)))
	notice("[Deployment]")
	if isYAC {
		notice(fmt.Sprintf("  mode=YAC  nodes=%d  cluster=%s  port=%d", len(ctx.HostsToRun()), clusterName, beginPort))
	} else {
		notice(fmt.Sprintf("  mode=standalone  cluster=%s  port=%d", clusterName, beginPort))
	}
	notice(fmt.Sprintf("  charset=%s  os_user=%s", hctx.GetParamString("db_character_set", "utf8"), osUser))
	if mode := strings.TrimSpace(hctx.GetParamString("db_mode", "")); mode != "" {
		notice(fmt.Sprintf("  db_mode=%s", mode))
	}
	if ctxCDBEnabled(hctx) {
		notice("  multitenant=CDB")
		if names, err := pdbNamesFromCtx(hctx); err == nil && len(names) > 0 {
			notice(fmt.Sprintf("  pdbs=%s", strings.Join(names, ", ")))
		}
	}
	if pkg := strings.TrimSpace(hctx.GetParamString("db_package", "")); pkg != "" {
		notice(fmt.Sprintf("  package=%s", filepath.Base(pkg)))
	}
	if isYAC {
		notice(fmt.Sprintf("  access_mode=%s", hctx.GetParamString("yac_access_mode", "vip")))
		if vips := hctx.GetParamStringSlice("yac_vips"); len(vips) > 0 {
			notice(fmt.Sprintf("  vips=%s", strings.Join(vips, ", ")))
		}
		scanIPs := hctx.GetParamStringSlice("yac_scan_ips_list")
		if len(scanIPs) == 0 {
			scanIPs = hctx.GetParamStringSlice("yac_scan_ips")
		}
		if scanName := strings.TrimSpace(hctx.GetParamString("yac_scanname", "")); scanName != "" {
			notice(fmt.Sprintf("  scan=%s", scanName))
		}
		if len(scanIPs) > 0 {
			notice(fmt.Sprintf("  scan_ips=%s", strings.Join(scanIPs, ", ")))
		}
	}

	notice("[Connection]")
	notice(fmt.Sprintf("  host=%s  port=%d  dbname=%s  login=sys", connectHost, beginPort, clusterName))
	notice(fmt.Sprintf("  password=%s", DisplaySysPassword(hctx)))
	notice(fmt.Sprintf("  yasql_example=%s", formatDBYasqlExample(hctx, connectHost, clusterName, beginPort)))
	if envFile != "" {
		notice(fmt.Sprintf("  sysdba_local=source %s && yasql / as sysdba", envFile))
	}
	if ctxCDBEnabled(hctx) {
		if names, err := pdbNamesFromCtx(hctx); err == nil {
			for _, pdb := range names {
				notice(fmt.Sprintf("  pdb_yasql=%s", formatDBYasqlExample(hctx, connectHost, pdb, beginPort)))
			}
		}
	}

	notice("[Paths]")
	notice(fmt.Sprintf("  db_home=%s", hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")))
	notice(fmt.Sprintf("  db_data=%s", hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")))
	notice(fmt.Sprintf("  db_log=%s", hctx.GetParamString("db_log_path", "/data/yashan/log")))
	notice(fmt.Sprintf("  stage_dir=%s", hctx.GetParamString("db_stage_dir", "/home/yashan/install")))
	if envFile != "" {
		notice(fmt.Sprintf("  env_file=%s", envFile))
	}

	notice("[Cluster Health]")
	if len(statusRows) == 0 {
		notice("  (cluster status table not parsed; see debug log for raw output)")
	} else {
		for _, r := range statusRows {
			ok := ClusterRowOK(r)
			pidPart := ""
			if pid := strings.TrimSpace(r.Pid); pid != "" && pid != "0" {
				pidPart = fmt.Sprintf(" pid=%s", pid)
			}
			notice(fmt.Sprintf("  %s node=%s%s listen=%s instance=%s database=%s role=%s %s",
				r.Hostid, r.Nodeid, pidPart, r.ListenAddress,
				r.InstanceStatus, r.DatabaseStatus, r.DatabaseRole, SummaryOKLabel(ok)))
		}
	}

	yasomOK, yasomPIDs := probeDBClusterProcess(hctx, clusterName, beginPort, "yasom")
	yasagentOK, yasagentPIDs := probeDBClusterProcess(hctx, clusterName, beginPort, "yasagent")
	yasdbOK := len(yasdbPIDs) > 0
	notice(fmt.Sprintf("  Processes: yasom=%s yasagent=%s yasdb=%s (cluster=%s)",
		SummaryOKLabel(yasomOK), SummaryOKLabel(yasagentOK), SummaryOKLabel(yasdbOK), clusterName))
	if yasomOK || yasagentOK || yasdbOK {
		notice(fmt.Sprintf("  Process PIDs: yasom=%s yasagent=%s yasdb=%s",
			formatPIDList(yasomPIDs), formatPIDList(yasagentPIDs), formatPIDList(yasdbPIDs)))
	}
	portOK := probeDBPortListening(hctx, beginPort)
	notice(fmt.Sprintf("  Port %d: %s", beginPort, SummaryOKLabel(portOK)))

	if isYAC && strings.TrimSpace(groupStatusOut) != "" {
		notice("[Cluster Resources]")
		notice("  source=yasboot cluster status -b group -d")
		groupRows := ParseClusterStatusTable(groupStatusOut)
		if len(groupRows) > 0 {
			for _, r := range groupRows {
				typePart := ""
				if r.NodeType != "" {
					typePart = " type=" + r.NodeType
				}
				dataPart := ""
				if r.DataPath != "" {
					dataPart = " data=" + r.DataPath
				}
				notice(fmt.Sprintf("  %s node=%s%s listen=%s instance=%s database=%s role=%s%s",
					r.Hostid, r.Nodeid, typePart, r.ListenAddress,
					r.InstanceStatus, r.DatabaseStatus, r.DatabaseRole, dataPart))
			}
		} else {
			for _, line := range SummarizeDBGroupStatus(groupStatusOut) {
				notice("  " + line)
			}
		}
	}

	notice("[Service]")
	notice(fmt.Sprintf("  cluster_cmd=yasboot cluster start|stop -c %s", clusterName))
	notice(fmt.Sprintf("  status_cmd=yasboot cluster status -c %s -d", clusterName))
	notice(fmt.Sprintf("  monit_script=%s", commonos.ScriptPath))
	active, enabled := probeDBSystemd(hctx, serviceName)
	if active != "unknown" || enabled != "unknown" {
		notice(fmt.Sprintf("  systemd=%s  active=%s  enabled=%s", serviceName, active, enabled))
	} else {
		notice(fmt.Sprintf("  systemd=%s  (not configured or C-033 skipped)", serviceName))
	}
	notice("====================================================")
}
