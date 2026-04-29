package db

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC014GenConfig 生成 hosts.toml 与集群 TOML 等配置文件
func StepC014GenConfig() *runner.Step {
	return &runner.Step{
		ID:          "C-014",
		Name:        "Generate Config",
		Description: "Generate hosts.toml and cluster configuration files",
		Tags:        []string{"db", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// 确认 yasboot 可执行文件存在
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasbootPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s", yasbootPath))
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
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
			continue
		}
		msg := fmt.Sprintf("host %s: %s (shmmax=%d shmall=%d, MemTotal=%d kB, db_memory_percent=%d, os_sysctl_shm_use_max_ram_only=%v)",
			th.Host, reason, curShmmax, curShmall, memKB, dbPct, useMaxRAM)
		if ctx.IsForceStep() {
			sub.Logger.Warn("C-014: %s - continuing because step is forced", msg)
			continue
		}
		return fmt.Errorf("C-014: %s - fix sysctl (e.g. re-run OS preparation) or use --force-steps C-014 to override", msg)
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
	shmmax, err = strconv.ParseInt(strings.TrimSpace(r1.GetStdout()), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse kernel.shmmax")
	}
	shmall, err = strconv.ParseInt(strings.TrimSpace(r2.GetStdout()), 10, 64)
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

	extra := ctx.GetParamString("yasboot_extra_args", "")
	genCmd = commonos.YasbootAppendExtraArgs(genCmd, extra, false)
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot package se gen: appending extra args: %s", strings.TrimSpace(extra))
	}

	// 以产品用户执行
	if _, err := commonos.ExecuteAsUserWithCheck(ctx, user, genCmd, true); err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}

	ctx.Logger.Info("Standalone configuration generated successfully")
	return nil
}

func genYACConfig(ctx *runner.StepContext, yasbootPath, clusterName, user, password, installPath, dataPath, logPath string, beginPort int) error {
	stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
	memoryPercent := ctx.GetParamInt("db_memory_percent", 50)
	accessMode := ctx.GetParamString("yac_access_mode", "vip")
	interCIDR := ctx.GetParamString("yac_inter_cidr", "")
	publicNetwork := ctx.GetParamString("yac_public_network", "")
	systemdgStr := ctx.GetParamString("yac_systemdg", "")
	datadgStr := ctx.GetParamString("yac_datadg", "")
	dbMode := ctx.GetParamString("db_mode", "")

	// 解析 diskgroup：yasboot 仅接受逗号分隔盘路径填入 --system-data / --data
	systemdg, _ := parseYACDiskGroup(systemdgStr)
	datadg, _ := parseYACDiskGroup(datadgStr)

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

	ctx.Logger.Info("Generating YAC configuration...")
	ctx.Logger.Info("  Cluster: %s", clusterName)
	ctx.Logger.Info("  Access mode: %s", accessMode)
	ctx.Logger.Info("  IPs: %s", ips)
	ctx.Logger.Info("  Install path: %s", installPath)
	ctx.Logger.Info("  System DG: %s", systemdgStr)
	ctx.Logger.Info("  Data DG: %s", datadgStr)
	ctx.Logger.Info("  Memory limit: %d%%", memoryPercent)

	// yasboot package ce gen：--system-data 与 --data 为逗号分隔盘路径；gen 阶段不含 arch
	systemDisks := formatDiskList(systemdg)
	dataDisks := formatDiskList(datadg)
	diskFoundPath := ctx.GetParamString("yac_disk_found_path", "/dev/mapper/")

	// 组装 package ce gen（以产品用户执行）
	var genCmd string
	if accessMode == "scan" {
		scanName := ctx.GetParamString("yac_scanname", "")
		genCmd = fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
-u %s -p %s --ip %s --port %d \
-i %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--memory-limit %d \
--node %d \
--inter-cidr %s \
--public-network %s \
--scanname %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			stageDir, yasbootPath, clusterName,
			user, commonos.ShellEscapeForSuC(password), ips, ctx.YasbootRemoteSSHPort(22),
			installPath, dataPath, logPath,
			beginPort, memoryPercent, nodeCount,
			interCIDR, publicNetwork, scanName,
			diskFoundPath,
			systemDisks, dataDisks)
	} else {
		vips := ctx.GetParamStringSlice("yac_vips")
		// yasboot 期望 VIP 形如 ip/前缀长度，例如 10.10.10.127/24（可选带网卡）
		vipNetmask := publicNetwork
		if vipNetmask == "" {
			vipNetmask = interCIDR
		}
		prefixLen := 24
		if vipNetmask != "" {
			if pl, err := commonos.CIDRPrefixLen(vipNetmask); err == nil {
				prefixLen = pl
			}
		}
		var vipParts []string
		for _, v := range vips {
			v = strings.TrimSpace(v)
			if v != "" {
				vipParts = append(vipParts, fmt.Sprintf("%s/%d", v, prefixLen))
			}
		}
		vipStr := strings.Join(vipParts, ",")
		genCmd = fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
-u %s -p %s --ip %s --port %d \
-i %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--memory-limit %d \
--node %d \
--inter-cidr %s \
--public-network %s \
--vips %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			stageDir, yasbootPath, clusterName,
			user, commonos.ShellSingleQuote(password), ips, ctx.YasbootRemoteSSHPort(22),
			installPath, dataPath, logPath,
			beginPort, memoryPercent, nodeCount,
			interCIDR, publicNetwork, vipStr,
			diskFoundPath,
			systemDisks, dataDisks)
	}

	if dbMode == "mysql" {
		genCmd += " \\\n--mode mysql"
	}

	extra := ctx.GetParamString("yasboot_extra_args", "")
	genCmd = commonos.YasbootAppendExtraArgs(genCmd, extra, false)
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot package ce gen: appending extra args: %s", strings.TrimSpace(extra))
	}

	// 以产品用户执行
	if _, err := commonos.ExecuteAsUserWithCheck(ctx, user, genCmd, true); err != nil {
		return fmt.Errorf("failed to generate YAC config: %w", err)
	}

	ctx.Logger.Info("YAC configuration generated successfully")
	return nil
}

func parseYACDiskGroup(config string) (*DiskGroupInfo, error) {
	if config == "" {
		return nil, nil
	}
	parts := strings.SplitN(config, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid diskgroup format: %s", config)
	}
	var disks []string
	for _, d := range strings.Split(parts[1], ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			disks = append(disks, d)
		}
	}
	return &DiskGroupInfo{Name: parts[0], Disks: disks}, nil
}

type DiskGroupInfo struct {
	Name  string
	Disks []string
}

func formatDiskList(dg *DiskGroupInfo) string {
	if dg == nil || len(dg.Disks) == 0 {
		return ""
	}
	return strings.Join(dg.Disks, ",")
}
