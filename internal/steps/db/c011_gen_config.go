package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC005GenConfig Generate DB configuration files
func StepC011GenConfig() *runner.Step {
	return &runner.Step{
		ID:          "C-011",
		Name:        "Generate Config",
		Description: "Generate hosts.toml and cluster configuration files",
		Tags:        []string{"db", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// Check yasboot exists
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasbootPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("yasboot not found at %s", yasbootPath)
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
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

			// Check hosts.toml exists
			hostsPath := path.Join(stageDir, "hosts.toml")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("hosts.toml not found at %s", hostsPath)
			}

			// Check cluster config exists
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

func genStandaloneConfig(ctx *runner.StepContext, yasbootPath, clusterName, user, password, installPath, dataPath, logPath string, beginPort int) error {
	stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
	memoryPercent := ctx.GetParamInt("db_memory_percent", 50)

	// Get target IP (assuming single target)
	// In real implementation, get from context
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

	// Build gen-config command (execute as yashan user); password escaped for su -c '...'
	genCmd := fmt.Sprintf(`cd %s && %s package se gen --cluster %s --recommend-param \
-u %s -p %s --ip %s --port %d \
--install-path %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--memory-limit %d \
--node 1`,
		stageDir, yasbootPath, clusterName,
		user, commonos.ShellEscapeForSuC(password), ip, ctx.GetParamInt("ssh_port", 22),
		installPath, dataPath, logPath,
		beginPort, memoryPercent)

	extra := ctx.GetParamString("yasboot_extra_args", "")
	genCmd = commonos.YasbootAppendExtraArgs(genCmd, extra, true)
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot package se gen: appending extra args: %s", strings.TrimSpace(extra))
	}

	// Execute as yashan user
	cmd := fmt.Sprintf("su - %s -c '%s'", user, genCmd)

	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
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

	// Parse diskgroups (yasboot uses --system-data and --data with comma-separated disk paths only)
	systemdg, _ := parseYACDiskGroup(systemdgStr)
	datadg, _ := parseYACDiskGroup(datadgStr)

	// Get target IPs and node count: YAC uses all nodes from params; standalone uses current host
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

	// yasboot package ce gen: --system-data and --data are comma-separated disk paths; no arch in gen
	systemDisks := formatDiskList(systemdg)
	dataDisks := formatDiskList(datadg)
	diskFoundPath := ctx.GetParamString("yac_disk_found_path", "/dev/mapper/")

	// Build gen-config command (execute as yashan user)
	var genCmd string
	if accessMode == "scan" {
		scanName := ctx.GetParamString("yac_scanname", "")
		genCmd = fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
-u %s -p %s --ip %s --port %d \
-i %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d \
--inter-cidr %s \
--public-network %s \
--scanname %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			stageDir, yasbootPath, clusterName,
			user, commonos.ShellEscapeForSuC(password), ips, ctx.GetParamInt("ssh_port", 22),
			installPath, dataPath, logPath,
			beginPort, nodeCount,
			interCIDR, publicNetwork, scanName,
			diskFoundPath,
			systemDisks, dataDisks)
	} else {
		vips := ctx.GetParamStringSlice("yac_vips")
		// yasboot expects VIP in format 'ip/netmask[/interface]', e.g. 10.10.10.127/24
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
--node %d \
--inter-cidr %s \
--public-network %s \
--vips %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			stageDir, yasbootPath, clusterName,
			user, commonos.ShellEscapeForSuC(password), ips, ctx.GetParamInt("ssh_port", 22),
			installPath, dataPath, logPath,
			beginPort, nodeCount,
			interCIDR, publicNetwork, vipStr,
			diskFoundPath,
			systemDisks, dataDisks)
	}

	extra := ctx.GetParamString("yasboot_extra_args", "")
	genCmd = commonos.YasbootAppendExtraArgs(genCmd, extra, true)
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot package ce gen: appending extra args: %s", strings.TrimSpace(extra))
	}

	// Execute as yashan user
	cmd := fmt.Sprintf("su - %s -c '%s'", user, genCmd)

	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
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
