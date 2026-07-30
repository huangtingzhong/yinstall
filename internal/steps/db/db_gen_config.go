package db

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

// stepGenConfig 生成 hosts.toml 与集群 TOML 等配置文件
func stepGenConfig() *runner.Step {
	return &runner.Step{
		Name:        "Generate Config",
		Description: "Generate hosts.toml and cluster configuration files",
		Tags:        []string{"db", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// 确认 yasboot 可执行文件存在
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasbootPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s", yasbootPath))
			}

			if err := ensureMultitenantPackageVersionCtx(ctx, ctx.CurrentStepID); err != nil {
				return err
			}
			reportConfigWillOverwrite(ctx, stageDir, clusterName)
			// 只读 shm 门禁提前到 PreCheck，避免 --precheck 漏检
			return checkKernelShmMeetsDBRequirements(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-014: Generate Config")
			if err := checkKernelShmMeetsDBRequirements(ctx); err != nil {
				return err
			}

			isYACMode := ctx.GetParamBool("yac_mode", false)
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			user := ctx.GetParamString("os_user", "yashan")
			password := ctx.GetParamString("os_user_password", "")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
			logPath := ctx.GetParamString("db_log_path", "/data/yashan/log")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			yasbootPath := path.Join(stageDir, "bin/yasboot")

			if isYACMode {
				return genYACConfig(ctx, yasbootPath, clusterName, user, password, installPath, dataPath, logPath, beginPort)
			}
			return genStandaloneConfig(ctx, yasbootPath, clusterName, user, password, installPath, dataPath, logPath, beginPort)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// 确认 hosts.toml 已生成
			hostsPath := path.Join(stageDir, "hosts.toml")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("hosts.toml not found at %s", hostsPath)
			}

			// 确认集群 TOML 已生成
			clusterPath := path.Join(stageDir, clusterName+".toml")
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", clusterPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("cluster config not found at %s", clusterPath)
			}

			ctx.Logger.Info("Config files generated: hosts.toml, %s.toml", clusterName)
			return nil
		},
	}
}

// reportConfigWillOverwrite 已有 hosts.toml / cluster.toml 时提示 apply 将覆盖。
func reportConfigWillOverwrite(ctx *runner.StepContext, stageDir, clusterName string) {
	hostsPath := path.Join(stageDir, "hosts.toml")
	clusterPath := path.Join(stageDir, clusterName+".toml")
	var existing []string
	if res, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(hostsPath)), false); res != nil && res.GetExitCode() == 0 {
		existing = append(existing, hostsPath)
	}
	if res, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(clusterPath)), false); res != nil && res.GetExitCode() == 0 {
		existing = append(existing, clusterPath)
	}
	if len(existing) == 0 {
		return
	}
	ctx.ReportPrecheckIssue(runner.PrecheckIssue{
		StepName:    "Generate Config",
		Host:        ctx.Executor.Host(),
		Severity:    runner.PrecheckSeverityInfo,
		Code:        "PC.DB.CONFIG_WILL_OVERWRITE",
		Message:     fmt.Sprintf("existing config will be regenerated/overwritten on apply: %s", strings.Join(existing, ", ")),
		Remediation: "back up these files if you need the current content; or skip C-014 if configs are already correct",
	})
	ctx.Logger.Info("Existing config detected (will overwrite on apply): %v", existing)
}

// checkKernelShmMeetsDBRequirements 校验 kernel.shmmax / kernel.shmall 是否满足与 OS 步骤 B-008
// 一致的估算规则。若主机无法满足规划的数据库内存百分比（或 standalone OS 下 max-RAM 策略，
// 取决于 os_sysctl_shm_use_max_ram_only），则失败（除非 force）。
func checkKernelShmMeetsDBRequirements(ctx *runner.StepContext) error {
	if ctx.DryRun {
		return nil
	}

	useMaxRAM := ctx.GetParamBool("os_sysctl_shm_use_max_ram_only", false)
	dbPct := ctx.GetParamInt("db_memory_percent", 50)

	for _, th := range ctx.HostsToRun() {
		dbLogPhase(ctx, "host-start", fmt.Sprintf("host=%s op=shm-check", th.Host))
		sub := ctx.ForHost(th)
		memKB, err := parseMemTotalKBFromProc(sub)
		if err != nil {
			return fmt.Errorf("host %s: %w", th.Host, err)
		}
		pageSize, err := parsePageSizeFromHost(sub)
		if err != nil {
			return fmt.Errorf("host %s: %w", th.Host, err)
		}
		curShmmax, curShmall, err := parseShmSysctlFromHost(sub)
		if err != nil {
			return fmt.Errorf("host %s: %w", th.Host, err)
		}

		ok, reason := commonos.ShmMeetsDBRequirement(memKB, pageSize, useMaxRAM, dbPct, curShmmax, curShmall)
		if ok {
			sub.Logger.Info("host %s: kernel shared memory sysctl OK (shmmax=%d shmall=%d)", th.Host, curShmmax, curShmall)
			dbLogPhase(sub, "host-done", fmt.Sprintf("host=%s op=shm-check", th.Host))
			continue
		}
		msg := fmt.Sprintf("host %s: %s (shmmax=%d shmall=%d, MemTotal=%d kB, db_memory_percent=%d, os_sysctl_shm_use_max_ram_only=%v)",
			th.Host, reason, curShmmax, curShmall, memKB, dbPct, useMaxRAM)
		if ctx.IsForceStep() {
			sub.Logger.Warn("%s: %s - continuing because step is forced", ctx.CurrentStepID, msg)
			continue
		}
		return fmt.Errorf("%s: %s - fix sysctl (e.g. re-run OS preparation) or use --force-steps %s to override", ctx.CurrentStepID, msg, ctx.CurrentStepID)
	}
	return nil
}

func parseMemTotalKBFromProc(ctx *runner.StepContext) (int64, error) {
	result, err := ctx.Execute("awk '/^MemTotal:/{print $2}' /proc/meminfo", false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return 0, fmt.Errorf("read MemTotal from /proc/meminfo")
	}
	v, err := strconv.ParseInt(strings.TrimSpace(result.GetStdout()), 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("parse MemTotal")
	}
	return v, nil
}

func parsePageSizeFromHost(ctx *runner.StepContext) (int64, error) {
	result, err := ctx.Execute("getconf PAGE_SIZE 2>/dev/null || echo 4096", false)
	if err != nil || result == nil {
		return 4096, nil
	}
	out := strings.TrimSpace(result.GetStdout())
	if out == "" {
		return 4096, nil
	}
	v, err := strconv.ParseInt(out, 10, 64)
	if err != nil || v <= 0 {
		return 4096, nil
	}
	return v, nil
}

func parseShmSysctlFromHost(ctx *runner.StepContext) (shmmax, shmall int64, err error) {
	r1, e1 := ctx.Execute("sysctl -n kernel.shmmax 2>/dev/null", false)
	if e1 != nil || r1 == nil || r1.GetExitCode() != 0 {
		return 0, 0, fmt.Errorf("read kernel.shmmax (sysctl)")
	}
	r2, e2 := ctx.Execute("sysctl -n kernel.shmall 2>/dev/null", false)
	if e2 != nil || r2 == nil || r2.GetExitCode() != 0 {
		return 0, 0, fmt.Errorf("read kernel.shmall (sysctl)")
	}
	shmmax, err = commonos.ParseSysctlShmValue(r1.GetStdout())
	if err != nil {
		return 0, 0, fmt.Errorf("parse kernel.shmmax")
	}
	shmall, err = commonos.ParseSysctlShmValue(r2.GetStdout())
	if err != nil {
		return 0, 0, fmt.Errorf("parse kernel.shmall")
	}
	return shmmax, shmall, nil
}

func genStandaloneConfig(ctx *runner.StepContext, yasbootPath, clusterName, user, password, installPath, dataPath, logPath string, beginPort int) error {
	stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
	memoryPercent := ctx.GetParamInt("db_memory_percent", 50)
	dbMode := ctx.GetParamString("db_mode", "")

	// 单机：取本机首个 IP（hostname -I）
	result, _ := ctx.Execute("hostname -I | awk '{print $1}'", false)
	ip := "127.0.0.1"
	if result != nil && result.GetStdout() != "" {
		ip = strings.TrimSpace(result.GetStdout())
	}

	ctx.Logger.Info("Generating standalone configuration...")
	ctx.Logger.Info("  Cluster: %s", clusterName)
	ctx.Logger.Info("  IP: %s", ip)
	ctx.Logger.Info("  Install path: %s", installPath)
	ctx.Logger.Info("  Data path: %s", dataPath)
	ctx.Logger.Info("  Log path: %s", logPath)
	ctx.Logger.Info("  Begin port: %d", beginPort)
	ctx.Logger.Info("  Memory limit: %d%%", memoryPercent)
	replicaCIDR := strings.TrimSpace(ctx.GetParamString("db_replica_cidr", ""))
	if replicaCIDR == "" {
		ctx.Logger.Info("  db_replica_cidr: (empty, yasboot-default/public); expected replication port %d", ReplicaPort(beginPort, false))
	} else {
		ctx.Logger.Info("  db_replica_cidr: %s; expected replication port %d", replicaCIDR, ReplicaPort(beginPort, false))
	}

	// 组装 package se gen（以产品用户执行；密码已按 Shell 规则转义）
	genCmd := fmt.Sprintf(`cd %s && %s package se gen --cluster %s --recommend-param \
-u %s -p %s --ip %s --port %d \
--install-path %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--memory-limit %d \
--node 1`,
		stageDir, yasbootPath, clusterName,
		user, commonos.ShellSingleQuote(password), ip, ctx.YasbootRemoteSSHPort(22),
		installPath, dataPath, logPath,
		beginPort, memoryPercent)

	if dbMode == "mysql" {
		genCmd += " \\\n--mode mysql"
	}
	if ctx.GetParamBool("db_enable_pluggable", false) {
		genCmd += " \\\n--enable-pluggable-database"
	}
	genCmd = AppendReplicaCIDRFlag(genCmd, replicaCIDR)

	extra := ctx.GetParamString(ParamYasbootGenExtraArgs, "")
	if replicaCIDR != "" {
		if strings.Contains(extra, "replica-cidr") {
			ctx.Logger.Warn("--db-replica-cidr set; stripping --replica-cidr from --yasboot-gen-extra-args")
		}
		extra = stripYasbootFlag(extra, "replica-cidr")
	}
	genCmd = AppendYasbootGenExtraArgs(genCmd, extra)
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot package se gen: appending extra args: %s", strings.TrimSpace(extra))
	}
	if ctx.GetParamBool("db_enable_pluggable", false) {
		ctx.Logger.Info("yasboot package se gen: multitenant mode enabled (--enable-pluggable-database)")
	}

	dbLogPhase(ctx, "config-gen-start", "standalone package se gen")
	if _, err := commonos.ExecuteAsUserWithCheck(ctx, user, genCmd, true); err != nil {
		dbLogPhase(ctx, "config-gen-fail", runner.TruncateForLog(err.Error(), 120))
		return fmt.Errorf("failed to generate config: %w", err)
	}
	if replicaCIDR != "" {
		configPath := path.Join(stageDir, clusterName+".toml")
		port := ReplicaPort(beginPort, false)
		changed, err := ensureReplicationAddrInClusterTOML(ctx, configPath, port)
		if err != nil {
			dbLogPhase(ctx, "config-gen-fail", runner.TruncateForLog(err.Error(), 120))
			return fmt.Errorf("ensure REPLICATION_ADDR in cluster toml: %w", err)
		}
		if changed {
			ctx.Logger.Info("Ensured REPLICATION_ADDR=:%d in %s (db_replica_cidr=%s)", port, configPath, replicaCIDR)
		} else {
			ctx.Logger.Info("REPLICATION_ADDR=:%d already set in %s, skip rewrite (db_replica_cidr=%s)", port, configPath, replicaCIDR)
		}
	}
	dbLogPhase(ctx, "config-gen-done", "standalone")

	ctx.Logger.Info("Standalone configuration generated successfully")
	return nil
}

func genYACConfig(ctx *runner.StepContext, yasbootPath, clusterName, user, password, installPath, dataPath, logPath string, beginPort int) error {
	stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
	accessMode := ctx.GetParamString("yac_access_mode", "vip")
	interCIDR := ctx.GetParamString("yac_inter_cidr", "")
	publicNetwork := ctx.GetParamString("yac_public_network", "")
	systemdgStr := ctx.GetParamString("yac_systemdg", "")
	datadgStr := ctx.GetParamString("yac_datadg", "")

	// 解析 diskgroup：yasboot 仅接受逗号分隔盘路径填入 --system-data / --data
	systemdg, _ := ParseYACDiskGroup(systemdgStr)
	datadg, _ := ParseYACDiskGroup(datadgStr)

	// 目标 IP 与节点数：YAC 使用 params 中 target_ips；否则退化为当前主机探测值
	ips := "127.0.0.1"
	nodeCount := 1
	if targetIPs := ctx.GetParamStringSlice("target_ips"); len(targetIPs) > 0 {
		ips = strings.Join(targetIPs, ",")
		nodeCount = len(targetIPs)
	} else {
		result, _ := ctx.Execute("hostname -I | awk '{print $1}'", false)
		if result != nil && result.GetStdout() != "" {
			ips = strings.TrimSpace(result.GetStdout())
		}
	}

	// CE 主备集群：--node 表示主集群实例数，--standby-node 表示备集群实例数，
	// --group>=2；此时不能用 len(targets) 填 --node（否则与 extra 中的 --node 冲突，且语义错误）。
	extraEarly := ctx.GetParamString(ParamYasbootGenExtraArgs, "")
	if yacCeHAPrimaryStandbyExtra(extraEarly) {
		if n, ok := parseYasbootIntFlag(extraEarly, "node"); ok && n > 0 {
			nodeCount = n
		} else {
			nodeCount = 1
		}
		ctx.Logger.Info("YAC CE primary-standby gen: using --node %d (not len(targets)); extra=%s",
			nodeCount, strings.TrimSpace(extraEarly))
	}

	ctx.Logger.Info("Generating YAC configuration...")
	ctx.Logger.Info("  Cluster: %s", clusterName)
	ctx.Logger.Info("  Access mode: %s", accessMode)
	ctx.Logger.Info("  IPs: %s", ips)
	ctx.Logger.Info("  Install path: %s", installPath)
	ctx.Logger.Info("  System DG: %s", systemdgStr)
	ctx.Logger.Info("  Data DG: %s", datadgStr)
	replicaCIDR := strings.TrimSpace(ctx.GetParamString("db_replica_cidr", ""))
	if replicaCIDR == "" {
		ctx.Logger.Info("  db_replica_cidr: (empty, yasboot-default/public); expected replication port %d", ReplicaPort(beginPort, true))
	} else {
		ctx.Logger.Info("  db_replica_cidr: %s; expected replication port %d", replicaCIDR, ReplicaPort(beginPort, true))
	}

	// yasboot package ce gen：--system-data 与 --data 为逗号分隔盘路径；gen 阶段不含 arch。
	// ce gen 不支持 --memory-limit / --mode mysql（与 installer.md YAC 章节一致）；内存比例见生成后的 toml 或 --db-memory-percent 仅用于 C-014 shm 校验。
	systemDisks := FormatDiskList(systemdg)
	dataDisks := FormatDiskList(datadg)
	diskFoundPath := ctx.GetParamString("yac_disk_found_path", "/dev/mapper/")

	genCmd := BuildYACCeGenCommand(YACCeGenParams{
		StageDir:      stageDir,
		YasbootPath:   yasbootPath,
		ClusterName:   clusterName,
		User:          user,
		Password:      password,
		IPs:           ips,
		SSHPort:       ctx.YasbootRemoteSSHPort(22),
		InstallPath:   installPath,
		DataPath:      dataPath,
		LogPath:       logPath,
		BeginPort:     beginPort,
		NodeCount:     nodeCount,
		InterCIDR:     interCIDR,
		PublicNetwork: publicNetwork,
		ReplicaCIDR:   replicaCIDR,
		AccessMode:    accessMode,
		ScanName:      ctx.GetParamString("yac_scanname", ""),
		VIPs:          ctx.GetParamStringSlice("yac_vips"),
		DiskFoundPath: diskFoundPath,
		SystemDisks:   systemDisks,
		DataDisks:     dataDisks,
	})

	extra := ctx.GetParamString(ParamYasbootGenExtraArgs, "")
	// 主备 gen 已把 --node 写入基础命令，extra 里若再带 --node 会触发 yasboot "flag --node repeat input"
	if yacCeHAPrimaryStandbyExtra(extra) {
		extra = stripYasbootFlag(extra, "node")
	}
	if replicaCIDR != "" {
		if strings.Contains(extra, "replica-cidr") {
			ctx.Logger.Warn("--db-replica-cidr set; stripping --replica-cidr from --yasboot-gen-extra-args")
		}
		extra = stripYasbootFlag(extra, "replica-cidr")
	}
	if ctx.GetParamBool("db_enable_pluggable", false) {
		genCmd += " \\\n--enable-pluggable-database"
	}
	genCmd = AppendYasbootGenExtraArgs(genCmd, extra)
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot package ce gen: appending extra args: %s", strings.TrimSpace(extra))
	}
	if ctx.GetParamBool("db_enable_pluggable", false) {
		ctx.Logger.Info("yasboot package ce gen: multitenant mode enabled (--enable-pluggable-database)")
	}

	dbLogPhase(ctx, "config-gen-start", fmt.Sprintf("yac nodes=%d access=%s", nodeCount, accessMode))
	if _, err := commonos.ExecuteAsUserWithCheck(ctx, user, genCmd, true); err != nil {
		dbLogPhase(ctx, "config-gen-fail", runner.TruncateForLog(err.Error(), 120))
		return fmt.Errorf("failed to generate YAC config: %w", err)
	}
	if replicaCIDR != "" {
		configPath := path.Join(stageDir, clusterName+".toml")
		port := ReplicaPort(beginPort, true)
		changed, err := ensureReplicationAddrInClusterTOML(ctx, configPath, port)
		if err != nil {
			dbLogPhase(ctx, "config-gen-fail", runner.TruncateForLog(err.Error(), 120))
			return fmt.Errorf("ensure REPLICATION_ADDR in cluster toml: %w", err)
		}
		if changed {
			ctx.Logger.Info("Ensured REPLICATION_ADDR=:%d in %s (db_replica_cidr=%s)", port, configPath, replicaCIDR)
		} else {
			ctx.Logger.Info("REPLICATION_ADDR=:%d already set in %s, skip rewrite (db_replica_cidr=%s)", port, configPath, replicaCIDR)
		}
	}
	dbLogPhase(ctx, "config-gen-done", "yac")

	ctx.Logger.Info("YAC configuration generated successfully")
	return nil
}

// ParseYACDiskGroup 解析盘参数：推荐 `/dev/a,/dev/b`；兼容旧 `role:/dev/a,/dev/b`；空串返回 nil,nil。
// Name 为可选 legacy 标签；yasboot 只用盘路径，运行时 YFS 组名固定 SYSTEM / DG0。
func ParseYACDiskGroup(config string) (*DiskGroupInfo, error) {
	name, disks, err := splitYACDiskGroupSpec(config)
	if err != nil {
		return nil, err
	}
	if name == "" && len(disks) == 0 {
		return nil, nil
	}
	return &DiskGroupInfo{Name: name, Disks: disks}, nil
}

// splitYACDiskGroupSpec 与 OS 侧语义一致（path-only + legacy role:）。
func splitYACDiskGroupSpec(config string) (name string, disks []string, err error) {
	config = strings.TrimSpace(config)
	if config == "" {
		return "", nil, nil
	}
	diskPart := config
	if idx := strings.Index(config, ":"); idx > 0 {
		role := strings.TrimSpace(config[:idx])
		rest := strings.TrimSpace(config[idx+1:])
		if role != "" && !strings.ContainsAny(role, "/,") {
			if rest == "" {
				return "", nil, fmt.Errorf("disk role '%s' must have at least one disk", role)
			}
			name = role
			diskPart = rest
		}
	}
	for _, d := range strings.Split(diskPart, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			disks = append(disks, d)
		}
	}
	if len(disks) == 0 {
		if name != "" {
			return "", nil, fmt.Errorf("disk role '%s' must have at least one disk", name)
		}
		return "", nil, fmt.Errorf("invalid disk list %q, expected '/dev/disk1,/dev/disk2'", config)
	}
	return name, disks, nil
}

// DiskGroupInfo YAC 盘路径列表（Name 为可选 legacy 标签，非 YFS 组名）。
type DiskGroupInfo struct {
	Name  string
	Disks []string
}

// FormatDiskList 将磁盘组盘路径拼为逗号分隔串（供 yasboot --system-data/--data；纯路径）。
func FormatDiskList(dg *DiskGroupInfo) string {
	if dg == nil || len(dg.Disks) == 0 {
		return ""
	}
	return strings.Join(dg.Disks, ",")
}

// DiskPathsFromYACDG 从盘参数取出逗号分隔盘路径（standby CE group gen 等复用）。
func DiskPathsFromYACDG(dgStr string) (string, error) {
	dg, err := ParseYACDiskGroup(dgStr)
	if err != nil {
		return "", err
	}
	return FormatDiskList(dg), nil
}

// DiskPathListFromYACDG 从盘参数取出路径切片。
func DiskPathListFromYACDG(dgStr string) []string {
	dg, err := ParseYACDiskGroup(dgStr)
	if err != nil || dg == nil {
		return nil
	}
	return append([]string(nil), dg.Disks...)
}

// MapYACDiskGroupParam 按盘映射改写参数，输出纯路径列表（兼容读入 legacy role:）。
func MapYACDiskGroupParam(dgStr string, mapDisk func(disk string, index int) string) string {
	dg, err := ParseYACDiskGroup(dgStr)
	if err != nil || dg == nil {
		return dgStr
	}
	updated := make([]string, 0, len(dg.Disks))
	for i, disk := range dg.Disks {
		if mapDisk == nil {
			updated = append(updated, disk)
			continue
		}
		updated = append(updated, mapDisk(disk, i))
	}
	return strings.Join(updated, ",")
}

// NormalizeDiskGroupToYfs 将 diskgroup 字符串中的裸磁盘路径映射为 /dev/yfs/<prefix><n> 路径。
// 每块盘按索引 i 映射到 /dev/yfs/<prefix><i+1>（如 sys→sys1、data→data1、arch→arch1），
// 仅当 /dev/yfs symlink/设备在目标节点存在时替换，否则保留原路径并 Warn。
// 内部使用 MapYACDiskGroupParam 做磁盘级遍历；供 db C-011 与 standby CE 备路径复用。
func NormalizeDiskGroupToYfs(dgStr, prefix string, exec runner.Executor, logger *logging.Logger) string {
	return MapYACDiskGroupParam(dgStr, func(disk string, i int) string {
		alias := fmt.Sprintf("%s%d", prefix, i+1)
		yfsPath := fmt.Sprintf("/dev/yfs/%s", alias)
		result, _ := exec.Execute(fmt.Sprintf("test -L %s || test -b %s", yfsPath, yfsPath), false)
		if result != nil && result.GetExitCode() == 0 {
			if logger != nil {
				logger.Info("  %s -> %s", disk, yfsPath)
			}
			return yfsPath
		}
		if logger != nil {
			logger.Warn("  /dev/yfs/%s not found, keeping path %s", alias, disk)
		}
		return disk
	})
}

// yacCeHAPrimaryStandbyExtra 判断 gen extra 是否在做 CE 主备集群（备集群实例数）。
func yacCeHAPrimaryStandbyExtra(extra string) bool {
	return parseYasbootFlagPresent(extra, "standby-node")
}

// parseYasbootIntFlag 从空格分隔的 yasboot 附加参数中解析 --name N / --name=N。
func parseYasbootIntFlag(extra, name string) (int, bool) {
	fields := strings.Fields(strings.TrimSpace(extra))
	long := "--" + name
	for i, f := range fields {
		if f == long && i+1 < len(fields) {
			n, err := strconv.Atoi(fields[i+1])
			if err == nil {
				return n, true
			}
		}
		if strings.HasPrefix(f, long+"=") {
			n, err := strconv.Atoi(strings.TrimPrefix(f, long+"="))
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func parseYasbootFlagPresent(extra, name string) bool {
	fields := strings.Fields(strings.TrimSpace(extra))
	long := "--" + name
	for _, f := range fields {
		if f == long || strings.HasPrefix(f, long+"=") {
			return true
		}
	}
	return false
}

// stripYasbootFlag 去掉 --name 及其取值（或 --name=val），避免与基础命令重复。
func stripYasbootFlag(extra, name string) string {
	fields := strings.Fields(strings.TrimSpace(extra))
	if len(fields) == 0 {
		return ""
	}
	long := "--" + name
	var out []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == long {
			if i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, long+"=") {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}
