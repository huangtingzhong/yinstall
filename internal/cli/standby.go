// standby.go - 添加备库命令实现
// 本文件实现 yinstall standby 命令，用于在已有主库基础上新增备库节点

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	ossteps "github.com/yinstall/internal/steps/os"
	standbysteps "github.com/yinstall/internal/steps/standby"
)

var (
	// 主库连接参数
	primaryIP          string // 主库 IP 地址
	primarySSHUser     string // 主库 SSH 用户名
	primarySSHPassword string // 主库 SSH 密码
	primarySSHKey      string // 主库 SSH 私钥路径

	// 主库数据库用户和环境变量参数
	primaryOSUser  string // 主库运行 yashan 的用户，默认 yashan
	primaryEnvFile string // 主库环境变量文件路径，默认 .bashrc（相对用户家目录）或自动检测

	// 操作系统配置控制
	skipOS                    bool // 是否跳过备库操作系统配置，默认 true
	standbyIgnoreInstallErrors bool // 忽略软件包安装错误

	// 备库 OS 用户参数（用于 yasboot 命令）
	standbyOSUser         string
	standbyOSUserPassword string
	standbyOSGroup        string
	standbyOSDepsPkgs     string // 覆盖 OS 依赖包列表（B-015），仅在 --skip-os=false 时生效

	// 数据库参数（复用部分 db.go 中的参数）
	standbyClusterName   string
	standbyAdminPassword string
	standbyInstallPath   string
	standbyDataPath      string
	standbyLogPath       string
	standbyStageDir      string
	standbyDepsPackage   string
	standbyNodeCount     int

	// 扩容控制
	standbyCleanupOnFailure bool

	// YAC 模式和多实例支持
	standbyYACMode   bool // 是否为 YAC 模式（影响环境变量和自启动配置）
	standbyBeginPort int  // 数据库起始端口（用于多实例场景的环境变量文件命名）

	standbyYasbootExtraArgs string // 追加到 yasboot config node gen 的额外参数
)

// newStandbyStepContext 构造带全局步骤相关标志的 StepContext（与 db/os 一致：dry-run、precheck、force、软件目录等）。
func newStandbyStepContext(ex runner.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags) *runner.StepContext {
	return &runner.StepContext{
		Executor:          ex,
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
	}
}

// trySyncClusterNameFromPrimaryEnv 在主库上按 GetPrimaryEnvFile 规则定位 env 文件，能解析则写回 params["db_cluster_name"]。
// 不依赖是否传入 --primary-env-file（自动探测 ~/.port* 时同样可得到 yashandb_3988 等真实集群名），
// 使仅执行备库阶段（-s E-015 等）或跳过 E-002 时 params 已与主库一致。

// tryResolvePrimaryStageDir 设置 params["db_stage_dir"]：显式 --db-stage-dir 且非空则用该路径，否则与 yinstall db 一致（1688→/home/<user>/install，其它端口→install_<port>）。
func tryResolvePrimaryStageDir(cmd *cobra.Command, logger *logging.Logger, params map[string]interface{}) {
	if cmd.Flags().Changed("db-stage-dir") && strings.TrimSpace(standbyStageDir) != "" {
		params["db_stage_dir"] = strings.TrimSpace(standbyStageDir)
		logger.Info("Primary stage directory (--db-stage-dir): %s", params["db_stage_dir"])
		return
	}
	u, _ := params["primary_os_user"].(string)
	if strings.TrimSpace(u) == "" {
		u = "yashan"
	}
	port := 1688
	if v, ok := params["db_begin_port"].(int); ok {
		port = v
	}
	def := standbysteps.DefaultPrimaryStageDir(u, port)
	params["db_stage_dir"] = def
	logger.Info("Primary stage directory (default): %s", def)
}

// tryResolveExpansionPaths 设置备库扩容传给 yasboot 的 install/data/log：显式 flag 且非空则用 flag，否则与 yinstall db 默认路径一致（1688 无 _port 后缀）。
func tryResolveExpansionPaths(cmd *cobra.Command, logger *logging.Logger, params map[string]interface{}) {
	u, _ := params["primary_os_user"].(string)
	if strings.TrimSpace(u) == "" {
		u = "yashan"
	}
	port := 1688
	if v, ok := params["db_begin_port"].(int); ok {
		port = v
	}

	if cmd.Flags().Changed("db-home-path") && strings.TrimSpace(standbyInstallPath) != "" {
		params["db_install_path"] = strings.TrimSpace(standbyInstallPath)
	} else {
		params["db_install_path"] = standbysteps.DefaultExpansionInstallPath(u, port)
	}
	if cmd.Flags().Changed("db-data-path") && strings.TrimSpace(standbyDataPath) != "" {
		params["db_data_path"] = strings.TrimSpace(standbyDataPath)
	} else {
		params["db_data_path"] = standbysteps.DefaultExpansionDataPath(u, port)
	}
	if cmd.Flags().Changed("db-log-path") && strings.TrimSpace(standbyLogPath) != "" {
		params["db_log_path"] = strings.TrimSpace(standbyLogPath)
	} else {
		params["db_log_path"] = standbysteps.DefaultExpansionLogPath(u, port)
	}
	logger.Info("Expansion paths: install=%s data=%s log=%s", params["db_install_path"], params["db_data_path"], params["db_log_path"])
}

// tryFillBeginPortFromPrimary 未显式传入 --db-port 时，在主库查询 LISTEN_ADDR 并写入 params["db_begin_port"]。
func tryFillBeginPortFromPrimary(cmd *cobra.Command, ex ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags) error {
	if cmd.Flags().Changed("db-port") {
		logger.Info("Database begin port: %d (--db-port)", standbyBeginPort)
		return nil
	}
	if flags.DryRun {
		logger.Info("Dry-run: database begin port %d (auto LISTEN_ADDR fill skipped; pass --db-port to pin)", standbyBeginPort)
		return nil
	}
	ctx := newStandbyStepContext(&runnerExecAdapter{e: ex}, logger, params, flags)
	if err := standbysteps.FillBeginPortFromPrimaryListenAddr(ctx); err != nil {
		return fmt.Errorf("omit --db-port: failed to derive port from primary LISTEN_ADDR (v$parameter): %w", err)
	}
	if p, ok := params["db_begin_port"].(int); ok {
		logger.Info("Database begin port: %d (from primary LISTEN_ADDR)", p)
	}
	return nil
}

func trySyncClusterNameFromPrimaryEnv(ex ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags) {
	ctx := newStandbyStepContext(&runnerExecAdapter{e: ex}, logger, params, flags)
	envFile, err := standbysteps.GetPrimaryEnvFile(ctx)
	if err != nil {
		if pef, _ := params["primary_env_file"].(string); strings.TrimSpace(pef) != "" {
			logger.Warn("Could not resolve primary env file for cluster name sync: %v", err)
		}
		return
	}
	if err := standbysteps.SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		logger.Warn("Could not derive db_cluster_name from primary env file: %v", err)
		return
	}
}

// setStandbyStepProgress 设置 StepIndex/TotalSteps，修正日志里 “step 0 of 0”。
func setStandbyStepProgress(ctx *runner.StepContext, orderedFiltered []*runner.Step, current *runner.Step) {
	ctx.TotalSteps = len(orderedFiltered)
	for i, s := range orderedFiltered {
		if s != nil && current != nil && s.ID == current.ID {
			ctx.StepIndex = i
			return
		}
	}
	ctx.StepIndex = 0
}

// standbyCmd 添加备库命令
var standbyCmd = &cobra.Command{
	Use:   "standby",
	Short: "Add standby database to existing cluster",
	Long: `Add standby database node(s) to an existing primary database:
  - Check primary database status
  - Configure standby node OS (optional, controlled by --skip-os, default: skip)
  - Generate expansion configuration
  - Install software on standby nodes
  - Create standby instances
  - Configure environment variables
  - Verify standby synchronization

Global --include-steps / -s applies to every phase (e.g. -s E-013 runs only E-013 if prerequisites are already satisfied).
Note: a trailing hyphen is range syntax (e.g. E-013- means E-013 through the last step), not a single step; use -s E-013 for one step.
Global -l/--list-steps prints the OS + standby step catalog; the OS section follows the current --skip-os value (default: B-001 only).`,
	RunE:         runStandby,
	SilenceUsage: true, // 报错时不显示帮助信息
}

func init() {
	// 主库连接参数
	standbyCmd.Flags().StringVar(&primaryIP, "primary-ip", "", "Primary database IP address (required)")
	standbyCmd.Flags().StringVar(&primarySSHUser, "primary-ssh-user", "", "Primary SSH user (defaults to --ssh-user)")
	standbyCmd.Flags().StringVar(&primarySSHPassword, "primary-ssh-password", "", "Primary SSH password (defaults to --ssh-password)")
	standbyCmd.Flags().StringVar(&primarySSHKey, "primary-ssh-key", "", "Primary SSH key path (defaults to --ssh-key-path)")

	// 主库数据库用户和环境变量参数
	standbyCmd.Flags().StringVar(&primaryOSUser, "primary-os-user", "yashan", "Primary database user (default: yashan)")
	standbyCmd.Flags().StringVar(&primaryEnvFile, "primary-env-file", "", "Primary environment file path (default: auto-detect from .yasboot or .bashrc)")

	// 操作系统配置控制
	standbyCmd.Flags().BoolVar(&skipOS, "skip-os", true, "Skip standby OS baseline configuration (default: true)")
	standbyCmd.Flags().BoolVar(&standbyIgnoreInstallErrors, "os-ignore-install-errors", false, "Ignore package installation errors and continue (only show warnings)")
	standbyCmd.Flags().StringVar(&standbyOSDepsPkgs, "os-deps-db-packages", "", "[OS] Override DB dependency packages for B-015 (space-separated; only effective when --skip-os=false; default: use OS preset list)")

	// 备库 OS 用户参数
	standbyCmd.Flags().StringVar(&standbyOSUser, "os-user", "yashan", "Standby product user name")
	standbyCmd.Flags().StringVar(&standbyOSUserPassword, "os-user-password", "aaBB11@@33$$", "Standby user SSH password (for yasboot, yashan default)")
	standbyCmd.Flags().StringVar(&standbyOSGroup, "os-group", "yashan", "Standby primary group name")

	// 数据库参数
	standbyCmd.Flags().StringVar(&standbyClusterName, "db-cluster-name", "yashandb", "Database cluster name (must match primary)")
	standbyCmd.Flags().StringVar(&standbyAdminPassword, "db-admin-password", "", "Database SYS admin password (optional, not used in standby creation)")
	standbyCmd.Flags().StringVar(&standbyInstallPath, "db-home-path", "", "Standby install path for yasboot (default: same as yinstall db — /data/<primary-os-user>/yasdb_home for port 1688, else yasdb_home_<port>)")
	standbyCmd.Flags().StringVar(&standbyDataPath, "db-data-path", "", "Standby data path for yasboot (default: same as yinstall db — /data/<primary-os-user>/yasdb_data or .../yasdb_data_<port>)")
	standbyCmd.Flags().StringVar(&standbyLogPath, "db-log-path", "", "Standby log path for yasboot (default: same as yinstall db — /data/<primary-os-user>/log or .../log_<port>)")
	standbyCmd.Flags().StringVar(&standbyStageDir, "db-stage-dir", "", "Primary stage directory on primary host (must exist; default same as yinstall db — /home/<user>/install for 1688, else install_<port>; port from --db-port or LISTEN_ADDR)")
	standbyCmd.Flags().StringVar(&standbyDepsPackage, "db-deps-package", "", "SSL deps package path (optional)")
	standbyCmd.Flags().IntVar(&standbyNodeCount, "standby-node-count", 0, "Number of standby nodes (auto-detected from --targets)")

	// 扩容控制
	standbyCmd.Flags().BoolVar(&standbyCleanupOnFailure, "standby-cleanup-on-failure", false, "Auto cleanup on failure (dangerous, requires --force)")

	// YAC 模式和多实例支持
	standbyCmd.Flags().BoolVar(&standbyYACMode, "yac-mode", false, "Enable YAC mode (affects env vars and autostart config)")
	standbyCmd.Flags().IntVar(&standbyBeginPort, "db-port", 1688, "Database begin port for yasboot expansion (default 1688; omit flag to use primary v$parameter.LISTEN_ADDR port)")
	standbyCmd.Flags().StringVar(&standbyYasbootExtraArgs, "yasboot-extra-args", "", "Extra arguments appended to yasboot config node gen only (space-separated)")
}

// runStandby 执行添加备库流程
func runStandby(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintStandbyStepCatalog(skipOS)
		return nil
	}

	// 参数校验
	if err := validateStandbyParams(flags); err != nil {
		return err
	}

	// Derive local execution for standby when both primary and all standby targets are local.
	// (Standby still requires --targets, but we can avoid SSH when operating on localhost.)
	if isLocalHost(primaryIP) {
		allLocal := true
		for _, t := range flags.Targets {
			if !isLocalHost(t) {
				allLocal = false
				break
			}
		}
		if allLocal {
			flags.Local = true
			// In local mode, do not inject default standby os-user-password unless explicitly set by user.
			if !cmd.Flags().Changed("os-user-password") {
				standbyOSUserPassword = ""
			}
		}
	}

	// 设置主库 SSH 参数默认值（继承全局参数）
	if primarySSHUser == "" {
		primarySSHUser = flags.SSHUser
	}
	if primarySSHPassword == "" {
		primarySSHPassword = flags.SSHPassword
	}
	if primarySSHKey == "" {
		primarySSHKey = flags.SSHKeyPath
	}

	// 自动推导节点数量
	if standbyNodeCount == 0 {
		standbyNodeCount = len(flags.Targets)
	}

	// 初始化日志
	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("standby-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting standby installation (RunID: %s)", rid)
	logger.Info("Primary: %s", primaryIP)
	logger.Info("Standby targets: %v", flags.Targets)

	if skipOS {
		logger.Info("Standby OS baseline: SKIPPED")
	} else {
		logger.Info("Standby OS baseline: ENABLED")
	}

	if standbyYACMode {
		logger.Info("YAC mode: ENABLED (ycsrootagent autostart will be configured)")
	} else {
		logger.Info("YAC mode: DISABLED")
	}

	// 构建参数
	params := buildStandbyParams(flags)

	// 创建主库执行器
	primaryExecutor, err := createPrimaryExecutor(flags, logger, "")
	if err != nil {
		return fmt.Errorf("failed to connect to primary: %w", err)
	}
	defer primaryExecutor.Close()

	trySyncClusterNameFromPrimaryEnv(primaryExecutor, logger, params, flags)
	if cn, ok := params["db_cluster_name"].(string); ok && cn != "" {
		logger.Info("Cluster name: %s", cn)
	} else {
		logger.Info("Cluster name: %s", standbyClusterName)
	}

	if err := tryFillBeginPortFromPrimary(cmd, primaryExecutor, logger, params, flags); err != nil {
		return err
	}

	tryResolvePrimaryStageDir(cmd, logger, params)
	tryResolveExpansionPaths(cmd, logger, params)

	// 收集所有步骤
	var allSteps []*runner.Step

	// 如果 skipOS=false，添加 OS 步骤到备库节点
	if !skipOS {
		osSteps := ossteps.GetAllSteps()
		allSteps = append(allSteps, osSteps...)
	} else {
		// 即使跳过 OS，也需要连通性检查 (B-001)
		osSteps := ossteps.GetAllSteps()
		for _, step := range osSteps {
			if step.ID == "B-001" {
				allSteps = append(allSteps, step)
				break
			}
		}
	}

	// 添加备库扩容步骤
	standbySteps := standbysteps.GetAllSteps()
	allSteps = append(allSteps, standbySteps...)

	// 过滤步骤
	steps := filterSteps(allSteps, flags)
	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}

	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	// 分类步骤：OS 步骤在备库执行，E 步骤根据类型决定执行位置
	osStepsFiltered, standbyStepsFiltered := categorizeStandbySteps(steps)

	// 阶段 1：主库连通性检查和状态检查（仅执行 filterSteps 后列表中的 E-001～E-004）
	logger.Info("======== Phase 1: Primary connectivity and status check ========")
	if err := checkPrimaryStatus(primaryExecutor, logger, params, steps, flags); err != nil {
		return err
	}

	// 阶段 2：备库节点连通性检查和 OS 配置
	logger.Info("======== Phase 2: Standby nodes preparation ========")
	standbyHosts, err := prepareStandbyNodes(flags, logger, params, osStepsFiltered, steps)
	if err != nil {
		return err
	}
	defer closeStandbyExecutors(standbyHosts)

	// 阶段 3：检查归档路径和网络连通性
	logger.Info("======== Phase 3: Archive destination check and network connectivity ========")
	if err := checkArchiveDestination(primaryExecutor, logger, params, steps, flags); err != nil {
		return err
	}
	if err := checkNetworkConnectivity(primaryExecutor, standbyHosts, logger, params, steps, flags); err != nil {
		return err
	}

	// 阶段 4：检查并清理已存在的节点，然后执行扩容步骤
	logger.Info("======== Phase 4: Check existing nodes and execute expansion steps on primary ========")
	if err := checkAndCleanupExistingNodes(primaryExecutor, logger, params, steps, flags); err != nil {
		return err
	}
	if err := executeExpansionSteps(primaryExecutor, logger, params, flags, steps, standbyStepsFiltered); err != nil {
		return err
	}

	// 阶段 5：备库后续配置（环境变量、自启动）
	logger.Info("======== Phase 5: Standby post-configuration ========")
	if err := configureStandbyPostSteps(standbyHosts, logger, params, flags, steps); err != nil {
		return err
	}

	// 阶段 6：显示集群状态
	logger.Info("======== Phase 6: Show cluster status ========")
	if err := showClusterStatus(primaryExecutor, logger, params, steps, flags); err != nil {
		return err
	}

	// 阶段 7：可选清理（E-018，在主库；仅当过滤结果含该步；PreCheck 不满足则 Optional 跳过）
	if err := runStandbyOptionalCleanup(primaryExecutor, logger, params, steps, flags); err != nil {
		return err
	}

	logger.Info("Standby installation completed successfully")
	return nil
}

// validateStandbyParams 校验必填参数
func validateStandbyParams(flags GlobalFlags) error {
	if primaryIP == "" {
		return fmt.Errorf("--primary-ip is required")
	}

	if len(flags.Targets) == 0 {
		return fmt.Errorf("--targets is required (standby node IP addresses)")
	}

	if err := validatePort("--db-port", standbyBeginPort); err != nil {
		return err
	}

	return nil
}

// buildStandbyParams 构建备库参数
func buildStandbyParams(flags GlobalFlags) map[string]interface{} {
	// 复用 OS 参数构建
	params := buildOSParams(false, len(flags.Targets))

	// 覆盖备库特定的 OS 用户参数
	if standbyOSUser != "" {
		params["os_user"] = standbyOSUser
	}
	if standbyOSUserPassword != "" {
		params["os_user_password"] = standbyOSUserPassword
	}
	if standbyOSGroup != "" {
		params["os_group"] = standbyOSGroup
	}

	// Override OS ignore install errors if specified
	params["os_ignore_install_errors"] = standbyIgnoreInstallErrors
	if strings.TrimSpace(standbyOSDepsPkgs) != "" {
		params["os_deps_db_packages"] = strings.TrimSpace(standbyOSDepsPkgs)
	}

	params["ssh_port"] = flags.SSHPort
	params["yasboot_ssh_port"] = flags.YasbootSSHPort

	// 主库参数
	params["primary_ip"] = primaryIP
	params["primary_ssh_user"] = primarySSHUser
	params["primary_ssh_password"] = primarySSHPassword
	params["primary_ssh_key"] = primarySSHKey
	params["primary_os_user"] = primaryOSUser
	params["primary_env_file"] = primaryEnvFile

	// 数据库参数
	params["db_cluster_name"] = standbyClusterName
	params["db_admin_password"] = standbyAdminPassword
	if strings.TrimSpace(standbyInstallPath) != "" {
		params["db_install_path"] = strings.TrimSpace(standbyInstallPath)
	}
	if strings.TrimSpace(standbyDataPath) != "" {
		params["db_data_path"] = strings.TrimSpace(standbyDataPath)
	}
	if strings.TrimSpace(standbyLogPath) != "" {
		params["db_log_path"] = strings.TrimSpace(standbyLogPath)
	}
	if strings.TrimSpace(standbyStageDir) != "" {
		params["db_stage_dir"] = strings.TrimSpace(standbyStageDir)
	}
	params["db_deps_package"] = standbyDepsPackage

	// YAC 模式和端口参数（影响环境变量和自启动配置）
	params["yac_mode"] = standbyYACMode
	params["db_begin_port"] = standbyBeginPort

	// 备库特定参数
	params["standby_node_count"] = standbyNodeCount
	params["standby_targets"] = flags.Targets
	params["standby_targets_str"] = strings.Join(flags.Targets, ",")
	params["standby_cleanup_on_failure"] = standbyCleanupOnFailure
	params["skip_os"] = skipOS
	params["yasboot_extra_args"] = standbyYasbootExtraArgs

	return params
}

// createPrimaryExecutor 创建主库执行器
func createPrimaryExecutor(flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	cfg := ssh.Config{
		Host:       primaryIP,
		Port:       flags.SSHPort,
		User:       primarySSHUser,
		AuthMethod: flags.SSHAuth,
		Password:   primarySSHPassword,
		KeyPath:    primarySSHKey,
		Logger:     logger,
		StepID:     stepID,
	}

	if flags.Local {
		cfg.AuthMethod = "local"
		return ssh.NewExecutor(cfg)
	}

	// 如果用户没有提供密码，使用fallback逻辑
	if primarySSHPassword == "" && flags.SSHAuth == "password" {
		return ssh.NewExecutorWithFallback(cfg, "")
	}

	return ssh.NewExecutor(cfg)
}

func isLocalHost(host string) bool {
	h := strings.TrimSpace(strings.ToLower(host))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// categorizeStandbySteps 分类步骤：OS 步骤和扩容步骤
func categorizeStandbySteps(steps []*runner.Step) ([]*runner.Step, []*runner.Step) {
	var osSteps, standbySteps []*runner.Step
	for _, step := range steps {
		if strings.HasPrefix(step.ID, "B-") {
			osSteps = append(osSteps, step)
		} else if strings.HasPrefix(step.ID, "E-") {
			standbySteps = append(standbySteps, step)
		}
	}
	return osSteps, standbySteps
}

// checkPrimaryStatus 检查主库状态（仅运行 filtered 中的 E-001～E-004，尊重 -s/--include-steps）
func checkPrimaryStatus(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	host := executor.Host()
	logger.Info("Checking primary database status on %s", host)

	ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)

	primaryPhase := map[string]bool{"E-001": true, "E-002": true, "E-003": true, "E-004": true}
	for _, step := range filtered {
		if !primaryPhase[step.ID] {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
	}

	return nil
}

// prepareStandbyNodes 准备备库节点（OS 基线 + 备库侧 E-005～E-007；各步仅当出现在 filtered 中才执行，且按 E-005→E-006→E-007 顺序）
func prepareStandbyNodes(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}, osSteps []*runner.Step, filtered []*runner.Step) ([]*HostInfo, error) {
	var hostInfos []*HostInfo

	for _, target := range flags.Targets {
		executor, err := createExecutor(target, flags, logger, "")
		if err != nil {
			return nil, fmt.Errorf("failed to connect to standby %s: %w", target, err)
		}

		logger.Info("-------- Standby: %s --------", target)

		ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)

		for _, step := range osSteps {
			ctx.CurrentStepID = step.ID
			setStandbyStepProgress(ctx, filtered, step)
			result := runner.RunStep(step, ctx)

			// 更新 OSInfo
			if step.ID == "B-001" && result.Success {
				hostInfos = append(hostInfos, &HostInfo{
					Host:     target,
					Executor: executor,
					OSInfo:   ctx.OSInfo,
				})
			}

			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			// B-015 等关键步骤失败时应该直接退出
			if !result.Success && !result.Skipped {
				executor.Close()
				return nil, fmt.Errorf("step %s failed on %s: %w", step.ID, target, result.Error)
			}
		}

		// 如果没有执行 B-001，也要添加到列表
		found := false
		for _, info := range hostInfos {
			if info.Host == target {
				found = true
				break
			}
		}
		if !found {
			hostInfos = append(hostInfos, &HostInfo{
				Host:     target,
				Executor: executor,
			})
		}

		// 备库侧预检 E-005～E-007（在每台备库上执行；仅当 -s 过滤结果包含对应步，顺序固定）
		standbyPrepStepIDs := []string{"E-005", "E-006", "E-007"}
		for _, id := range standbyPrepStepIDs {
			for _, step := range filtered {
				if step.ID != id {
					continue
				}
				ctx.CurrentStepID = step.ID
				setStandbyStepProgress(ctx, filtered, step)
				result := runner.RunStep(step, ctx)
				if !result.Success && !result.Skipped {
					executor.Close()
					return nil, fmt.Errorf("step %s failed on %s: %w", step.ID, target, result.Error)
				}
				break
			}
		}
	}

	return hostInfos, nil
}

// closeStandbyExecutors 关闭备库执行器
func closeStandbyExecutors(hosts []*HostInfo) {
	for _, host := range hosts {
		if host.Executor != nil {
			host.Executor.Close()
		}
	}
}

// checkArchiveDestination 检查归档路径是否已包含目标端（仅当 filtered 含 E-008）
func checkArchiveDestination(primaryExecutor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Checking if archive destination already contains standby targets")

	ctx := newStandbyStepContext(&runnerExecAdapter{e: primaryExecutor}, logger, params, flags)

	for _, step := range filtered {
		if step.ID != "E-008" {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		break
	}

	return nil
}

// checkAndCleanupExistingNodes 检查并清理已存在的节点（仅当 filtered 含 E-010）
func checkAndCleanupExistingNodes(primaryExecutor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Checking and cleaning up existing nodes if needed")

	ctx := newStandbyStepContext(&runnerExecAdapter{e: primaryExecutor}, logger, params, flags)

	for _, step := range filtered {
		if step.ID != "E-010" {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		break
	}

	return nil
}

// checkNetworkConnectivity 检查主备网络互通（仅当 filtered 含 E-009）
func checkNetworkConnectivity(primaryExecutor ssh.Executor, standbyHosts []*HostInfo, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Checking network connectivity between primary and standby nodes")

	for _, step := range filtered {
		if step.ID != "E-009" {
			continue
		}
		ctx := newStandbyStepContext(&runnerExecAdapter{e: primaryExecutor}, logger, params, flags)
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if step.Optional {
				logger.Warn("Network connectivity check failed, but step is optional, continuing...")
			} else {
				return fmt.Errorf("network connectivity check failed: %w", result.Error)
			}
		}
		break
	}

	return nil
}

// executeExpansionSteps 在主库执行扩容步骤
func executeExpansionSteps(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, orderedFiltered []*runner.Step, standbySteps []*runner.Step) error {
	host := executor.Host()
	logger.Info("Executing expansion steps on primary: %s", host)

	ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)

	// 执行扩容步骤（E-011, E-012, E-013, E-014）
	// standbySteps 已经通过 filterSteps 过滤，如果用户指定了 --include-steps，
	// 则只包含指定的步骤；否则包含所有步骤
	expansionStepIDs := []string{"E-011", "E-012", "E-013", "E-014"}

	for _, step := range standbySteps {
		// 检查步骤是否在扩容步骤列表中
		isExpansionStep := false
		for _, id := range expansionStepIDs {
			if step.ID == id {
				isExpansionStep = true
				break
			}
		}
		
		if isExpansionStep {
			ctx.CurrentStepID = step.ID
			setStandbyStepProgress(ctx, orderedFiltered, step)
			result := runner.RunStep(step, ctx)
			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
			}
		}
	}

	return nil
}

// configureStandbyPostSteps 配置备库后续步骤（仅执行 filtered 中的 E-015、E-016、E-017，顺序与过滤列表一致）
func configureStandbyPostSteps(standbyHosts []*HostInfo, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, filtered []*runner.Step) error {
	postPhase := map[string]bool{"E-015": true, "E-016": true, "E-017": true}
	var postSteps []*runner.Step
	for _, step := range filtered {
		if postPhase[step.ID] {
			postSteps = append(postSteps, step)
		}
	}

	for _, host := range standbyHosts {
		logger.Info("-------- Standby post-config: %s --------", host.Host)

		ctx := newStandbyStepContext(&runnerExecAdapter{e: host.Executor}, logger, params, flags)
		ctx.OSInfo = host.OSInfo

		for _, step := range postSteps {
			ctx.CurrentStepID = step.ID
			setStandbyStepProgress(ctx, filtered, step)
			result := runner.RunStep(step, ctx)
			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed on %s: %w", step.ID, host.Host, result.Error)
			}
		}
	}

	return nil
}

// showClusterStatus 显示集群状态（仅当 filtered 含 E-019）
func showClusterStatus(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Showing cluster status on primary database")
	ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)
	for _, step := range filtered {
		if step.ID != "E-019" {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		return nil
	}
	logger.Warn("E-019 not in filtered step list, skipping cluster status display")
	return nil
}

// runStandbyOptionalCleanup 若 filtered 含 E-018，在主库执行；步骤为 Optional，PreCheck 不满足时跳过
func runStandbyOptionalCleanup(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	for _, step := range filtered {
		if step.ID != "E-018" {
			continue
		}
		logger.Info("======== Phase 7: Optional cleanup (E-018) ========")
		ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		return nil
	}
	return nil
}
