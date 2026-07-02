package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
	ossteps "github.com/yinstall/internal/steps/os"
)

var (
	// DB 通用参数
	dbClusterName            string
	dbPort                   int
	dbMemoryPercent          int
	dbCharacterSet           string
	dbUseNativeType          bool
	dbMode                   string
	dbSysPassword            string
	dbInstallPath            string
	dbDataPath               string
	dbLogPath                string
	dbStageDir               string
	dbPackage                string
	dbDepsPackage            string
	dbNodes                  int
	dbRedoFileNum            int    // REDO 文件个数
	dbRedoFileSize           string // REDO 文件大小
	dbDisableArchivelog      bool   // 关闭归档：将 yashandb.toml 中 ISARCHIVELOG 设为 false
	dbCustomSQLScript        string // 自定义 SQL 脚本路径
	dbTPCC                   bool   // TPCC 参数优化
	dbUnifiedAudit           bool   // 统一审计与清理策略
	dbSpfileParams           string // 自定义 SPFILE 参数 name=value|...
	dbYasbootGenExtraArgs    string // 追加到 yasboot package se/ce gen 的额外参数
	dbYasbootDeployExtraArgs string // 追加到 yasboot cluster deploy 的额外参数

	// 多租户（CDB / PDB）
	dbEnablePluggable bool
	dbPDBSpecs        []string

	// 是否跳过 OS 基线配置
	dbSkipOS bool

	// YAC 网络参数
	yacInterCIDR     string
	yacPublicNetwork string
	yacAccessMode    string
	yacVIPs          []string
	yacScanName      string
	yacDiskFoundPath string

	// YAC db skip-os 下 C-001 内 udev 磁盘发现
	yacAutoDiscoverDisks      bool
	yacDiscoverRoot           string
	yacDiscoverFallbackMapper bool
	yacEnsureOSPassword       bool

	// YAC YFS 调优参数
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
	// skip-os 参数
	dbCmd.Flags().BoolVar(&dbSkipOS, "skip-os", false, "Skip OS baseline preparation")

	// OS 参数（与 yinstall os 共用变量，--skip-os=false 时参与 OS 基线步骤）
	registerAllOSFlags(dbCmd, registerOSFlagsConfig{forDB: true})
	registerYACModeFlag(dbCmd)

	// DB 通用参数（flag 注册）
	dbCmd.Flags().StringVar(&dbClusterName, "db-cluster-name", "yashandb", "Cluster name")
	dbCmd.Flags().IntVar(&dbPort, "db-port", 1688, "Database begin port (yasboot --begin-port)")
	dbCmd.Flags().IntVar(&dbMemoryPercent, "db-memory-percent", 50, "Memory percentage (0-100)")
	dbCmd.Flags().StringVar(&dbCharacterSet, "db-character-set", "utf8", "Character set: UTF8, GBK, ASCII, GB18030, BINARY, LATIN1, UTF8MB3, UTF8MB4 (case-insensitive)")
	dbCmd.Flags().BoolVar(&dbUseNativeType, "db-use-native-type", false, "Set USE_NATIVE_TYPE in cluster TOML (native column types when true) (default: false)")
	dbCmd.Flags().StringVar(&dbMode, "db-mode", "", "Standalone only: empty (default) or mysql (passes --mode mysql to yasboot package se gen; not supported for YAC/ce gen)")
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
	dbCmd.Flags().BoolVar(&dbUnifiedAudit, "db-unified-audit", false, "Enable unified auditing, audit policies, and purge jobs (default: false)")
	dbCmd.Flags().StringVar(&dbSpfileParams, "db-spfile-params", "", "Custom SPFILE parameters as name=value|name=value (empty=skip C-026; values may include quotes, e.g. date_format='yyyy-mm-dd hh24:mi:ss')")
	dbCmd.Flags().StringVar(&dbYasbootGenExtraArgs, "yasboot-gen-extra-args", "", "Extra arguments appended to yasboot package se gen / package ce gen (space-separated)")
	dbCmd.Flags().StringVar(&dbYasbootDeployExtraArgs, "yasboot-deploy-extra-args", "", "Extra arguments appended to yasboot cluster deploy (space-separated)")
	dbCmd.Flags().BoolVar(&dbEnablePluggable, "db-enable-pluggable", false, "Deploy as CDB (multitenant); passes --enable-pluggable-database to yasboot package se/ce gen (mutually exclusive with --db-mode mysql)")
	dbCmd.Flags().StringSliceVar(&dbPDBSpecs, "db-pdb", nil, "PDB to create after install (repeatable). Bare name or key=value. Short keys: name,user,password,datafile,size,file_convert,compat,archivelog,open. Official aliases: admin_user,admin_password,tablespace_datafile,tablespace_size,compat_mode,file_name_convert,file_convert_from,file_convert_to")

	// YAC 网络参数
	dbCmd.Flags().StringVar(&yacInterCIDR, "yac-inter-cidr", "", "YAC inter-connect CIDR (required for YAC)")
	dbCmd.Flags().StringVar(&yacPublicNetwork, "yac-public-network", "", "YAC public network CIDR or interface (required for YAC)")
	dbCmd.Flags().StringVar(&yacAccessMode, "yac-access-mode", "vip", "YAC access mode (vip/scan/direct; direct skips VIP)")
	dbCmd.Flags().StringSliceVar(&yacVIPs, "yac-vips", nil, "VIP addresses for YAC (vip/scan mode; auto-generated if omitted)")
	dbCmd.Flags().StringVar(&yacScanName, "yac-scanname", "", "SCAN name for YAC (dns:name for DNS mode, name or empty for local mode)")
	dbCmd.Flags().StringVar(&yacDiskFoundPath, "yac-disk-found-path", "/dev/yfs/", "Disk found path for yasboot package ce gen")
	dbCmd.Flags().BoolVar(&yacAutoDiscoverDisks, "yac-auto-discover-disks", false,
		"Auto-discover YAC disk groups from /dev/yfs when --skip-os (default: true if --skip-os is set)")
	dbCmd.Flags().StringVar(&yacDiscoverRoot, "yac-discover-root", "/dev/yfs",
		"Root directory for C-001 udev disk discovery when --skip-os (default: /dev/yfs)")
	dbCmd.Flags().BoolVar(&yacDiscoverFallbackMapper, "yac-discover-fallback-mapper", true,
		"When /dev/yfs is empty, discover sys*/data* under /dev/mapper")
	dbCmd.Flags().BoolVar(&yacEnsureOSPassword, "yac-ensure-os-password", true,
		"YAC: verify product user SSH password before ce gen; reset to --os-user-password on mismatch when login user has root/sudo (default: true)")

	// YAC YFS 调优参数
	dbCmd.Flags().BoolVar(&yacYFSTuneEnable, "yac-yfs-tune", false, "Enable YFS tuning")
	dbCmd.Flags().StringVar(&yacYFSAuSize, "yac-yfs-au-size", "32M", "YFS allocation unit size")
	dbCmd.Flags().StringVar(&yacRedoFileSize, "yac-redo-file-size", "128", "Redo file size in MB (default: 128, unit: MB)")
	dbCmd.Flags().IntVar(&yacRedoFileNum, "yac-redo-file-num", 6, "Number of redo files")
	dbCmd.Flags().StringVar(&yacShmPoolSize, "yac-shm-pool-size", "2G", "Shared memory pool size")
	dbCmd.Flags().IntVar(&yacMaxInstances, "yac-max-instances", 64, "Maximum instances")

}

func runDB(cmd *cobra.Command, args []string) error {
	if err := validatePorts(map[string]int{
		"--db-port": dbPort,
	}); err != nil {
		return err
	}
	if err := validateMemoryPercent("--db-memory-percent", dbMemoryPercent); err != nil {
		return err
	}

	applyInstallArchiveDefault(cmd)
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintDBStepCatalog(dbSkipOS)
		return nil
	}

	if dbMode != "" {
		normalized := strings.ToLower(strings.TrimSpace(dbMode))
		if normalized != "mysql" {
			return fmt.Errorf("invalid --db-mode: '%s'. Valid values are: '' (default) or 'mysql' (case-insensitive)", dbMode)
		}
		dbMode = normalized
	}

	// 未指定 --targets 时，默认本地执行。
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}

	ResolveOSUserPassword(cmd, flags, osUser, &osUserPassword)

	// 未显式指定的 stage/home/data/log 按 --os-user 与 --db-port 推导（避免 tpcc 用户仍用 /home/yashan/...）。
	applyDBUserPathDefaults(cmd)

	// 判定 YAC 模式
	isYACMode := yacMode || len(flags.Targets) >= 2
	yacAccessMode = dbsteps.NormalizeYACAccessMode(yacAccessMode)

	if isYACMode {
		if err := dbsteps.ValidateYACAccessMode(yacAccessMode); err != nil {
			return err
		}
	}

	if isYACMode && dbMode == "mysql" {
		return fmt.Errorf("--db-mode mysql is not supported for YAC cluster installation (yasboot package ce gen does not accept --mode mysql)")
	}

	if dbEnablePluggable && dbMode == "mysql" {
		return fmt.Errorf("--db-enable-pluggable is mutually exclusive with --db-mode mysql (use compat=mysql on --db-pdb instead)")
	}

	if len(dbPDBSpecs) > 0 {
		dbEnablePluggable = true
		if _, err := dbsteps.ParsePDBSpecs(dbPDBSpecs); err != nil {
			return fmt.Errorf("invalid --db-pdb: %w", err)
		}
	}
	if dbEnablePluggable && strings.TrimSpace(dbPackage) != "" {
		if err := dbsteps.ValidateMultitenantDBPackage(dbPackage); err != nil {
			return err
		}
	}

	// 校验必填参数
	if dbSysPassword == "" && !flags.DryRun && !flags.Precheck {
		return fmt.Errorf("--db-sys-password is required for database creation")
	}
	// 远程模式下 yasboot gen-config 需要以产品用户 SSH 到 targets；
	// 本地模式（未指定 --targets）不要求 os-user-password。
	if !flags.Local && osUserPassword == "" && !flags.DryRun && !flags.Precheck {
		return fmt.Errorf("--os-user-password is required for yasboot gen-config (SSH password of product user)")
	}

	// --skip-os 时默认开启 C-001 内 udev 磁盘发现（除非用户显式改过 flag）
	if dbSkipOS && !cmd.Flags().Changed("yac-auto-discover-disks") {
		yacAutoDiscoverDisks = true
	}

	// YAC 专属校验
	if isYACMode {
		if yacSystemDG == "" || yacDataDG == "" {
			if dbSkipOS && !yacAutoDiscoverDisks {
				return fmt.Errorf("--yac-systemdg and --yac-datadg are required for YAC mode when --skip-os is set\n" +
					"  Hint: enable --yac-auto-discover-disks (default with --skip-os) after /dev/yfs is ready,\n" +
					"        or run without --skip-os to use OS step B-021 auto discovery,\n" +
					"        or pass --yac-systemdg and --yac-datadg explicitly")
			}
			// --skip-os=false：OS 步骤中的 B-021 会自动发现磁盘；skip-os + auto-discover：C-001
		}
		// SCAN 模式的 scanname 解析在下方构建 params 之后进行
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("db-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting DB installation (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)
	logger.Info("Product user: %s (group: %s, uid: %d)", osUser, osGroup, osUserUID)
	if !cmd.Flags().Changed("os-user-password") && strings.TrimSpace(flags.SSHUser) == strings.TrimSpace(osUser) && strings.TrimSpace(flags.SSHPassword) != "" && !strings.EqualFold(strings.TrimSpace(flags.SSHAuth), "key") {
		logger.Info("Product user password: aligned with --ssh-password (SSH login user matches --os-user)")
	}
	logger.Info("Paths: stage=%s home=%s data=%s log=%s", dbStageDir, dbInstallPath, dbDataPath, dbLogPath)
	if dbMode == "" {
		logger.Info("DB mode: (empty)")
	} else {
		logger.Info("DB mode: %s", dbMode)
	}
	if dbEnablePluggable {
		logger.Info("Multitenant: CDB enabled (--enable-pluggable-database)")
		if len(dbPDBSpecs) > 0 {
			logger.Info("PDB specs: %d", len(dbPDBSpecs))
		}
	}

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

	// 构建 params
	params := buildDBParams(isYACMode, len(flags.Targets))
	params["sudo"] = flags.UseSudo
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

	// 获取全部 steps
	var allSteps []*runner.Step

	// 若不跳过 OS，则加入完整 OS steps
	if !dbSkipOS {
		osSteps := ossteps.GetAllSteps()
		allSteps = append(allSteps, osSteps...)
	} else {
		// 即使跳过 OS，仍需要连通性检查（B-001）
		osSteps := ossteps.GetAllSteps()
		for _, step := range osSteps {
			if step.ID == "B-001" {
				allSteps = append(allSteps, step)
				break
			}
		}
	}

	// 加入 DB steps
	dbSteps := dbsteps.GetAllSteps()
	allSteps = append(allSteps, dbSteps...)

	// 按全局过滤规则筛选 steps
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

	// 进度：分母 = 非 Optional 安装步 +（--archive 时）非 Optional 且跳过 R-001 的 collect 步
	plannedProgress := runner.CountNonOptionalSteps(steps)
	if flags.ArchiveOnSuccess && !flags.DryRun && !flags.Precheck {
		plannedProgress += CountArchiveCollectSteps("db", isYACMode, flags)
	}
	progress := runner.NewStepProgress(plannedProgress)
	totalSteps := progress.Total()

	if connectivityStep != nil {
		logger.Info("======== Phase 1: Connectivity check ========")
		precheckFailed := false
		var connProgIdx, connProgTot int
		connProgFrozen := false
		for ti, target := range flags.Targets {
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
				Progress:          progress,
			}
			if progress != nil && ti > 0 && connProgFrozen {
				ctx.Progress = nil
				ctx.StepIndex = connProgIdx
				ctx.TotalSteps = connProgTot
			}

			result := runner.RunStep(connectivityStep, ctx)
			if progress != nil && !connProgFrozen {
				connProgIdx, connProgTot = ctx.StepIndex, ctx.TotalSteps
				connProgFrozen = true
			}
			if !result.Success && !result.Skipped {
				executor.Close()
				if flags.Precheck {
					precheckFailed = true
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
		if flags.Precheck && precheckFailed {
			// 继续执行其它 precheck 以收集全部问题，但最终仍以非零退出码结束。
			logger.Error("Connectivity precheck has failures; continuing to collect all issues.")
		}
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

	// 构建 hostExecs 供 C-001 全局预检查使用
	hostExecs := make([]dbsteps.HostExec, 0, len(hostInfos))
	for _, info := range hostInfos {
		hostExecs = append(hostExecs, dbsteps.HostExec{Host: info.Host, Executor: &c001ExecAdapter{e: &runnerExecAdapter{e: info.Executor}}})
	}

	// C-001 作为全局预检只运行一次（连通性 + YAC 网段/密码/磁盘发现等，合并为单一步骤 ID）。
	var stepsToRun []*runner.Step
	if len(otherSteps) > 0 && stepsContainID(otherSteps, "C-001") {
		(&runner.StepContext{
			Logger:        logger,
			Params:        params,
			CurrentStepID: "C-001",
		}).LogPhase("plan", fmt.Sprintf("global-precheck hosts=%d yac=%v", len(hostExecs), isYACMode))
		if err := dbsteps.RunC001FullPrecheck(hostExecs, params, logger, isYACMode, dbSkipOS, flags.Precheck, flags.DryRun); err != nil {
			if flags.Precheck {
				pc := &runner.StepContext{Logger: logger, Params: params, Results: make(map[string]interface{}), Precheck: flags.Precheck}
				pc.ReportPrecheckIssue(runner.PrecheckIssue{
					StepID:   "C-001",
					StepName: "Connectivity and YAC precheck",
					Severity: runner.PrecheckSeverityError,
					Code:     "PC.DB.C001",
					Message:  err.Error(),
				})
			} else {
				for _, info := range hostInfos {
					info.Executor.Close()
				}
				return fmt.Errorf("C-001 precheck failed: %w", err)
			}
		} else {
			(&runner.StepContext{Logger: logger, CurrentStepID: "C-001"}).LogPhase("op-done", "global-connectivity-precheck")
			logger.Info("C-001: global precheck completed (placeholder step C-001 is not repeated in the numbered list below)")
		}
		stepsToRun = removeFirstStepWithID(otherSteps, "C-001")
	} else {
		stepsToRun = otherSteps
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
	precheckFailed := false

	// OS 步骤：使用共享的 RunPerHostSteps 处理 Global/PerHost 步骤
	if len(osStepsToRun) > 0 {
		osResult := RunPerHostStepsEx(osStepsToRun, hostInfos, params, flags, logger, 0, totalSteps, nil, nil, progress)
		if osResult.PrecheckFailed {
			precheckFailed = true
		}
		if osResult.LastError != nil {
			lastErr = osResult.LastError
		}
	}

	var dbInstallResults map[string]interface{}

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
			Progress:          progress,
		}

		for _, step := range dbStepsToRun {
			result := runner.RunStep(step, ctx)
			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
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
		dbInstallResults = ctx.Results
	}

	if lastErr != nil {
		logger.Error("DB installation completed with errors")
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return lastErr
	}
	if flags.Precheck && precheckFailed {
		logger.Error("Precheck completed with failures")
		return fmt.Errorf("precheck failed")
	}

	installResults := map[string]interface{}{}
	if dbInstallResults != nil {
		for k, v := range dbInstallResults {
			installResults[k] = v
		}
	}
	if cn, ok := params["db_cluster_name"].(string); ok && cn != "" {
		installResults["cluster_name"] = cn
	}
	executedIDs := make([]string, 0, len(stepsToRun))
	for _, s := range stepsToRun {
		executedIDs = append(executedIDs, s.ID)
	}
	if !dbSkipOS {
		for _, s := range osStepsToRun {
			executedIDs = append(executedIDs, s.ID)
		}
	}
	installSnap := buildInstallParamsSnapshot("db", rid, params, executedIDs)
	runInstallArchiveCollect("db", isYACMode, progress, hostInfos, installSnap, installResults, flags, logger)

	logger.Info("DB installation completed successfully")
	return nil
}

func buildDBParams(isYACMode bool, targetCount int) map[string]interface{} {
	// 先继承 OS 侧参数（buildOSParams）
	params := buildOSParams(isYACMode, targetCount)

	// 兜底：若仍残留 yashan 硬编码默认路径，按当前 os_user 重写（兼容未走 runDB 的调用路径）。
	user := dbProductUser(osUser)
	if dbStageDir == "/home/yashan/install" || (dbPort != dbDefaultBeginPort && dbStageDir == fmt.Sprintf("/home/yashan/install_%d", dbPort)) {
		dbStageDir = defaultDBStageDir(user, dbPort)
	}
	if dbPort != dbDefaultBeginPort {
		if dbInstallPath == "/data/yashan/yasdb_home" {
			dbInstallPath = defaultDBInstallPath(user, dbPort)
		}
		if dbDataPath == "/data/yashan/yasdb_data" {
			dbDataPath = defaultDBDataPath(user, dbPort)
		}
		if dbLogPath == "/data/yashan/log" {
			dbLogPath = defaultDBLogPath(user, dbPort)
		}
	}

	// 追加 DB 专用参数
	params["db_cluster_name"] = dbClusterName
	params["db_begin_port"] = dbPort
	params["db_memory_percent"] = dbMemoryPercent
	params["db_character_set"] = dbCharacterSet
	params["db_use_native_type"] = dbUseNativeType
	params["db_mode"] = dbMode
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
	params["db_unified_audit"] = dbUnifiedAudit
	params["db_spfile_params"] = dbSpfileParams
	params[dbsteps.ParamYasbootGenExtraArgs] = dbYasbootGenExtraArgs
	params[dbsteps.ParamYasbootDeployExtraArgs] = dbYasbootDeployExtraArgs
	params["db_enable_pluggable"] = dbEnablePluggable
	params["db_pdb_specs"] = dbPDBSpecs

	// YAC 网络相关参数
	params["yac_inter_cidr"] = yacInterCIDR
	params["yac_public_network"] = yacPublicNetwork
	params["yac_access_mode"] = yacAccessMode
	params["yac_vips"] = yacVIPs
	params["yac_scanname"] = yacScanName
	params["yac_scan_ips"] = yacScanIPs
	params["yac_disk_found_path"] = yacDiskFoundPath
	params["yac_auto_discover_disks"] = yacAutoDiscoverDisks
	params["yac_discover_root"] = yacDiscoverRoot
	params["yac_discover_fallback_mapper"] = yacDiscoverFallbackMapper
	params["yac_ensure_os_password"] = yacEnsureOSPassword

	// YAC YFS 相关参数
	params["yac_yfs_tune_enable"] = yacYFSTuneEnable
	params["yac_yfs_au_size"] = yacYFSAuSize
	params["yac_redo_file_size"] = yacRedoFileSize
	params["yac_redo_file_num"] = yacRedoFileNum
	params["yac_shm_pool_size"] = yacShmPoolSize
	params["yac_max_instances"] = yacMaxInstances

	// DB 安装合并 OS steps 后，sysctl 尺寸始终来自 --db-memory-percent，而非 standalone 的 max-RAM-only 模式。
	params["os_sysctl_shm_use_max_ram_only"] = false

	return params
}

func stepsContainID(steps []*runner.Step, id string) bool {
	for _, s := range steps {
		if s != nil && s.ID == id {
			return true
		}
	}
	return false
}

// removeFirstStepWithID 删除列表中第一个指定 ID 的步骤（全局 C-001 已覆盖其预检逻辑时避免重复执行占位步骤）。
func removeFirstStepWithID(steps []*runner.Step, id string) []*runner.Step {
	out := make([]*runner.Step, 0, len(steps))
	removed := false
	for _, s := range steps {
		if !removed && s != nil && s.ID == id {
			removed = true
			continue
		}
		out = append(out, s)
	}
	return out
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
