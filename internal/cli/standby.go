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
  - Verify standby synchronization`,
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

	// 备库 OS 用户参数
	standbyCmd.Flags().StringVar(&standbyOSUser, "os-user", "yashan", "Standby product user name")
	standbyCmd.Flags().StringVar(&standbyOSUserPassword, "os-user-password", "aaBB11@@33$$", "Standby user SSH password (for yasboot, yashan default)")
	standbyCmd.Flags().StringVar(&standbyOSGroup, "os-group", "yashan", "Standby primary group name")

	// 数据库参数
	standbyCmd.Flags().StringVar(&standbyClusterName, "db-cluster-name", "yashandb", "Database cluster name (must match primary)")
	standbyCmd.Flags().StringVar(&standbyAdminPassword, "db-admin-password", "", "Database SYS admin password (optional, not used in standby creation)")
	standbyCmd.Flags().StringVar(&standbyInstallPath, "db-home-path", "/data/yashan/yasdb_home", "Software installation path")
	standbyCmd.Flags().StringVar(&standbyDataPath, "db-data-path", "/data/yashan/yasdb_data", "Data directory path")
	standbyCmd.Flags().StringVar(&standbyLogPath, "db-log-path", "/data/yashan/log", "Log directory path")
	standbyCmd.Flags().StringVar(&standbyStageDir, "db-stage-dir", "/home/yashan/install", "Primary stage directory (where yasboot runs)")
	standbyCmd.Flags().StringVar(&standbyDepsPackage, "db-deps-package", "", "SSL deps package path (optional)")
	standbyCmd.Flags().IntVar(&standbyNodeCount, "standby-node-count", 0, "Number of standby nodes (auto-detected from --targets)")

	// 扩容控制
	standbyCmd.Flags().BoolVar(&standbyCleanupOnFailure, "standby-cleanup-on-failure", false, "Auto cleanup on failure (dangerous, requires --force)")

	// YAC 模式和多实例支持
	standbyCmd.Flags().BoolVar(&standbyYACMode, "yac-mode", false, "Enable YAC mode (affects env vars and autostart config)")
	standbyCmd.Flags().IntVar(&standbyBeginPort, "db-begin-port", 1688, "Database begin port (for multi-instance env file naming)")
	standbyCmd.Flags().StringVar(&standbyYasbootExtraArgs, "yasboot-extra-args", "", "Extra arguments appended to yasboot config node gen only (space-separated)")
}

// runStandby 执行添加备库流程
func runStandby(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()

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
	logger.Info("Cluster name: %s", standbyClusterName)

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

	// 收集所有步骤
	var allSteps []*runner.Step

	// 如果 skipOS=false，添加 OS 步骤到备库节点
	if !skipOS {
		osSteps := ossteps.GetAllSteps()
		allSteps = append(allSteps, osSteps...)
	} else {
		// 即使跳过 OS，也需要连通性检查 (B-000)
		osSteps := ossteps.GetAllSteps()
		for _, step := range osSteps {
			if step.ID == "B-000" {
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

	// 阶段 1：主库连通性检查和状态检查
	logger.Info("======== Phase 1: Primary connectivity and status check ========")
	if err := checkPrimaryStatus(primaryExecutor, logger, params); err != nil {
		return err
	}

	// 阶段 2：备库节点连通性检查和 OS 配置
	logger.Info("======== Phase 2: Standby nodes preparation ========")
	standbyHosts, err := prepareStandbyNodes(flags, logger, params, osStepsFiltered)
	if err != nil {
		return err
	}
	defer closeStandbyExecutors(standbyHosts)

	// 阶段 3：检查归档路径和网络连通性
	logger.Info("======== Phase 3: Archive destination check and network connectivity ========")
	if err := checkArchiveDestination(primaryExecutor, logger, params); err != nil {
		return err
	}
	if err := checkNetworkConnectivity(primaryExecutor, standbyHosts, logger, params); err != nil {
		return err
	}

	// 阶段 4：检查并清理已存在的节点，然后执行扩容步骤
	logger.Info("======== Phase 4: Check existing nodes and execute expansion steps on primary ========")
	if err := checkAndCleanupExistingNodes(primaryExecutor, logger, params); err != nil {
		return err
	}
	if err := executeExpansionSteps(primaryExecutor, logger, params, flags, standbyStepsFiltered); err != nil {
		return err
	}

	// 阶段 5：备库后续配置（环境变量、自启动）
	logger.Info("======== Phase 5: Standby post-configuration ========")
	if err := configureStandbyPostSteps(standbyHosts, logger, params, flags); err != nil {
		return err
	}

	// 阶段 6：显示集群状态
	logger.Info("======== Phase 6: Show cluster status ========")
	if err := showClusterStatus(primaryExecutor, logger, params); err != nil {
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

	if err := validatePort("--db-begin-port", standbyBeginPort); err != nil {
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
	params["db_install_path"] = standbyInstallPath
	params["db_data_path"] = standbyDataPath
	params["db_log_path"] = standbyLogPath
	params["db_stage_dir"] = standbyStageDir
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

// checkPrimaryStatus 检查主库状态
func checkPrimaryStatus(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}) error {
	host := executor.Host()
	logger.Info("Checking primary database status on %s", host)

	ctx := &runner.StepContext{
		Executor: &runnerExecAdapter{e: executor},
		Logger:   logger,
		Params:   params,
		Results:  make(map[string]interface{}),
	}

	// 获取 E-000, E-001, E-001-A, E-001-B 步骤
	steps := standbysteps.GetAllSteps()
	for _, step := range steps {
		if step.ID == "E-000" || step.ID == "E-001" || step.ID == "E-001-A" || step.ID == "E-001-B" {
			ctx.CurrentStepID = step.ID
			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
			}
		}
	}

	return nil
}

// prepareStandbyNodes 准备备库节点（连通性检查 + OS 配置 + 用户密码验证）
func prepareStandbyNodes(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}, osSteps []*runner.Step) ([]*HostInfo, error) {
	var hostInfos []*HostInfo

	for _, target := range flags.Targets {
		executor, err := createExecutor(target, flags, logger, "")
		if err != nil {
			return nil, fmt.Errorf("failed to connect to standby %s: %w", target, err)
		}

		logger.Info("-------- Standby: %s --------", target)

		// 执行 OS 步骤
		ctx := &runner.StepContext{
			Executor:          &runnerExecAdapter{e: executor},
			Logger:            logger,
			Params:            params,
			Results:           make(map[string]interface{}),
			LocalSoftwareDirs: flags.LocalSoftwareDirs,
			RemoteSoftwareDir: flags.RemoteSoftwareDir,
			ForceAll:          flags.ForceAll,
			ForceSteps:        flags.ForceSteps,
			ForceDeleteUser:   flags.ForceDeleteUser,
		}

		for _, step := range osSteps {
			result := runner.RunStep(step, ctx)

			// 更新 OSInfo
			if step.ID == "B-000" && result.Success {
				hostInfos = append(hostInfos, &HostInfo{
					Host:     target,
					Executor: executor,
					OSInfo:   ctx.OSInfo,
				})
			}

			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			// B-012 等关键步骤失败时应该直接退出
			if !result.Success && !result.Skipped {
				executor.Close()
				return nil, fmt.Errorf("step %s failed on %s: %w", step.ID, target, result.Error)
			}
		}

		// 如果没有执行 B-000，也要添加到列表
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

		// 执行 E-002 用户密码验证（在备库节点上执行）
		steps := standbysteps.GetAllSteps()
		for _, step := range steps {
			if step.ID == "E-002" {
				result := runner.RunStep(step, ctx)
				// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
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

// checkArchiveDestination 检查归档路径是否已包含目标端
func checkArchiveDestination(primaryExecutor ssh.Executor, logger *logging.Logger, params map[string]interface{}) error {
	logger.Info("Checking if archive destination already contains standby targets")

	ctx := &runner.StepContext{
		Executor: &runnerExecAdapter{e: primaryExecutor},
		Logger:   logger,
		Params:   params,
		Results:  make(map[string]interface{}),
	}

	// 获取 E-002-A 步骤
	steps := standbysteps.GetAllSteps()
	for _, step := range steps {
		if step.ID == "E-002-A" {
			ctx.CurrentStepID = step.ID
			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
			}
		}
	}

	return nil
}

// checkAndCleanupExistingNodes 检查并清理已存在的节点
func checkAndCleanupExistingNodes(primaryExecutor ssh.Executor, logger *logging.Logger, params map[string]interface{}) error {
	logger.Info("Checking and cleaning up existing nodes if needed")

	ctx := &runner.StepContext{
		Executor: &runnerExecAdapter{e: primaryExecutor},
		Logger:   logger,
		Params:   params,
		Results:  make(map[string]interface{}),
	}

	// 获取 E-003A 步骤
	steps := standbysteps.GetAllSteps()
	for _, step := range steps {
		if step.ID == "E-003A" {
			ctx.CurrentStepID = step.ID
			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
			}
		}
	}

	return nil
}

// checkNetworkConnectivity 检查主备网络互通
func checkNetworkConnectivity(primaryExecutor ssh.Executor, standbyHosts []*HostInfo, logger *logging.Logger, params map[string]interface{}) error {
	logger.Info("Checking network connectivity between primary and standby nodes")

	// 获取 E-003 步骤
	steps := standbysteps.GetAllSteps()
	for _, step := range steps {
		if step.ID == "E-003" {
			ctx := &runner.StepContext{
				Executor: &runnerExecAdapter{e: primaryExecutor},
				Logger:   logger,
				Params:   params,
				Results:  make(map[string]interface{}),
			}
			result := runner.RunStep(step, ctx)
			// E-003 是可选步骤，失败时仅警告，不阻止后续流程
			if !result.Success && !result.Skipped {
				if step.Optional {
					logger.Warn("Network connectivity check failed, but step is optional, continuing...")
				} else {
					return fmt.Errorf("network connectivity check failed: %w", result.Error)
				}
			}
		}
	}

	return nil
}

// executeExpansionSteps 在主库执行扩容步骤
func executeExpansionSteps(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, standbySteps []*runner.Step) error {
	host := executor.Host()
	logger.Info("Executing expansion steps on primary: %s", host)

	ctx := &runner.StepContext{
		Executor:        &runnerExecAdapter{e: executor},
		Logger:          logger,
		Params:          params,
		Results:         make(map[string]interface{}),
		ForceAll:        flags.ForceAll,
		ForceSteps:      flags.ForceSteps,
		ForceDeleteUser: flags.ForceDeleteUser,
	}

	// 执行扩容步骤（E-008, E-009, E-010, E-011）
	// standbySteps 已经通过 filterSteps 过滤，如果用户指定了 --include-steps，
	// 则只包含指定的步骤；否则包含所有步骤
	expansionStepIDs := []string{"E-008", "E-009", "E-010", "E-011"}

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
			result := runner.RunStep(step, ctx)
			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
			}
		}
	}

	return nil
}

// configureStandbyPostSteps 配置备库后续步骤（环境变量、自启动）
// 在备库节点上执行 E-012（配置环境变量）和 E-013（配置自启动）
func configureStandbyPostSteps(standbyHosts []*HostInfo, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags) error {
	// 获取 E-012, E-013 步骤（备库后续配置）
	steps := standbysteps.GetAllSteps()
	var postSteps []*runner.Step
	postStepIDs := []string{"E-012", "E-013"}

	for _, step := range steps {
		for _, id := range postStepIDs {
			if step.ID == id {
				postSteps = append(postSteps, step)
			}
		}
	}

	for _, host := range standbyHosts {
		logger.Info("-------- Standby post-config: %s --------", host.Host)

		ctx := &runner.StepContext{
			Executor:   &runnerExecAdapter{e: host.Executor},
			Logger:          logger,
			Params:          params,
			Results:         make(map[string]interface{}),
			OSInfo:          host.OSInfo,
			ForceAll:        flags.ForceAll,
			ForceSteps:      flags.ForceSteps,
			ForceDeleteUser: flags.ForceDeleteUser,
		}

		for _, step := range postSteps {
			ctx.CurrentStepID = step.ID
			result := runner.RunStep(step, ctx)
			// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed on %s: %w", step.ID, host.Host, result.Error)
			}
		}
	}

	return nil
}

// showClusterStatus 显示集群状态（E-016 步骤）
func showClusterStatus(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}) error {
	logger.Info("Showing cluster status on primary database")
	ctx := &runner.StepContext{
		Executor: &runnerExecAdapter{e: executor},
		Logger:   logger,
		Params:   params,
		Results:  make(map[string]interface{}),
	}
	steps := standbysteps.GetAllSteps()
	for _, step := range steps {
		if step.ID == "E-016" {
			ctx.CurrentStepID = step.ID
			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
			}
			return nil
		}
	}
	logger.Warn("E-016 step not found, skipping cluster status display")
	return nil
}
