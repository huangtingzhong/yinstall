package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	ossteps "github.com/yinstall/internal/steps/os"
)

var (
	// OS 子命令参数
	osUser          string
	osUserUID       int
	osGroup         string
	osGroupGID      int
	osDBAGroup      string
	osDBAGroupGID   int
	osUserShell     string
	osUserPassword  string
	osSudoersEnable bool

	osTimezone  string
	osNTPServer string

	osSysctlFile       string
	osLimitsFile       string
	osKernelArgsEnable bool
	osKernelArgs       string

	// Hugepages 参数
	osHugepagesEnable bool

	// 单机 OS 下：若设置则让 sysctl 的 shmmax/shmall 与 DB memory percent 对齐；-1 表示不写（按 90%% 内存估算）
	osDbMemoryPercent int

	osYumMode             string
	osISODevice           string
	osISOMountpoint       string
	osYumRepoFile         string
	osDepsPkgs            string
	osToolsPkgs           string
	osIgnoreInstallErrors bool
	osZstdSourceTarball   string

	osFirewallMode  string
	osFirewallPorts string

	yacMultipathEnable   bool
	yacMultipathPkgs     string
	yacMultipathConf     string
	yacMultipathAutoWWID bool
	yacUdevRulesFile     string
	yacUdevOwner         string
	yacUdevGroup         string
	yacUdevMode          string

	// 本地磁盘参数
	osLocalDisks []string
	osLocalVG    string
	osLocalLV    string
	osLocalMount string

	// YAC diskgroup 参数
	yacSystemDG     string // 格式：dgname:disk1,disk2,...
	yacDataDG       string // 格式：dgname:disk1,disk2,...
	yacArchDG       string // 格式：dgname:disk1,disk2,...（可选，默认跟随 datadg）
	yacArchDGEnable bool   // 是否启用独立 ArchDG 创建

	// YAC SCAN 参数
	yacScanIPs string // local SCAN 模式下逗号分隔的 SCAN IP

	// YAC 自动发现磁盘参数
	yacDiskPattern     string // 过滤磁盘路径的模式（例如 "/dev/sd[c-z]"）
	yacExcludeDisks    string // 排除磁盘列表，逗号分隔（默认 "/dev/sda,/dev/sdb"）
	yacSystemdgSizeMax string // systemdg 分类的最大容量阈值（默认 "10G"）
	yacAutoConfirm     bool   // 自动发现磁盘后跳过人工确认

	// YAC 模式开关（targets>=2 时也会自动启用）
	yacMode bool // 手动启用 YAC 模式（targets>=2 时自动启用）
)

var osCmd = &cobra.Command{
	Use:   "os",
	Short: "Execute OS baseline preparation",
	Long: `Execute OS baseline preparation steps including:
  - Check host connectivity
  - Create product user and groups
  - Configure timezone and NTP
  - Set kernel parameters (sysctl)
  - Configure resource limits
  - Install dependencies
  - Configure firewall
  - (Optional) Configure multipath and udev for YAC`,
	RunE:         runOS,
	SilenceUsage: true, // 报错时不显示帮助信息
}

func init() {
	// OS 用户/组参数
	osCmd.Flags().StringVar(&osUser, "os-user", "yashan", "Product user name")
	osCmd.Flags().IntVar(&osUserUID, "os-user-uid", 701, "User UID")
	osCmd.Flags().StringVar(&osGroup, "os-group", "yashan", "Primary group name")
	osCmd.Flags().IntVar(&osGroupGID, "os-group-gid", 701, "Primary group GID")
	osCmd.Flags().StringVar(&osDBAGroup, "os-dba-group", "YASDBA", "DBA group name")
	osCmd.Flags().IntVar(&osDBAGroupGID, "os-dba-group-gid", 702, "DBA group GID")
	osCmd.Flags().StringVar(&osUserShell, "os-user-shell", "/bin/bash", "User shell")
	osCmd.Flags().StringVar(&osUserPassword, "os-user-password", "aaBB11@@33$$", "User password (yashan default)")
	osCmd.Flags().BoolVar(&osSudoersEnable, "os-sudoers-enable", true, "Enable sudoers configuration")

	// 时区/时间参数
	osCmd.Flags().StringVar(&osTimezone, "os-timezone", "Asia/Shanghai", "System timezone")
	osCmd.Flags().StringVar(&osNTPServer, "os-ntp-server", "", "NTP server address (empty to skip NTP configuration)")

	// 内核参数
	osCmd.Flags().StringVar(&osSysctlFile, "os-sysctl-file", "/etc/sysctl.d/yashandb.conf", "Sysctl config file path")
	osCmd.Flags().StringVar(&osLimitsFile, "os-limits-file", "/etc/security/limits.conf", "Limits config file path")
	osCmd.Flags().BoolVar(&osKernelArgsEnable, "os-kernel-args-enable", true, "Enable kernel args configuration")
	osCmd.Flags().StringVar(&osKernelArgs, "os-kernel-args", "transparent_hugepage=never elevator=deadline LANG=en_US.UTF-8", "Kernel boot arguments")

	// Hugepages 参数
	osCmd.Flags().BoolVar(&osHugepagesEnable, "os-hugepages-enable", false, "Enable huge pages configuration (memory size based on db-memory-percent)")

	// 与 db install 同名：填 1-100 则按 MemTotal 估算 shmmax/shmall；standalone os 省略时使用 90%% RAM 作为 shmmax，且 hugepages 使用 db_memory_percent=90。
	osCmd.Flags().IntVar(&osDbMemoryPercent, "db-memory-percent", -1, "Planned DB memory percent (1-100) for shared memory sizing; omit on standalone os to use 90%% physical RAM")

	// YUM/repo 参数
	osCmd.Flags().StringVar(&osYumMode, "os-yum-mode", "none", "YUM mode (online/local-iso/none)")
	osCmd.Flags().StringVar(&osISODevice, "os-iso-device", "/dev/cdrom", "ISO device or file path")
	osCmd.Flags().StringVar(&osISOMountpoint, "os-iso-mountpoint", "/media", "ISO mount point")
	osCmd.Flags().StringVar(&osYumRepoFile, "os-yum-repo-file", "/etc/yum.repos.d/local.repo", "Local repo file path")
	osCmd.Flags().StringVar(&osDepsPkgs, "os-deps-db-packages", "libzstd zlib lz4 openssl openssl-devel libaio", "DB dependency packages")
	osCmd.Flags().StringVar(&osToolsPkgs, "os-deps-tools-packages", "zip bind-utils sysstat telnet iotop openssh-clients net-tools unzip libvncserver tigervnc-server device-mapper-multipath dstat lsof psmisc redhat-lsb-core parted xhost strace showmount expect tcl sysfsutils gdisk rsync lvm2 qperf chrony tmux bpftrace perf", "Common tools packages (empty to skip)")
	osCmd.Flags().BoolVar(&osIgnoreInstallErrors, "os-ignore-install-errors", false, "Ignore package installation errors and continue (only show warnings)")
	osCmd.Flags().StringVar(&osZstdSourceTarball, "os-zstd-source-tarball", "", "Explicit zstd source tarball path or filename (zstd-x.y.z.tar.gz); empty=auto-discover under local/remote software dirs (EL7 libzstd fallback)")

	// 防火墙参数
	osCmd.Flags().StringVar(&osFirewallMode, "os-firewall-mode", "disable", "Firewall mode (keep/disable/open-ports)")
	osCmd.Flags().StringVar(&osFirewallPorts, "os-firewall-ports", "", "Ports to open (comma-separated)")

	// YAC multipath 参数
	osCmd.Flags().BoolVar(&yacMultipathEnable, "yac-multipath-enable", false, "Enable multipath configuration")
	osCmd.Flags().StringVar(&yacMultipathPkgs, "yac-multipath-packages", "device-mapper-multipath", "Multipath packages")
	osCmd.Flags().StringVar(&yacMultipathConf, "yac-multipath-conf", "/etc/multipath.conf", "Multipath config file")
	osCmd.Flags().BoolVar(&yacMultipathAutoWWID, "yac-multipath-auto-wwid", false, "Auto collect WWID")
	osCmd.Flags().StringVar(&yacUdevRulesFile, "yac-udev-rules-file", "/etc/udev/rules.d/99-yashandb-permissions.rules", "Udev rules file")
	osCmd.Flags().StringVar(&yacUdevOwner, "yac-udev-owner", "yashan", "Disk owner")
	osCmd.Flags().StringVar(&yacUdevGroup, "yac-udev-group", "YASDBA", "Disk group")
	osCmd.Flags().StringVar(&yacUdevMode, "yac-udev-mode", "0666", "Disk mode")

	// 本地磁盘参数
	osCmd.Flags().StringSliceVar(&osLocalDisks, "os-local-disk", nil, "Local disks for data directory (e.g., /dev/sdb,/dev/sdc)")
	osCmd.Flags().StringVar(&osLocalVG, "os-local-vg", "yasvg", "Volume group name")
	osCmd.Flags().StringVar(&osLocalLV, "os-local-lv", "yaslv", "Logical volume name")
	osCmd.Flags().StringVar(&osLocalMount, "os-local-mount", "/data", "Mount point for data directory")

	// YAC diskgroup 参数
	osCmd.Flags().StringVar(&yacSystemDG, "yac-systemdg", "", "System diskgroup (format: dgname:/dev/sda,/dev/sdb)")
	osCmd.Flags().StringVar(&yacDataDG, "yac-datadg", "", "Data diskgroup (format: dgname:/dev/sdc,/dev/sdd)")
	osCmd.Flags().StringVar(&yacArchDG, "yac-archdg", "", "Archive diskgroup (format: dgname:/dev/sde, optional, defaults to datadg)")
	osCmd.Flags().BoolVar(&yacArchDGEnable, "yac-archdg-enable", false, "Enable independent ArchDG creation (separate archive diskgroup)")
	osCmd.Flags().StringVar(&yacScanIPs, "yac-scan-ips", "", "SCAN IP addresses for local SCAN mode (comma-separated, empty=auto-allocate)")

	// YAC 自动发现磁盘参数
	osCmd.Flags().StringVar(&yacDiskPattern, "yac-disk-pattern", "", "Disk path pattern for filtering (e.g., '/dev/sd[c-z]', empty=all disks)")
	osCmd.Flags().StringVar(&yacExcludeDisks, "yac-exclude-disks", "/dev/sda,/dev/sdb", "Disks to exclude from auto-discovery (comma-separated)")
	osCmd.Flags().StringVar(&yacSystemdgSizeMax, "yac-systemdg-size-max", "10G", "Max size threshold for systemdg classification")
	osCmd.Flags().BoolVar(&yacAutoConfirm, "yac-auto-confirm", false, "Skip user confirmation for auto-discovered disks")

	// YAC 模式开关
	osCmd.Flags().BoolVar(&yacMode, "yac-mode", false, "Enable YAC mode (auto-enabled when targets >= 2)")
}

// HostInfo 保存主机信息。
type HostInfo struct {
	Host     string
	Executor ssh.Executor
	OSInfo   *runner.OSInfo
}

func runOS(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintOSStepCatalog()
		return nil
	}

	// 未指定 --targets 时，默认本地执行。
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}

	// 本地模式下，除非用户显式指定，否则不注入默认的 os-user-password，
	// 避免在 local 执行时出现不必要的“登录凭据”参数。
	if flags.Local && !cmd.Flags().Changed("os-user-password") {
		osUserPassword = ""
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("os-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting OS preparation (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)

	// 判定 YAC 模式：targets >= 2 时自动启用，或由参数手动启用
	isYACMode := yacMode || len(flags.Targets) >= 2
	if isYACMode {
		logger.Info("YAC mode: enabled (%d hosts)", len(flags.Targets))
	} else {
		logger.Info("Standalone mode: single host")
	}

	params := buildOSParams(isYACMode, len(flags.Targets))
	params["ssh_port"] = flags.SSHPort
	params["yasboot_ssh_port"] = flags.YasbootSSHPort

	if cmd.Flags().Changed("db-memory-percent") {
		if err := validateMemoryPercent("--db-memory-percent", osDbMemoryPercent); err != nil {
			return err
		}
		params["os_sysctl_shm_use_max_ram_only"] = false
		params["db_memory_percent"] = osDbMemoryPercent
	} else {
		params["os_sysctl_shm_use_max_ram_only"] = true
		params["db_memory_percent"] = 90
	}

	allSteps := ossteps.GetAllSteps()
	steps := filterSteps(allSteps, flags)

	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}

	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	// 阶段 1：连通性检查
	var hostInfos []*HostInfo
	var connectivityStep *runner.Step
	var otherSteps []*runner.Step

	for _, step := range steps {
		if step.ID == "B-001" {
			connectivityStep = step
		} else {
			otherSteps = append(otherSteps, step)
		}
	}

	// 维护 step index（用于终端输出）
	stepIndex := 0
	totalSteps := len(steps)

	if connectivityStep != nil {
		logger.Info("======== Phase 1: Connectivity check ========")
		for _, target := range flags.Targets {
			executor, err := createExecutor(target, flags, logger, "")
			if err != nil {
				logger.Error("Failed to connect to %s: %v", target, err)
				return fmt.Errorf("connectivity check failed for %s: %w", target, err)
			}

			ctx := &runner.StepContext{
				Executor:          &runnerExecAdapter{e: executor},
				Logger:            logger,
				Params:            params,
				DryRun:            flags.DryRun,
				Precheck:          flags.Precheck,
				Results:           make(map[string]interface{}),
				LocalSoftwareDirs: flags.LocalSoftwareDirs,
				RemoteSoftwareDir: flags.RemoteSoftwareDir,
				ForceAll:          flags.ForceAll,
				ForceSteps:        flags.ForceSteps,
				ForceDeleteUser:   flags.ForceDeleteUser,
				StepIndex:         stepIndex,
				TotalSteps:        totalSteps,
			}

			result := runner.RunStep(connectivityStep, ctx)
			if !result.Success && !result.Skipped {
				executor.Close()
				if flags.Precheck {
					// precheck 模式下继续收集其它主机的问题
					continue
				}
				return fmt.Errorf("connectivity check failed for %s: %w", target, result.Error)
			}

			hostInfos = append(hostInfos, &HostInfo{
				Host:     target,
				Executor: executor,
				OSInfo:   ctx.OSInfo,
			})
		}
		stepIndex++
	} else {
		for _, target := range flags.Targets {
			executor, err := createExecutor(target, flags, logger, "")
			if err != nil {
				return fmt.Errorf("failed to connect to %s: %w", target, err)
			}
			hostInfos = append(hostInfos, &HostInfo{Host: target, Executor: executor})
		}
	}

	// 阶段 2：执行 steps
	if len(otherSteps) > 0 {
		logger.Info("======== Phase 2: Executing steps ========")
	}

	// 构建 TargetHosts（供 Global 步骤使用）
	targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
	for _, info := range hostInfos {
		targetHosts = append(targetHosts, runner.TargetHost{
			Host:     info.Host,
			Executor: &runnerExecAdapter{e: info.Executor},
		})
	}

	// 分离 Global 步骤和普通步骤，保持原始顺序
	// Global 步骤在逐主机循环之前执行一次（带 TargetHosts）
	var globalSteps []*runner.Step
	var perHostSteps []*runner.Step
	for _, step := range otherSteps {
		if step.Global {
			globalSteps = append(globalSteps, step)
		} else {
			perHostSteps = append(perHostSteps, step)
		}
	}

	var lastErr error
	precheckFailed := false

	// 执行 Global 步骤（跨节点，仅执行一次）
	if len(globalSteps) > 0 {
		logger.Info("-------- Global steps (all nodes) --------")
		globalResults := make(map[string]interface{})
		for i, step := range globalSteps {
			ctx := &runner.StepContext{
				Executor:          &runnerExecAdapter{e: hostInfos[0].Executor},
				Logger:            logger,
				Params:            params,
				DryRun:            flags.DryRun,
				Precheck:          flags.Precheck,
				Results:           globalResults,
				OSInfo:            hostInfos[0].OSInfo,
				LocalSoftwareDirs: flags.LocalSoftwareDirs,
				RemoteSoftwareDir: flags.RemoteSoftwareDir,
				ForceAll:          flags.ForceAll,
				ForceSteps:        flags.ForceSteps,
				ForceDeleteUser:   flags.ForceDeleteUser,
				StepIndex:         stepIndex + i,
				TotalSteps:        totalSteps,
				TargetHosts:       targetHosts,
			}

			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				logger.Error("Step %s failed: %v", step.ID, result.Error)
				if flags.Precheck {
					precheckFailed = true
					continue
				}
				lastErr = result.Error
				break
			}
		}
		stepIndex += len(globalSteps)
	}

	// 执行逐主机步骤
	if lastErr == nil {
		for _, info := range hostInfos {
			logger.Info("-------- Host: %s --------", info.Host)

			hostResults := make(map[string]interface{})

			for i, step := range perHostSteps {
				ctx := &runner.StepContext{
					Executor:          &runnerExecAdapter{e: info.Executor},
					Logger:            logger,
					Params:            params,
					DryRun:            flags.DryRun,
					Precheck:          flags.Precheck,
					Results:           hostResults,
					OSInfo:            info.OSInfo,
					LocalSoftwareDirs: flags.LocalSoftwareDirs,
					RemoteSoftwareDir: flags.RemoteSoftwareDir,
					ForceAll:          flags.ForceAll,
					ForceSteps:        flags.ForceSteps,
					ForceDeleteUser:   flags.ForceDeleteUser,
					StepIndex:         stepIndex + i,
					TotalSteps:        totalSteps,
				}

				result := runner.RunStep(step, ctx)
				if !result.Success && !result.Skipped {
					logger.Error("Step %s failed: %v", step.ID, result.Error)
					if flags.Precheck {
						precheckFailed = true
						continue
					}
					lastErr = result.Error
					break
				}
			}

			if lastErr != nil {
				break
			}
		}
	}

	for _, info := range hostInfos {
		info.Executor.Close()
	}

	if lastErr != nil {
		logger.Error("OS preparation completed with errors")
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return lastErr
	}
	if flags.Precheck && precheckFailed {
		return fmt.Errorf("precheck failed")
	}

	logger.Info("OS preparation completed successfully")
	return nil
}

func buildOSParams(isYACMode bool, targetCount int) map[string]interface{} {
	return map[string]interface{}{
		"os_user":                  osUser,
		"os_user_uid":              osUserUID,
		"os_group":                 osGroup,
		"os_group_gid":             osGroupGID,
		"os_dba_group":             osDBAGroup,
		"os_dba_group_gid":         osDBAGroupGID,
		"os_user_shell":            osUserShell,
		"os_user_password":         osUserPassword,
		"os_sudoers_enable":        osSudoersEnable,
		"os_timezone":              osTimezone,
		"os_ntp_server":            osNTPServer,
		"os_sysctl_file":           osSysctlFile,
		"os_limits_file":           osLimitsFile,
		"os_kernel_args_enable":    osKernelArgsEnable,
		"os_kernel_args":           osKernelArgs,
		"os_hugepages_enable":      osHugepagesEnable,
		"os_yum_mode":              osYumMode,
		"os_iso_device":            osISODevice,
		"os_iso_mountpoint":        osISOMountpoint,
		"os_yum_repo_file":         osYumRepoFile,
		"os_deps_db_packages":      osDepsPkgs,
		"os_deps_tools_packages":   osToolsPkgs,
		"os_ignore_install_errors": osIgnoreInstallErrors,
		"os_zstd_source_tarball":   osZstdSourceTarball,
		"os_firewall_mode":         osFirewallMode,
		"os_firewall_ports":        osFirewallPorts,
		"yac_mode":                 isYACMode,
		"yac_target_count":         targetCount,
		"yac_multipath_enable":     yacMultipathEnable,
		"yac_multipath_packages":   yacMultipathPkgs,
		"yac_multipath_conf":       yacMultipathConf,
		"yac_multipath_auto_wwid":  yacMultipathAutoWWID,
		"yac_udev_rules_file":      yacUdevRulesFile,
		"yac_udev_owner":           yacUdevOwner,
		"yac_udev_group":           yacUdevGroup,
		"yac_udev_mode":            yacUdevMode,
		"os_local_disks":           osLocalDisks,
		"os_local_vg":              osLocalVG,
		"os_local_lv":              osLocalLV,
		"os_local_mount":           osLocalMount,
		"yac_systemdg":             yacSystemDG,
		"yac_datadg":               yacDataDG,
		"yac_archdg":               yacArchDG,
		"yac_archdg_enable":        yacArchDGEnable,
		"yac_scan_ips":             yacScanIPs,
		"yac_disk_pattern":         yacDiskPattern,
		"yac_exclude_disks":        yacExcludeDisks,
		"yac_systemdg_size_max":    yacSystemdgSizeMax,
		"yac_auto_confirm":         yacAutoConfirm,
	}
}

const (
	sshConnectMaxRetries = 3
	sshConnectRetryDelay = 5 * time.Second
)

func createExecutor(target string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	cfg := ssh.Config{
		Host:       target,
		Port:       flags.SSHPort,
		User:       flags.SSHUser,
		AuthMethod: flags.SSHAuth,
		Password:   flags.SSHPassword,
		KeyPath:    flags.SSHKeyPath,
		Logger:     logger,
		StepID:     stepID,
	}

	if flags.Local {
		cfg.AuthMethod = "local"
		return ssh.NewExecutor(cfg)
	}

	// 带重试的 SSH 连接：网络波动或目标端 sshd 未就绪时自动重试
	var (
		executor ssh.Executor
		lastErr  error
	)
	for attempt := 1; attempt <= sshConnectMaxRetries; attempt++ {
		if flags.SSHPassword == "" {
			executor, lastErr = ssh.NewExecutorWithFallback(cfg, defaultSSHPassword())
		} else {
			executor, lastErr = ssh.NewExecutor(cfg)
		}
		if lastErr == nil {
			return executor, nil
		}
		if attempt < sshConnectMaxRetries {
			if logger != nil {
				logger.Warn("SSH connection attempt %d/%d failed for %s: %v, retrying in %v...",
					attempt, sshConnectMaxRetries, target, lastErr, sshConnectRetryDelay)
			}
			time.Sleep(sshConnectRetryDelay)
		}
	}
	return nil, fmt.Errorf("failed to connect to %s after %d attempts: %w", target, sshConnectMaxRetries, lastErr)
}

// defaultSSHPassword 返回默认SSH密码
func defaultSSHPassword() string {
	// 可以从环境变量或配置文件读取默认密码
	// 这里暂时返回空字符串，表示不使用默认密码
	return ""
}

// runnerExecAdapter 将 ssh.Executor 适配为 runner.Executor，供 StepContext 使用（runner 仅依赖接口，实现来自 ssh/executor.go）
type runnerExecAdapter struct {
	e ssh.Executor
}

func (a *runnerExecAdapter) Execute(cmd string, sudo bool) (runner.ExecResult, error) {
	return a.e.Execute(cmd, sudo)
}

func (a *runnerExecAdapter) Host() string {
	return a.e.Host()
}

func (a *runnerExecAdapter) Close() error {
	return a.e.Close()
}

func (a *runnerExecAdapter) Upload(localPath, remotePath string) error {
	return a.e.Upload(localPath, remotePath)
}
