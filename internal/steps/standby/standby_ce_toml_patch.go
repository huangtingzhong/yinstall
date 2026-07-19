// standby_ce_toml_patch.go - CE 备集群 yashandb_add.toml / hosts_add.toml 修补
// 将 gen 产物改为 standby 角色并走私网互联/复制

package standby

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
)

// StandbyCETomlPatchOpt CE add.toml 修补选项。
type StandbyCETomlPatchOpt struct {
	InterCIDR     string
	PublicNetwork string
	DataPath      string
	LogPath       string
	ReplicaPort   int // 如 1690
	InterPort     int // CLUSTER_INTERCONNECT 端口, 默认 begin+1
	InterURLPort  int // INTER_URL 端口, 默认 begin+100 或 1788（演练）

	// ApplyYFSPatch 为 true 时写入数据盘组名 / ARCHIVE_LOCAL_DEST / REDO|DB_FILE_NAME_CONVERT。
	ApplyYFSPatch       bool
	DataDiskgroupName   string // 须与主库众数 data 组一致；仅改 [[group.diskgroup]]，不改 SYSTEM
	ArchiveLocalDest    string // 如 +DG0/arch_files
	RedoFileNameConvert string // 空则删除已有行；非空则写入（redo 组异名映射，如 +REDO→+DG0）
	DBFileNameConvert   string // 空则删除；非空则写入（主库额外 data 组映到备侧众数组，如 +DG1→+DG0）
}

// PrimaryYFSLayout 主库 YFS/路径探测结果。
type PrimaryYFSLayout struct {
	DataDG      string   // 数据文件众数盘组（备侧对齐目标）
	DataDGs     []string // 含数据文件的全部盘组（已排序；众数在前）
	RedoDG      string   // redo 文件所在组
	ArchiveDest string   // ARCHIVE_LOCAL_DEST 原值（可空）
	Diskgroups  []string
}

// StandbyYFSAvailability 备侧首轮可用盘组（gen 默认通常无 REDO/ARCH）。
type StandbyYFSAvailability struct {
	HasREDO bool
	HasARCH bool
}

// StandbyCEYFSPatch 由探测结果推导的首轮 patch 字段。
type StandbyCEYFSPatch struct {
	DataDiskgroup       string
	ArchiveLocalDest    string
	RedoFileNameConvert string
	DBFileNameConvert   string // 主库多 data 组时映到备侧众数组；整组改名场景不用此字段
}

// YFSDiskgroupFromPath 从 +DG0/dbfiles/x 提取 DG0；非 YFS 返回空。
func YFSDiskgroupFromPath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "+") {
		return ""
	}
	rest := strings.TrimPrefix(p, "+")
	if i := strings.IndexByte(rest, '/'); i > 0 {
		return rest[:i]
	}
	return rest
}

func isYFSRedoPath(p string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	if !strings.HasPrefix(p, "+") {
		return false
	}
	base := p
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		base = p[i+1:]
	}
	if !strings.HasPrefix(base, "redo") {
		return false
	}
	suf := strings.TrimPrefix(base, "redo")
	if suf == "" {
		return true
	}
	for _, c := range suf {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isYFSCtrlPath(p string) bool {
	return strings.Contains(strings.ToLower(p), "ctrlfile")
}

// ParsePrimaryYFSProbe 解析主库 yasql 探测输出（datafile/logfile/参数/diskgroup 混排均可）。
func ParsePrimaryYFSProbe(stdout string) (PrimaryYFSLayout, error) {
	dataCount := map[string]int{}
	redoCount := map[string]int{}
	dgSet := map[string]struct{}{}
	archDest := ""

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "ARCHIVE_LOCAL_DEST") {
			fields := strings.Fields(line)
			for i := len(fields) - 1; i >= 0; i-- {
				v := fields[i]
				if strings.HasPrefix(v, "+") || strings.Contains(v, "/") {
					archDest = v
					break
				}
			}
		}
		if strings.HasPrefix(line, "+") {
			dg := YFSDiskgroupFromPath(line)
			if dg == "" {
				continue
			}
			dgSet[dg] = struct{}{}
			switch {
			case isYFSRedoPath(line):
				redoCount[dg]++
			case isYFSCtrlPath(line):
				// ignore
			default:
				dataCount[dg]++
			}
			continue
		}
		// v$yfs_diskgroup 名列：单独一行的组名
		if reYFSDiskgroupNameLine.MatchString(line) {
			name := strings.Fields(line)[0]
			switch name {
			case "NAME", "STATE", "TOTAL_MB", "SQL":
			default:
				dgSet[name] = struct{}{}
			}
		}
	}

	dataMajority := majorityKey(dataCount)
	layout := PrimaryYFSLayout{
		DataDG:      dataMajority,
		DataDGs:     sortedDataDGs(dataCount, dataMajority),
		RedoDG:      majorityKey(redoCount),
		ArchiveDest: archDest,
	}
	if layout.RedoDG == "" {
		layout.RedoDG = layout.DataDG
	}
	if layout.DataDG == "" && archDest != "" {
		layout.DataDG = YFSDiskgroupFromPath(archDest)
		if layout.DataDG != "" && len(layout.DataDGs) == 0 {
			layout.DataDGs = []string{layout.DataDG}
		}
	}
	for n := range dgSet {
		layout.Diskgroups = append(layout.Diskgroups, n)
	}
	sort.Strings(layout.Diskgroups)
	return layout, nil
}

// sortedDataDGs 众数组在前，其余按字典序（稳定生成 CONVERT）。
func sortedDataDGs(counts map[string]int, majority string) []string {
	if len(counts) == 0 {
		return nil
	}
	var rest []string
	for k := range counts {
		if majority != "" && strings.EqualFold(k, majority) {
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	out := make([]string, 0, len(rest)+1)
	if majority != "" {
		out = append(out, majority)
	}
	out = append(out, rest...)
	return out
}

var reYFSDiskgroupNameLine = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,30}(\s|$)`)

func majorityKey(m map[string]int) string {
	best := ""
	bestN := 0
	for k, n := range m {
		if n > bestN || (n == bestN && (best == "" || k < best)) {
			best = k
			bestN = n
		}
	}
	return best
}

// DeriveStandbyCEYFSPatch 推导首轮数据组对齐、REDO/DB CONVERT 与 ARCHIVE_LOCAL_DEST。
// 备侧 data 组名必须与主库众数 DataDG 一致（CE 不支持把主 data 组改成异名再靠 CONVERT 建库）。
// 主库若还有其它 data 组：写 DB_FILE_NAME_CONVERT 映到众数组（真机已验证可行）。
// redo 落点回退：REDO → ARCH → 备侧 data 组（与生产手册一致）。
func DeriveStandbyCEYFSPatch(layout PrimaryYFSLayout, avail StandbyYFSAvailability) StandbyCEYFSPatch {
	data := strings.TrimSpace(layout.DataDG)
	redoDst := data
	if avail.HasREDO {
		redoDst = "REDO"
	} else if avail.HasARCH {
		redoDst = "ARCH"
	}
	archDG := data
	if avail.HasARCH {
		archDG = "ARCH"
	}
	out := StandbyCEYFSPatch{DataDiskgroup: data}
	if archDG != "" {
		out.ArchiveLocalDest = "+" + archDG + "/arch_files"
	}
	src := strings.TrimSpace(layout.RedoDG)
	if src != "" && redoDst != "" && !strings.EqualFold(src, redoDst) {
		out.RedoFileNameConvert = fmt.Sprintf("'+%s/dbfiles','+%s/dbfiles'", src, redoDst)
	}
	out.DBFileNameConvert = buildDBFileNameConvert(layout.DataDGs, data)
	return out
}

// buildDBFileNameConvert 将众数以外的 data 组映到 dst（'+DG1/dbfiles','+DG0/dbfiles'[,'+DG2/...']）。
func buildDBFileNameConvert(dataDGs []string, dst string) string {
	dst = strings.TrimSpace(dst)
	if dst == "" || len(dataDGs) <= 1 {
		return ""
	}
	var pairs []string
	for _, dg := range dataDGs {
		dg = strings.TrimSpace(dg)
		if dg == "" || strings.EqualFold(dg, dst) {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("'+%s/dbfiles','+%s/dbfiles'", dg, dst))
	}
	return strings.Join(pairs, ",")
}

// PatchStandbyCEHostsAddTOML 修补 hosts_add.toml：为各 [[host]] 补齐/校正私网 yasdb_ip。
func PatchStandbyCEHostsAddTOML(raw, interCIDR string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("empty hosts_add.toml")
	}
	cidr := strings.TrimSpace(interCIDR)
	if cidr == "" {
		return raw, nil
	}
	out := ensureHostYasdbIP(raw, cidr)
	// 已有 inter_ip / replica_ip 若落在 CIDR 外则按末段映射改写
	out = rewriteQuotedHostIPsInCIDR(out, cidr, []string{"inter_ip", "inter_url", "replica_ip"})
	return out, nil
}

// rewriteQuotedHostIPsInCIDR 将 key = "ip" 中不在 cidr 的地址映射进 cidr（末段启发式）。
func rewriteQuotedHostIPsInCIDR(toml, cidr string, keys []string) string {
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return toml
	}
	out := toml
	for _, key := range keys {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)(%s\s*=\s*)"([^"]+)"`, regexp.QuoteMeta(key)))
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) < 3 {
				return m
			}
			ip := net.ParseIP(strings.TrimSpace(sub[2]))
			if ip == nil {
				return m
			}
			if ok, _ := commonos.IPInSubnet(ip.String(), cidr); ok {
				return m
			}
			mapped := mapIPIntoCIDR(ip, ipnet)
			if mapped == "" {
				return m
			}
			return sub[1] + `"` + mapped + `"`
		})
	}
	return out
}

// PatchStandbyCEAddTOML 修补 group gen 产出的 *_add.toml。
func PatchStandbyCEAddTOML(raw string, opt StandbyCETomlPatchOpt) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("empty add.toml")
	}
	out := raw

	// database_role -> standby
	reRole := regexp.MustCompile(`(?i)(database_role\s*=\s*)"[^"]*"`)
	if reRole.MatchString(out) {
		out = reRole.ReplaceAllString(out, `${1}"standby"`)
	} else {
		// 在首个 [[group]] 块内尽量插入
		out = injectAfterGroupHeader(out, `  database_role = "standby"`)
	}

	if pn := strings.TrimSpace(opt.PublicNetwork); pn != "" {
		rePub := regexp.MustCompile(`(?i)(public_network\s*=\s*)"[^"]*"`)
		if rePub.MatchString(out) {
			out = rePub.ReplaceAllString(out, fmt.Sprintf(`${1}"%s"`, pn))
		}
	}

	if dp := strings.TrimSpace(opt.DataPath); dp != "" {
		reData := regexp.MustCompile(`(?i)(data_path\s*=\s*)"[^"]*"`)
		out = reData.ReplaceAllString(out, fmt.Sprintf(`${1}"%s"`, dp))
	}
	if lp := strings.TrimSpace(opt.LogPath); lp != "" {
		reLog := regexp.MustCompile(`(?i)(log_path\s*=\s*)"[^"]*"`)
		out = reLog.ReplaceAllString(out, fmt.Sprintf(`${1}"%s"`, lp))
	}

	if opt.ApplyYFSPatch {
		out = applyStandbyCEYFSPatch(out, opt)
	}

	interPort := opt.InterPort
	if interPort <= 0 {
		interPort = 1689
	}
	urlPort := opt.InterURLPort
	if urlPort <= 0 {
		urlPort = 1788
	}
	repPort := opt.ReplicaPort
	if repPort <= 0 {
		repPort = 1690
	}

	cidr := strings.TrimSpace(opt.InterCIDR)
	if cidr != "" {
		out = rewriteAddrKeysToCIDR(out, cidr, map[string]int{
			"CLUSTER_INTERCONNECT": interPort,
			"INTER_URL":            urlPort,
			"REPLICATION_ADDR":     repPort,
		})
		out = ensureHostYasdbIP(out, cidr)
	}

	return out, nil
}

// applyStandbyCEYFSPatch 写入数据盘组名、ARCHIVE_LOCAL_DEST、REDO/DB_FILE_NAME_CONVERT。
// DB_FILE_NAME_CONVERT：仅用于「主库额外 data 组 → 备侧众数组」；空则清除残留，避免误导整组改名。
func applyStandbyCEYFSPatch(toml string, opt StandbyCETomlPatchOpt) string {
	out := toml
	if dg := strings.TrimSpace(opt.DataDiskgroupName); dg != "" {
		out = patchGroupDiskgroupName(out, dg)
	}
	if dest := strings.TrimSpace(opt.ArchiveLocalDest); dest != "" {
		out = upsertNodeConfigQuoted(out, "ARCHIVE_LOCAL_DEST", dest)
	}
	if opt.ApplyYFSPatch {
		dbConv := strings.TrimSpace(opt.DBFileNameConvert)
		if dbConv == "" {
			out = removeNodeConfigKey(out, "DB_FILE_NAME_CONVERT")
		} else {
			out = upsertNodeConfigQuoted(out, "DB_FILE_NAME_CONVERT", dbConv)
		}
		redoConv := strings.TrimSpace(opt.RedoFileNameConvert)
		if redoConv == "" {
			out = removeNodeConfigKey(out, "REDO_FILE_NAME_CONVERT")
		} else {
			out = upsertNodeConfigQuoted(out, "REDO_FILE_NAME_CONVERT", redoConv)
		}
	}
	return out
}

// patchGroupDiskgroupName 仅修改首个 [[group.diskgroup]] 的 name，不改 systemdiskgroup。
func patchGroupDiskgroupName(toml, newName string) string {
	re := regexp.MustCompile(`(?s)(\[\[group\.diskgroup\]\].*?)(name\s*=\s*)"[^"]*"`)
	loc := re.FindStringSubmatchIndex(toml)
	if loc == nil || len(loc) < 6 {
		return toml
	}
	return toml[:loc[0]] + toml[loc[2]:loc[3]] + toml[loc[4]:loc[5]] + `"` + newName + `"` + toml[loc[1]:]
}

func upsertNodeConfigQuoted(toml, key, value string) string {
	re := regexp.MustCompile(`(?i)(` + regexp.QuoteMeta(key) + `\s*=\s*)"[^"]*"`)
	if re.MatchString(toml) {
		return re.ReplaceAllString(toml, `${1}"`+value+`"`)
	}
	// 插入到首个 [group.node.config] 块内
	reHdr := regexp.MustCompile(`(?m)^(\s*\[group\.node\.config\]\s*)$`)
	loc := reHdr.FindStringIndex(toml)
	if loc == nil {
		return toml + fmt.Sprintf("\n  %s = %q\n", key, value)
	}
	insert := fmt.Sprintf("\n      %s = %q", key, value)
	return toml[:loc[1]] + insert + toml[loc[1]:]
}

func removeNodeConfigKey(toml, key string) string {
	re := regexp.MustCompile(`(?im)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*"[^"]*"\s*\n`)
	return re.ReplaceAllString(toml, "")
}

func injectAfterGroupHeader(toml, line string) string {
	re := regexp.MustCompile(`(?m)^\[\[group\]\]\s*$`)
	loc := re.FindStringIndex(toml)
	if loc == nil {
		return toml + "\n" + line + "\n"
	}
	insertAt := loc[1]
	return toml[:insertAt] + "\n" + line + toml[insertAt:]
}

// rewriteAddrKeysToCIDR 将 KEY = "ip:port" 中 ip 若能映射到同一主机在 CIDR 内的私网则改写。
// 简化策略：若当前 ip 已在 CIDR 内则只校正端口；否则把地址最后一段保留、网络前缀换成 CIDR 网络（仅当公网与私网末段一致时，如演练 172/173）。
func rewriteAddrKeysToCIDR(toml, cidr string, keyPorts map[string]int) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return toml
	}
	out := toml
	for key, port := range keyPorts {
		re := regexp.MustCompile(`(?i)(` + regexp.QuoteMeta(key) + `\s*=\s*")([^"]+)(")`)
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) < 4 {
				return m
			}
			hostPort := sub[2]
			host, _, _ := splitHostPortLoose(hostPort)
			ip := net.ParseIP(host)
			if ip == nil {
				return m
			}
			priv := mapIPIntoCIDR(ip, ipnet)
			if priv == "" {
				return m
			}
			return sub[1] + fmt.Sprintf("%s:%d", priv, port) + sub[3]
		})
	}
	return out
}

func splitHostPortLoose(s string) (host, port string, ok bool) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i > 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

// mapIPIntoCIDR：若 ip 已在网段内返回原 ip；否则用网段前缀 + 原 ip 最后一字节构造（IPv4）。
func mapIPIntoCIDR(ip net.IP, ipnet *net.IPNet) string {
	ip4 := ip.To4()
	if ip4 == nil || ipnet == nil {
		return ""
	}
	if ipnet.Contains(ip4) {
		return ip4.String()
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones > 24 {
		// 仅对 /24 及更宽的常见演练网段做末段映射
		if ones > 24 {
			return ""
		}
	}
	base := ipnet.IP.To4()
	if base == nil {
		return ""
	}
	mapped := make(net.IP, 4)
	copy(mapped, base)
	// 保留主机部分：对 /24 用最后一字节
	mapped[3] = ip4[3]
	if !ipnet.Contains(mapped) {
		return ""
	}
	return mapped.String()
}

// ensureHostYasdbIP 为每个 [[host]] 补齐 [host.yasdb_ip]（按 host 块内已有 LISTEN/公网 IP 映射）。
func ensureHostYasdbIP(toml, cidr string) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return toml
	}
	// 按 [[host]] 分块处理
	parts := regexp.MustCompile(`(?m)^\[\[host\]\]\s*$`).Split(toml, -1)
	if len(parts) <= 1 {
		return toml
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for i := 1; i < len(parts); i++ {
		b.WriteString("[[host]]")
		block := parts[i]
		if strings.Contains(block, "[host.yasdb_ip]") {
			b.WriteString(block)
			continue
		}
		// 从块内找一个 IPv4
		reIP := regexp.MustCompile(`\b(\d{1,3}(?:\.\d{1,3}){3})\b`)
		m := reIP.FindStringSubmatch(block)
		priv := ""
		if len(m) >= 2 {
			if ip := net.ParseIP(m[1]); ip != nil {
				priv = mapIPIntoCIDR(ip, ipnet)
			}
		}
		if priv == "" {
			b.WriteString(block)
			continue
		}
		yasdb := fmt.Sprintf("\n  [host.yasdb_ip]\n    inter_ip = %q\n    inter_url = %q\n    replica_ip = %q\n", priv, priv, priv)
		// 必须插在 host 标量键之后、子表之前；插在 hostid 后会把 ip/user 挤出 [[host]]（yasboot: ipAddr not set）
		reSub := regexp.MustCompile(`(?m)^(\s*\[host\.)`)
		if loc := reSub.FindStringIndex(block); loc != nil {
			block = block[:loc[0]] + yasdb + block[loc[0]:]
		} else {
			block = strings.TrimRight(block, "\n") + yasdb
		}
		b.WriteString(block)
	}
	return b.String()
}

// PublicNetworkFromTOML 从 cluster/hosts toml 片段提取 public_network。
func PublicNetworkFromTOML(toml string) string {
	re := regexp.MustCompile(`(?i)public_network\s*=\s*"([^"]+)"`)
	if m := re.FindStringSubmatch(toml); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// ResolveNodeInterIP 解析节点私网 IP：toml inter_ip → 探测地址落 CIDR → 公网末段映射。
func ResolveNodeInterIP(hostid, listenIP, interCIDR, tomlInterIP string, probedIPs []string) (string, error) {
	candidates := make([]string, 0, 4+len(probedIPs))
	if ip := strings.TrimSpace(tomlInterIP); ip != "" {
		candidates = append(candidates, ip)
	}
	candidates = append(candidates, probedIPs...)
	if listenIP = strings.TrimSpace(listenIP); listenIP != "" {
		candidates = append(candidates, listenIP)
	}
	if ip := MatchIPInCIDR(candidates, interCIDR); ip != "" {
		return ip, nil
	}
	// 末段启发式（仅作最后手段）
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(interCIDR))
	if err != nil {
		return "", fmt.Errorf("invalid inter-cidr %q: %w", interCIDR, err)
	}
	if lip := net.ParseIP(listenIP); lip != nil {
		if mapped := mapIPIntoCIDR(lip, ipnet); mapped != "" {
			return mapped, nil
		}
	}
	return "", fmt.Errorf("cannot resolve private IP for hostid=%s listen=%s in %s (toml/probe/map all failed)", hostid, listenIP, interCIDR)
}

// InterIPFromHostsTOML 按 hostid 取 inter_ip。
func InterIPFromHostsTOML(toml, hostid string) string {
	hostid = strings.TrimSpace(hostid)
	if hostid == "" || toml == "" {
		return ""
	}
	parts := regexp.MustCompile(`(?m)^\[\[host\]\]\s*$`).Split(toml, -1)
	for i := 1; i < len(parts); i++ {
		block := parts[i]
		if !strings.Contains(block, `"`+hostid+`"`) && !strings.Contains(block, hostid) {
			continue
		}
		re := regexp.MustCompile(`(?i)inter_ip\s*=\s*"([^"]+)"`)
		if m := re.FindStringSubmatch(block); len(m) >= 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// MatchIPInCIDR 从候选地址中选第一个落在 cidr 的 IPv4（复用 commonos.IPInSubnet）。
func MatchIPInCIDR(candidates []string, cidr string) string {
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		// 允许 cidr 形式
		if strings.Contains(c, "/") {
			if host, _, err := net.ParseCIDR(c); err == nil {
				c = host.String()
			}
		}
		ok, err := commonos.IPInSubnet(c, cidr)
		if err == nil && ok {
			return c
		}
	}
	return ""
}

// ReplicationAddrPlan 单个主实例的目标 REPLICATION_ADDR。
type ReplicationAddrPlan struct {
	Nodeid string
	Hostid string
	Addr   string // ip:port
}

// PlanPrimaryReplicationAddrs 为每个节点生成目标地址（interIP 优先，否则 listenIP 若在 cidr 内）。
func PlanPrimaryReplicationAddrs(hostid, nodeid, listenIP, interIP, interCIDR string, port int) (ReplicationAddrPlan, error) {
	ip := strings.TrimSpace(interIP)
	if ip == "" {
		ip = MatchIPInCIDR([]string{listenIP}, interCIDR)
	}
	if ip == "" {
		// 尝试公网末段映射到私网
		if lip := net.ParseIP(strings.TrimSpace(listenIP)); lip != nil {
			if _, ipnet, err := net.ParseCIDR(strings.TrimSpace(interCIDR)); err == nil {
				ip = mapIPIntoCIDR(lip, ipnet)
			}
		}
	}
	if ip == "" {
		return ReplicationAddrPlan{}, fmt.Errorf("cannot resolve private IP for node %s (host=%s listen=%s) in %s", nodeid, hostid, listenIP, interCIDR)
	}
	if ok, err := commonos.IPInSubnet(ip, interCIDR); err != nil || !ok {
		return ReplicationAddrPlan{}, fmt.Errorf("resolved IP %s for node %s is outside %s", ip, nodeid, interCIDR)
	}
	return ReplicationAddrPlan{Nodeid: nodeid, Hostid: hostid, Addr: fmt.Sprintf("%s:%d", ip, port)}, nil
}

// ParseReplicationAddrValue 从 yasql 查询输出解析 REPLICATION_ADDR 值。
func ParseReplicationAddrValue(stdout string) (addr string, configured bool) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "---") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "name") && strings.Contains(strings.ToLower(line), "value") {
			continue
		}
		if !strings.Contains(strings.ToUpper(line), "REPLICATION_ADDR") {
			continue
		}
		var value string
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 2 {
				value = strings.TrimSpace(parts[len(parts)-1])
				if len(parts) >= 3 {
					value = strings.TrimSpace(parts[2])
				}
			}
		} else {
			upperLine := strings.ToUpper(line)
			idx := strings.Index(upperLine, "REPLICATION_ADDR")
			if idx >= 0 {
				fields := strings.Fields(strings.TrimSpace(line[idx+len("REPLICATION_ADDR"):]))
				if len(fields) > 0 {
					value = fields[len(fields)-1]
				}
			}
		}
		if value != "" && !strings.EqualFold(value, "null") && !strings.EqualFold(value, "none") {
			return value, true
		}
	}
	return "", false
}
