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
	yacMode bool // --yac：手动启用 YAC（targets>=2 时亦自动启用）
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
	registerAllOSFlags(osCmd, registerOSFlagsConfig{forDB: false})
}

// HostInfo 保存主机信息。
type HostInfo struct {
	Host     string
	Executor ssh.Executor
	OSInfo   *runner.OSInfo
	// 连通性步骤（B-001/S-01/R-001）PreCheck 写入 Results 的快照，供后续归档步骤使用。
	Hostname    string
	CPUCores    string
	MemoryTotal string
}

func runOS(cmd *cobra.Command, args []string) error {
	applyInstallArchiveDefault(cmd)
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

	ResolveOSUserPassword(cmd, flags, osUser, &osUserPassword)

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
	params["sudo"] = flags.UseSudo
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

	// 拆出连通步与其它步骤
	var connectivityStep *runner.Step
	var otherSteps []*runner.Step
	for _, step := range steps {
		if step.ID == "B-001" {
			connectivityStep = step
		} else {
			otherSteps = append(otherSteps, step)
		}
	}
	plannedProgress := runner.CountNonOptionalSteps(steps)
	if flags.ArchiveOnSuccess && !flags.DryRun && !flags.Precheck {
		plannedProgress += CountArchiveCollectSteps("os", isYACMode, flags)
	}
	progress := runner.NewStepProgress(plannedProgress)
	totalSteps := progress.Total()

	// Phase 1：连通性检查
	connResult, err := RunConnectivityPhase(connectivityStep, flags.Targets, flags, params, logger, 0, totalSteps, progress)
	if err != nil {
		return err
	}
	hostInfos := connResult.HostInfos

	defer func() {
		for _, info := range hostInfos {
			info.Executor.Close()
		}
	}()

	// Phase 2：Global + 逐主机步骤
	phaseResult := RunPerHostStepsEx(otherSteps, hostInfos, params, flags, logger, 0, totalSteps, nil, nil, progress)

	if phaseResult.LastError != nil {
		logger.Error("OS preparation completed with errors")
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return phaseResult.LastError
	}
	if flags.Precheck && (connResult.PrecheckFailed || phaseResult.PrecheckFailed) {
		return fmt.Errorf("precheck failed")
	}

	installSnap := buildInstallParamsSnapshot("os", rid, params, collectStepIDs(steps))
	runInstallArchiveCollect("os", isYACMode, progress, hostInfos, installSnap, nil, flags, logger)

	logger.Info("OS preparation completed successfully")
	return nil
}

// buildOSYumISOParams 返回 YUM/ISO 相关 ctx.Params 条目（os / db / stressos 共用）。
func buildOSYumISOParams() map[string]interface{} {
	return map[string]interface{}{
		"os_yum_mode":       osYumMode,
		"os_iso_device":     osISODevice,
		"os_iso_mountpoint": osISOMountpoint,
		"os_yum_repo_file":  osYumRepoFile,
	}
}

func buildOSParams(isYACMode bool, targetCount int) map[string]interface{} {
	params := map[string]interface{}{
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
	for k, v := range buildOSYumISOParams() {
		params[k] = v
	}
	return params
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

func (a *runnerExecAdapter) Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error {
	return a.e.Upload(localPath, remotePath, uploadCtx)
}
