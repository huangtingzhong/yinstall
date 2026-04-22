package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
	ossteps "github.com/yinstall/internal/steps/os"
)

var (
	// DB common parameters
	dbClusterName   string
	dbPort int
	dbMemoryPercent int
	dbCharacterSet  string
	dbUseNativeType bool
	dbSysPassword   string
	dbInstallPath   string
	dbDataPath      string
	dbLogPath       string
	dbStageDir      string
	dbPackage       string
	dbDepsPackage   string
	dbNodes         int
	dbRedoFileNum   int    // REDO 文件个数
	dbRedoFileSize  string // REDO 文件大小
	dbDisableArchivelog bool // 关闭归档：将 yashandb.toml 中 ISARCHIVELOG 设为 false
	dbCustomSQLScript string // 自定义 SQL 脚本路径
	dbTPCC          bool   // TPCC 参数优化
	dbYasbootExtraArgs string // 追加到 yasboot package se/ce gen 等命令的额外参数

	// OS user parameters for DB (needed for gen-config)
	dbOSUser         string
	dbOSUserPassword string
	dbOSGroup        string

	// Skip OS configuration
	dbSkipOS              bool
	dbIgnoreInstallErrors bool

	// OS baseline parameters (only effective when --skip-os=false)
	dbOSTimezone        string
	dbOSNTPServer       string
	dbOSYumMode         string
	dbOSISODevice       string
	dbOSISOMountpoint   string
	dbOSYumRepoFile     string
	dbOSDepsPkgs        string
	dbOSToolsPkgs       string
	dbOSFirewallMode    string
	dbOSFirewallPorts   string
	dbOSHugepagesEnable bool

	// YAC network parameters
	yacInterCIDR     string
	yacPublicNetwork string
	yacAccessMode    string
	yacVIPs          []string
	yacScanName      string
	yacDiskFoundPath string

	// YAC YFS tuning parameters
	yacYFSTuneEnable bool
	yacYFSAuSize     string
	yacRedoFileSize  string
	yacRedoFileNum   int
	yacShmPoolSize   string
	yacMaxInstances  int
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Install YashanDB database",
	Long: `Install YashanDB database (standalone or YAC mode):
  - OS baseline preparation (optional, can be skipped)
  - Create directories
  - Extract installation package
  - Generate configuration files
  - Install software
  - Create database
  - Configure environment variables
  - Verify installation`,
	RunE:         runDB,
	SilenceUsage: true, // 报错时不显示帮助信息
}

func init() {
	// Skip OS parameter
	dbCmd.Flags().BoolVar(&dbSkipOS, "skip-os", false, "Skip OS baseline preparation")

	// OS user parameters (needed for gen-config and installation)
	dbCmd.Flags().StringVar(&dbOSUser, "os-user", "yashan", "Product user name")
	dbCmd.Flags().StringVar(&dbOSUserPassword, "os-user-password", defaultOSUserPassword, "Product user SSH password (for yasboot, yashan default)")
	dbCmd.Flags().StringVar(&dbOSGroup, "os-group", "yashan", "Primary group name")

	// OS baseline parameters (only effective when --skip-os=false)
	dbCmd.Flags().BoolVar(&dbIgnoreInstallErrors, "os-ignore-install-errors", false, "[OS] Ignore package installation errors and continue (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSTimezone, "os-timezone", "Asia/Shanghai", "[OS] System timezone (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSNTPServer, "os-ntp-server", "ntp.aliyun.com", "[OS] NTP server address (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSYumMode, "os-yum-mode", "none", "[OS] YUM mode: online/local-iso/none (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSISODevice, "os-iso-device", "/dev/cdrom", "[OS] ISO file path/name or block device used when --os-yum-mode=local-iso (auto-searched if filename only)")
	dbCmd.Flags().StringVar(&dbOSISOMountpoint, "os-iso-mountpoint", "/media", "[OS] Mount point for ISO when --os-yum-mode=local-iso")
	dbCmd.Flags().StringVar(&dbOSYumRepoFile, "os-yum-repo-file", "/etc/yum.repos.d/local.repo", "[OS] YUM repo file path for local-iso mode")
	dbCmd.Flags().StringVar(&dbOSDepsPkgs, "os-deps-db-packages", "libzstd zlib lz4 openssl openssl-devel libnsl libaio", "[OS] DB dependency packages (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSToolsPkgs, "os-deps-tools-packages", "", "[OS] Common tools packages (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSFirewallMode, "os-firewall-mode", "disable", "[OS] Firewall mode: keep/disable/open-ports (only effective when --skip-os=false)")
	dbCmd.Flags().StringVar(&dbOSFirewallPorts, "os-firewall-ports", "", "[OS] Ports to open, comma-separated (only effective when --skip-os=false)")
	dbCmd.Flags().BoolVar(&dbOSHugepagesEnable, "os-hugepages-enable", false, "[OS] Enable huge pages configuration (only effective when --skip-os=false)")

	// DB common parameters
	dbCmd.Flags().StringVar(&dbClusterName, "db-cluster-name", "yashandb", "Cluster name")
	dbCmd.Flags().IntVar(&dbPort, "db-port", 1688, "Database begin port (yasboot --begin-port)")
	dbCmd.Flags().IntVar(&dbMemoryPercent, "db-memory-percent", 50, "Memory percentage (0-100)")
	dbCmd.Flags().StringVar(&dbCharacterSet, "db-character-set", "utf8", "Character set: UTF8, GBK, ASCII, GB18030, BINARY, LATIN1, UTF8MB3, UTF8MB4 (case-insensitive)")
	dbCmd.Flags().BoolVar(&dbUseNativeType, "db-use-native-type", false, "Set USE_NATIVE_TYPE in cluster TOML (native column types when true) (default: false)")
	dbCmd.Flags().StringVar(&dbSysPassword, "db-sys-password", "Yashan1!", "Database SYS password")
	dbCmd.Flags().StringVar(&dbInstallPath, "db-home-path", "/data/yashan/yasdb_home", "Software installation path (auto-appends _<port> for non-default ports, e.g., yasdb_home_2688)")
	dbCmd.Flags().StringVar(&dbDataPath, "db-data-path", "/data/yashan/yasdb_data", "Data directory path (auto-appends _<port> for non-default ports, e.g., yasdb_data_2688)")
	dbCmd.Flags().StringVar(&dbLogPath, "db-log-path", "/data/yashan/log", "Log directory path (auto-appends _<port> for non-default ports, e.g., log_2688)")
	dbCmd.Flags().StringVar(&dbStageDir, "db-stage-dir", "/home/yashan/install", "Stage directory for extraction (auto-appends _<port> for non-default ports, e.g., install_2688)")
	dbCmd.Flags().StringVar(&dbPackage, "db-package", "", "DB installation package path")
	dbCmd.Flags().StringVar(&dbDepsPackage, "db-deps-package", "", "SSL deps package path (optional)")
	dbCmd.Flags().IntVar(&dbNodes, "db-nodes", 0, "Number of nodes (auto-detected from targets)")
	dbCmd.Flags().IntVar(&dbRedoFileNum, "db-redo-file-num", 6, "REDO file number (default: 6)")
	dbCmd.Flags().StringVar(&dbRedoFileSize, "db-redo-file-size", "128", "REDO file size in MB (default: 128, unit: MB)")
	dbCmd.Flags().BoolVar(&dbDisableArchivelog, "db-disable-archivelog", false, "Disable archive log: set ISARCHIVELOG = false in yashandb.toml (default yasboot keeps archive log on)")
	dbCmd.Flags().StringVar(&dbCustomSQLScript, "db-custom-sql-script", "", "Custom SQL script to execute after installation (supports: remote:/path, local:/path, /absolute/path, relative/path)")
	dbCmd.Flags().BoolVar(&dbTPCC, "db-tpcc", false, "Enable TPCC parameter optimization (default: false)")
	dbCmd.Flags().MarkHidden("db-tpcc")
	dbCmd.Flags().StringVar(&dbYasbootExtraArgs, "yasboot-extra-args", "", "Extra arguments appended to yasboot package se gen / package ce gen (space-separated, e.g. '--disk-found-path /dev/foo')")

	// YAC diskgroup parameters (shared with os command)
	dbCmd.Flags().StringVar(&yacSystemDG, "yac-systemdg", "", "System diskgroup (format: dgname:/dev/sda,/dev/sdb, required for YAC)")
	dbCmd.Flags().StringVar(&yacDataDG, "yac-datadg", "", "Data diskgroup (format: dgname:/dev/sdc,/dev/sdd, required for YAC)")
	dbCmd.Flags().StringVar(&yacArchDG, "yac-archdg", "", "Archive diskgroup (format: dgname:/dev/sde, optional)")
	dbCmd.Flags().BoolVar(&yacArchDGEnable, "yac-archdg-enable", false, "Enable independent ArchDG creation (separate archive diskgroup)")

	// YAC network parameters
	dbCmd.Flags().StringVar(&yacInterCIDR, "yac-inter-cidr", "", "YAC inter-connect CIDR (required for YAC)")
	dbCmd.Flags().StringVar(&yacPublicNetwork, "yac-public-network", "", "YAC public network CIDR or interface (required for YAC)")
	dbCmd.Flags().StringVar(&yacAccessMode, "yac-access-mode", "vip", "YAC access mode (vip/scan)")
	dbCmd.Flags().StringSliceVar(&yacVIPs, "yac-vips", nil, "VIP addresses for YAC (required for vip mode)")
	dbCmd.Flags().StringVar(&yacScanName, "yac-scanname", "", "SCAN name for YAC (dns:name for DNS mode, name or empty for local mode)")
	dbCmd.Flags().StringVar(&yacScanIPs, "yac-scan-ips", "", "SCAN IP addresses for local SCAN mode (comma-separated, empty=auto-allocate)")
	dbCmd.Flags().StringVar(&yacDiskFoundPath, "yac-disk-found-path", "/dev/yfs/", "Disk found path for yasboot package ce gen")

	// YAC auto-discovery parameters (effective when --skip-os=false)
	dbCmd.Flags().StringVar(&yacDiskPattern, "yac-disk-pattern", "", "[OS] Disk path pattern for filtering (e.g., '/dev/sd[c-z]', empty=all disks)")
	dbCmd.Flags().StringVar(&yacExcludeDisks, "yac-exclude-disks", "/dev/sda,/dev/sdb", "[OS] Disks to exclude from auto-discovery (comma-separated)")
	dbCmd.Flags().StringVar(&yacSystemdgSizeMax, "yac-systemdg-size-max", "10G", "[OS] Max size threshold for systemdg classification")
	dbCmd.Flags().BoolVar(&yacAutoConfirm, "yac-auto-confirm", false, "[OS] Skip user confirmation for auto-discovered disks")

	// YAC YFS tuning parameters
	dbCmd.Flags().BoolVar(&yacYFSTuneEnable, "yac-yfs-tune", false, "Enable YFS tuning")
	dbCmd.Flags().StringVar(&yacYFSAuSize, "yac-yfs-au-size", "32M", "YFS allocation unit size")
	dbCmd.Flags().StringVar(&yacRedoFileSize, "yac-redo-file-size", "128", "Redo file size in MB (default: 128, unit: MB)")
	dbCmd.Flags().IntVar(&yacRedoFileNum, "yac-redo-file-num", 6, "Number of redo files")
	dbCmd.Flags().StringVar(&yacShmPoolSize, "yac-shm-pool-size", "2G", "Shared memory pool size")
	dbCmd.Flags().IntVar(&yacMaxInstances, "yac-max-instances", 64, "Maximum instances")
}

const defaultOSUserPassword = "aaBB11@@33$$"

func runDB(cmd *cobra.Command, args []string) error {
	if err := validatePorts(map[string]int{
		"--db-port": dbPort,
	}); err != nil {
		return err
	}
	if err := validateMemoryPercent("--db-memory-percent", dbMemoryPercent); err != nil {
		return err
	}

	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintDBStepCatalog(dbSkipOS)
		return nil
	}

	if dbOSUserPassword == "" {
		dbOSUserPassword = defaultOSUserPassword
	}

	// If --targets is not specified, default to local execution.
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}

	// When port is not 1688, if user did not explicitly set home/data/log/cluster-name,
	// use port-suffixed defaults to avoid conflicting with default instance (yasdb_home_<port>, etc.).
	if dbPort != 1688 {
		if !cmd.Flags().Changed("db-home-path") {
			dbInstallPath = fmt.Sprintf("/data/yashan/yasdb_home_%d", dbPort)
		}
		if !cmd.Flags().Changed("db-data-path") {
			dbDataPath = fmt.Sprintf("/data/yashan/yasdb_data_%d", dbPort)
		}
		if !cmd.Flags().Changed("db-log-path") {
			dbLogPath = fmt.Sprintf("/data/yashan/log_%d", dbPort)
		}
		if !cmd.Flags().Changed("db-cluster-name") {
			dbClusterName = fmt.Sprintf("yashandb_%d", dbPort)
		}
	}

	// Determine YAC mode
	isYACMode := yacMode || len(flags.Targets) >= 2

	// Validate required parameters
	if dbSysPassword == "" && !flags.DryRun && !flags.Precheck {
		return fmt.Errorf("--db-sys-password is required for database creation")
	}
	// In remote mode, yasboot gen-config needs to SSH into targets as product user.
	// In local mode (no --targets specified), we don't require os-user-password.
	if !flags.Local && dbOSUserPassword == "" && !flags.DryRun && !flags.Precheck {
		return fmt.Errorf("--os-user-password is required for yasboot gen-config (SSH password of product user)")
	}

	// YAC specific validation
	if isYACMode {
		if yacSystemDG == "" || yacDataDG == "" {
			if dbSkipOS {
				return fmt.Errorf("--yac-systemdg and --yac-datadg are required for YAC mode when --skip-os is set\n" +
					"  Hint: run without --skip-os to enable auto disk discovery (B-021),\n" +
					"        or run 'yinstall os' first to discover disks, then 'yinstall db --skip-os' with discovered disk groups")
			}
			// --skip-os=false: B-021 will auto-discover disks during OS steps
		}
		// SCAN mode scanname parsing is done below after params are built
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("db-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting DB installation (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)

	if isYACMode {
		logger.Info("Mode: YAC (%d nodes)", len(flags.Targets))
	} else {
		logger.Info("Mode: Standalone")
	}

	if dbSkipOS {
		logger.Info("OS baseline preparation: SKIPPED")
	} else {
		logger.Info("OS baseline preparation: ENABLED")
	}

	// Build parameters
	params := buildDBParams(isYACMode, len(flags.Targets))
	params["target_ips"] = flags.Targets
	params["ssh_port"] = flags.SSHPort
	params["yasboot_ssh_port"] = flags.YasbootSSHPort

	if isYACMode && yacAccessMode == "scan" {
		if yacScanName == "" {
			params["yac_scan_mode"] = "local"
			params["yac_scanname"] = dbClusterName + "-scan"
		} else if strings.HasPrefix(yacScanName, "dns:") {
			params["yac_scan_mode"] = "dns"
			params["yac_scanname"] = strings.TrimPrefix(yacScanName, "dns:")
		} else {
			params["yac_scan_mode"] = "local"
			params["yac_scanname"] = yacScanName
		}
	}

	// Get all steps
	var allSteps []*runner.Step

	// Add OS steps if not skipped
	if !dbSkipOS {
		osSteps := ossteps.GetAllSteps()
		allSteps = append(allSteps, osSteps...)
	} else {
		// Even when skipping OS, still need connectivity check (B-001)
		osSteps := ossteps.GetAllSteps()
		for _, step := range osSteps {
			if step.ID == "B-001" {
				allSteps = append(allSteps, step)
				break
			}
		}
	}

	// Add DB steps
	dbSteps := dbsteps.GetAllSteps()
	allSteps = append(allSteps, dbSteps...)

	// Filter steps
	steps := filterSteps(allSteps, flags)

	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}

	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	// Phase 1: Connectivity check
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

	// Track step index for console output
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

	// Phase 2: Execute steps
	if len(otherSteps) > 0 {
		logger.Info("======== Phase 2: Executing steps ========")
	}

	// 构建 hostExecs 供 C-001、C-009-VIP、C-013-SCAN 等全局预检查使用
	hostExecs := make([]dbsteps.HostExec, 0, len(hostInfos))
	for _, info := range hostInfos {
		hostExecs = append(hostExecs, dbsteps.HostExec{Host: info.Host, Executor: &c001ExecAdapter{e: &runnerExecAdapter{e: info.Executor}}})
	}

	// C-001 runs once as global precheck (network + YAC UID/GID + shared disks on all nodes)
	var stepsToRun []*runner.Step
	if len(otherSteps) > 0 && otherSteps[0].ID == "C-001" {
		if err := dbsteps.RunConnectivityAndYACPrecheck(hostExecs, params, logger, isYACMode); err != nil {
			for _, info := range hostInfos {
				info.Executor.Close()
			}
			return fmt.Errorf("C-001 precheck failed: %w", err)
		}
		stepsToRun = otherSteps[1:]
	} else {
		stepsToRun = otherSteps
	}

	// C-001A: Network CIDR validation and auto-detection (before VIP/SCAN)
	if isYACMode {
		if err := dbsteps.RunNetworkValidation(hostExecs, params, logger); err != nil {
			for _, info := range hostInfos {
				info.Executor.Close()
			}
			return fmt.Errorf("C-001A network validation failed: %w", err)
		}
	}

	// C-009-VIP runs once when YAC mode（与步骤 C-009 VIP 占位对应）
	if isYACMode {
		if err := dbsteps.RunVIPValidationOrAutoGenerate(hostExecs, params, logger); err != nil {
			for _, info := range hostInfos {
				info.Executor.Close()
			}
			return fmt.Errorf("C-009-VIP VIP check failed: %w", err)
		}
	}

	// C-013-SCAN runs once when YAC scan mode（与步骤 C-013 SCAN 名解析对应）
	if isYACMode && yacAccessMode == "scan" {
		scanMode, _ := params["yac_scan_mode"].(string)
		if scanMode == "local" {
			if err := dbsteps.RunScanIPAllocation(hostExecs, params, logger); err != nil {
				for _, info := range hostInfos {
					info.Executor.Close()
				}
				return fmt.Errorf("C-013-SCAN local SCAN IP allocation failed: %w", err)
			}
		} else {
			if err := dbsteps.RunScanNameResolveAndSubnetCheck(hostExecs, params, logger); err != nil {
				for _, info := range hostInfos {
					info.Executor.Close()
				}
				return fmt.Errorf("C-013-SCAN SCAN name check failed: %w", err)
			}
		}
	}

	// 分离 OS 步骤和 DB 步骤
	var osStepsToRun []*runner.Step
	var dbStepsToRun []*runner.Step
	for _, step := range stepsToRun {
		if strings.HasPrefix(step.ID, "B-") {
			osStepsToRun = append(osStepsToRun, step)
		} else {
			dbStepsToRun = append(dbStepsToRun, step)
		}
	}

	defer func() {
		for _, info := range hostInfos {
			info.Executor.Close()
		}
	}()

	var lastErr error

	// OS 步骤：分离 Global 步骤和逐主机步骤
	if len(osStepsToRun) > 0 {
		// 构建 TargetHosts（供 Global 步骤使用）
		targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
		for _, info := range hostInfos {
			targetHosts = append(targetHosts, runner.TargetHost{
				Host:     info.Host,
				Executor: &runnerExecAdapter{e: info.Executor},
			})
		}

		var globalOSSteps []*runner.Step
		var perHostOSSteps []*runner.Step
		for _, step := range osStepsToRun {
			if step.Global {
				globalOSSteps = append(globalOSSteps, step)
			} else {
				perHostOSSteps = append(perHostOSSteps, step)
			}
		}

		// 执行 Global OS 步骤（跨节点，仅执行一次）
		if len(globalOSSteps) > 0 {
			logger.Info("-------- Global OS steps (all nodes) --------")
			globalResults := make(map[string]interface{})
			for i, step := range globalOSSteps {
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
					lastErr = result.Error
					break
				}
			}
			stepIndex += len(globalOSSteps)
		}

		// 执行逐主机 OS 步骤
		if lastErr == nil && len(perHostOSSteps) > 0 {
			for _, info := range hostInfos {
				logger.Info("-------- Host: %s --------", info.Host)

				hostResults := make(map[string]interface{})

				for i, step := range perHostOSSteps {
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
						lastErr = result.Error
						break
					}
				}

				if lastErr != nil {
					break
				}
			}
			stepIndex += len(perHostOSSteps)
		}
	}

	// DB 步骤：使用 TargetHosts 方式（步骤内部自行决定在哪些节点执行）
	if lastErr == nil && len(dbStepsToRun) > 0 {
		// 构建多节点上下文：Executor 为第一个节点（首节点步骤用），TargetHosts 为全部节点
		targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
		for _, info := range hostInfos {
			targetHosts = append(targetHosts, runner.TargetHost{
				Host:     info.Host,
				Executor: &runnerExecAdapter{e: info.Executor},
			})
		}
		firstInfo := hostInfos[0]
		ctx := &runner.StepContext{
			Executor:          &runnerExecAdapter{e: firstInfo.Executor},
			Logger:            logger,
			Params:            params,
			DryRun:            flags.DryRun,
			Precheck:          flags.Precheck,
			Results:           make(map[string]interface{}),
			OSInfo:            firstInfo.OSInfo,
			LocalSoftwareDirs: flags.LocalSoftwareDirs,
			RemoteSoftwareDir: flags.RemoteSoftwareDir,
			ForceAll:          flags.ForceAll,
				ForceSteps:        flags.ForceSteps,
				ForceDeleteUser:   flags.ForceDeleteUser,
			TargetHosts:       targetHosts,
		}

		for i, step := range dbStepsToRun {
			ctx.StepIndex = stepIndex + i
			ctx.TotalSteps = totalSteps
			result := runner.RunStep(step, ctx)
			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			if !result.Success && !result.Skipped {
				logger.Error("Step %s failed: %v", step.ID, result.Error)
				lastErr = result.Error
				break
			}
		}
	}

	if lastErr != nil {
		logger.Error("DB installation completed with errors")
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return lastErr
	}

	logger.Info("DB installation completed successfully")
	return nil
}

func buildDBParams(isYACMode bool, targetCount int) map[string]interface{} {
	// Start with OS params
	params := buildOSParams(isYACMode, targetCount)

	// Auto-adjust paths based on port number (if not default 1688)
	// For non-default ports, add _<port> suffix to avoid conflicts
	if dbPort != 1688 {
		portSuffix := fmt.Sprintf("_%d", dbPort)

		// Only auto-adjust if user hasn't explicitly set these paths
		// Check by comparing with default values
		if dbInstallPath == "/data/yashan/yasdb_home" {
			dbInstallPath = dbInstallPath + portSuffix
		}
		if dbDataPath == "/data/yashan/yasdb_data" {
			dbDataPath = dbDataPath + portSuffix
		}
		if dbLogPath == "/data/yashan/log" {
			dbLogPath = dbLogPath + portSuffix
		}
		if dbStageDir == "/home/yashan/install" {
			dbStageDir = dbStageDir + portSuffix
		}
	}

	// Override OS user params with DB-specific values if provided
	if dbOSUser != "" {
		params["os_user"] = dbOSUser
	}
	if dbOSUserPassword != "" {
		params["os_user_password"] = dbOSUserPassword
	}
	if dbOSGroup != "" {
		params["os_group"] = dbOSGroup
	}

	// Override OS ignore install errors if specified in DB command
	params["os_ignore_install_errors"] = dbIgnoreInstallErrors

	// Override OS baseline parameters if specified in DB command
	if dbOSTimezone != "" {
		params["os_timezone"] = dbOSTimezone
	}
	if dbOSNTPServer != "" {
		params["os_ntp_server"] = dbOSNTPServer
	}
	if dbOSYumMode != "" {
		params["os_yum_mode"] = dbOSYumMode
	}
	if dbOSISODevice != "" {
		params["os_iso_device"] = dbOSISODevice
	}
	if dbOSISOMountpoint != "" {
		params["os_iso_mountpoint"] = dbOSISOMountpoint
	}
	if dbOSYumRepoFile != "" {
		params["os_yum_repo_file"] = dbOSYumRepoFile
	}
	if dbOSDepsPkgs != "" {
		params["os_deps_db_packages"] = dbOSDepsPkgs
	}
	if dbOSToolsPkgs != "" {
		params["os_deps_tools_packages"] = dbOSToolsPkgs
	}
	if dbOSFirewallMode != "" {
		params["os_firewall_mode"] = dbOSFirewallMode
	}
	if dbOSFirewallPorts != "" {
		params["os_firewall_ports"] = dbOSFirewallPorts
	}
	params["os_hugepages_enable"] = dbOSHugepagesEnable

	// Add DB specific params
	params["db_cluster_name"] = dbClusterName
	params["db_begin_port"] = dbPort
	params["db_memory_percent"] = dbMemoryPercent
	params["db_character_set"] = dbCharacterSet
	params["db_use_native_type"] = dbUseNativeType
	params["db_admin_password"] = dbSysPassword
	params["db_install_path"] = dbInstallPath
	params["db_data_path"] = dbDataPath
	params["db_log_path"] = dbLogPath
	params["db_stage_dir"] = dbStageDir
	params["db_package"] = dbPackage
	params["db_deps_package"] = dbDepsPackage
	params["db_nodes"] = dbNodes
	params["db_skip_os"] = dbSkipOS
	params["db_redo_file_num"] = dbRedoFileNum
	params["db_redo_file_size"] = dbRedoFileSize
	params["db_disable_archivelog"] = dbDisableArchivelog
	params["db_custom_sql_script"] = dbCustomSQLScript
	params["db_tpcc"] = dbTPCC
	params["yasboot_extra_args"] = dbYasbootExtraArgs

	// YAC network params
	params["yac_inter_cidr"] = yacInterCIDR
	params["yac_public_network"] = yacPublicNetwork
	params["yac_access_mode"] = yacAccessMode
	params["yac_vips"] = yacVIPs
	params["yac_scanname"] = yacScanName
	params["yac_scan_ips"] = yacScanIPs
	params["yac_disk_found_path"] = yacDiskFoundPath

	// YAC YFS params
	params["yac_yfs_tune_enable"] = yacYFSTuneEnable
	params["yac_yfs_au_size"] = yacYFSAuSize
	params["yac_redo_file_size"] = yacRedoFileSize
	params["yac_redo_file_num"] = yacRedoFileNum
	params["yac_shm_pool_size"] = yacShmPoolSize
	params["yac_max_instances"] = yacMaxInstances

	// DB install always sizes sysctl from --db-memory-percent (merged OS steps), not standalone max-RAM mode.
	params["os_sysctl_shm_use_max_ram_only"] = false

	return params
}

// c001ExecAdapter 将 runner.Executor 适配为 dbsteps.ExecutorForC001，供 C-001 预检查调用（db 包不直接依赖 ssh）
type c001ExecAdapter struct {
	e runner.Executor
}

func (a *c001ExecAdapter) Execute(cmd string, sudo bool) (dbsteps.ExecResultForC001, error) {
	return a.e.Execute(cmd, sudo)
}

func (a *c001ExecAdapter) Host() string {
	return a.e.Host()
}
