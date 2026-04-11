// ymp.go - YMP 安装命令实现
// 本文件实现 yinstall ymp 命令，用于安装 YMP（YashanDB Migration Platform）
// 流程：OS 基线配置（可选）→ YMP 安装步骤（H-001 ~ H-012）

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
	ympsteps "github.com/yinstall/internal/steps/ymp"
)

var (
	// YMP OS 控制
	ympSkipOS              bool   // 是否跳过 OS 基线配置，默认 true
	ympIgnoreInstallErrors bool   // 忽略软件包安装错误
	ympOSYumMode           string // YUM 模式：online/local-iso/none
	ympOSISODevice         string // ISO 文件路径、文件名或块设备（local-iso 模式使用）
	ympOSISOMountpoint     string // ISO 挂载目录
	ympOSYumRepoFile       string // YUM repo 文件路径

	// YMP 用户参数
	ympUser         string
	ympUserPassword string

	// YMP 安装参数
	ympPackage    string
	ympInstallDir string
	ympPort       int

	// JDK 参数
	ympJDKEnable  bool
	ympJDKVersion string
	ympJDKPackage string

	// 软件包参数
	ympInstantclientBasic   string
	ympInstantclientSQLPlus string
	ympDBPackage            string

	// 环境变量
	ympOracleEnvFile string

	// YMP 依赖包
	ympDepsPackages string

	// 内置数据库模式
	ympDBMode string

	// 清理
	ympCleanup bool
)

var ympCmd = &cobra.Command{
	Use:   "ymp",
	Short: "Install YMP (YashanDB Migration Platform)",
	Long: `Install YMP (YashanDB Migration Platform):
  - OS baseline preparation (optional, controlled by --skip-os, default: skip)
    - Minimal OS steps: connectivity check, timezone, NTP, firewall
    - Note: YMP does not require full database OS baseline (no kernel params, multipath, etc.)
  - Create YMP user and configure limits
  - Install YMP dependencies (libaio, lsof)
  - Validate and install JDK
  - Extract YMP, Oracle instantclient, and sqlplus
  - Run YMP installation (ymp.sh install)
  - Verify processes and ports`,
	RunE:         runYMP,
	SilenceUsage: true,
}

func init() {
	// OS 控制
	ympCmd.Flags().BoolVar(&ympSkipOS, "skip-os", true, "Skip OS baseline preparation (default: true)")
	ympCmd.Flags().BoolVar(&ympIgnoreInstallErrors, "os-ignore-install-errors", false, "Ignore package installation errors and continue (only show warnings)")
	ympCmd.Flags().StringVar(&ympOSYumMode, "os-yum-mode", "none", "YUM/DNF mode: none (use system repos), local-iso (use mounted ISO repo only), online (internet repos)")
	ympCmd.Flags().StringVar(&ympOSISODevice, "os-iso-device", "/dev/cdrom", "ISO file path/name or block device used when --os-yum-mode=local-iso (auto-searched if filename only)")
	ympCmd.Flags().StringVar(&ympOSISOMountpoint, "os-iso-mountpoint", "/media", "Mount point for ISO when --os-yum-mode=local-iso")
	ympCmd.Flags().StringVar(&ympOSYumRepoFile, "os-yum-repo-file", "/etc/yum.repos.d/local.repo", "YUM repo file path for local-iso mode")

	// 用户参数
	ympCmd.Flags().StringVar(&ympUser, "ymp-user", "ymp", "YMP user name")
	ympCmd.Flags().StringVar(&ympUserPassword, "ymp-user-password", "aaBB11@@33$$", "YMP user password")

	// 安装参数
	ympCmd.Flags().StringVar(&ympPackage, "ymp-package", "", "YMP zip file path (optional, auto-searched if not specified)")
	ympCmd.Flags().StringVar(&ympInstallDir, "ymp-install-dir", "/opt/ymp", "YMP installation directory")
	ympCmd.Flags().IntVar(&ympPort, "ymp-port", 8090, "YMP Web service port (other ports will be calculated automatically: db=port+1, yasom=port+3, yasagent=port+4)")

	// JDK 参数
	ympCmd.Flags().BoolVar(&ympJDKEnable, "ymp-jdk-enable", false, "Install JDK (false = validate only)")
	ympCmd.Flags().StringVar(&ympJDKVersion, "ymp-jdk-version", "17", "Expected JDK version (8/11/17)")
	ympCmd.Flags().StringVar(&ympJDKPackage, "ymp-jdk-package", "", "JDK package path (required when --ymp-jdk-enable)")

	// 软件包参数
	ympCmd.Flags().StringVar(&ympInstantclientBasic, "ymp-instantclient-basic", "", "Oracle instantclient basic zip (optional, auto-searched if not specified; e.g. instantclient-basic-linux.arm64-19.10.0.0.0dbru-2.zip)")
	ympCmd.Flags().StringVar(&ympInstantclientSQLPlus, "ymp-instantclient-sqlplus", "", "Oracle instantclient sqlplus zip (optional)")
	ympCmd.Flags().StringVar(&ympDBPackage, "ymp-db-package", "", "YashanDB package for embedded database (optional, auto-searched if not specified)")

	// 环境变量
	ympCmd.Flags().StringVar(&ympOracleEnvFile, "ymp-oracle-env-file", "", "Oracle env file path (default: /home/<ymp-user>/.oracle)")

	// 依赖包
	ympCmd.Flags().StringVar(&ympDepsPackages, "ymp-deps-packages", "libaio lsof", "YMP dependency packages")

	// 内置数据库模式
	ympCmd.Flags().StringVar(&ympDBMode, "ymp-db-mode", "yashandb", "YMP embedded database mode: yashandb (default) or mysql")

	// 清理
	ympCmd.Flags().BoolVar(&ympCleanup, "ymp-cleanup", false, "Enable cleanup of failed installation (dangerous)")
}

// runYMP 执行 YMP 安装流程
func runYMP(cmd *cobra.Command, args []string) error {
	if err := validatePort("--ymp-port", ympPort); err != nil {
		return err
	}

	flags := GetGlobalFlags()

	// If --targets is not specified, default to local execution.
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}

	// 校验数据库模式
	if ympDBMode != "yashandb" && ympDBMode != "mysql" {
		return fmt.Errorf("invalid --ymp-db-mode: '%s' (case-sensitive). Valid values are: 'yashandb' (default) or 'mysql'", ympDBMode)
	}

	// ymp_package、ymp_instantclient_basic、ymp_db_package 均可为空，会在步骤 PreCheck 阶段自动查找最新版本

	// 初始化日志
	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("ymp-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	// 构建参数
	params := buildYMPParams(cmd, flags)

	logger.Info("Starting YMP installation (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)
	logger.Info("YMP user: %s", ympUser)
	logger.Info("YMP package: %s", ympPackage)
	logger.Info("Install directory: %s", params["ymp_install_dir"])
	logger.Info("YMP port: %d", ympPort)
	logger.Info("JDK install: %v (version: %s)", ympJDKEnable, ympJDKVersion)

	if ympSkipOS {
		logger.Info("OS baseline preparation: SKIPPED")
	} else {
		logger.Info("OS baseline preparation: ENABLED")
	}

	// 收集所有步骤
	var allSteps []*runner.Step

	if !ympSkipOS {
		// YMP只需要部分OS步骤，不需要完整的数据库OS基线配置
		// 需要的步骤：连通性检查、用户创建、资源限制、依赖包安装、主机名、时区、时间同步、防火墙
		osSteps := getYMPRequiredOSSteps()
		allSteps = append(allSteps, osSteps...)
		logger.Info("YMP OS preparation: Using minimal OS steps (connectivity, user, limits, deps, hostname, timezone, NTP, firewall)")
	} else {
		// 即使跳过OS，也需要连通性检查
		osSteps := ossteps.GetAllSteps()
		for _, step := range osSteps {
			if step.ID == "B-000" {
				allSteps = append(allSteps, step)
				break
			}
		}
	}

	// 添加 YMP 步骤
	ympSteps := ympsteps.GetAllSteps()
	allSteps = append(allSteps, ympSteps...)

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

	// 阶段 1：连通性检查
	var hostInfos []*HostInfo
	var connectivityStep *runner.Step
	var otherSteps []*runner.Step

	for _, step := range steps {
		if step.ID == "B-000" {
			connectivityStep = step
		} else {
			otherSteps = append(otherSteps, step)
		}
	}

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

	defer func() {
		for _, info := range hostInfos {
			info.Executor.Close()
		}
	}()

	// 阶段 2：执行步骤
	if len(otherSteps) > 0 {
		logger.Info("======== Phase 2: Executing steps ========")
	}

	// 分离 OS 步骤和 YMP 步骤
	var osStepsToRun []*runner.Step
	var ympStepsToRun []*runner.Step
	for _, step := range otherSteps {
		if strings.HasPrefix(step.ID, "B-") {
			osStepsToRun = append(osStepsToRun, step)
		} else {
			ympStepsToRun = append(ympStepsToRun, step)
		}
	}

	var lastErr error

	// OS 和 YMP 步骤共享 Results map（按主机隔离），使 OS 步骤产出可被 YMP 步骤读取
	hostResultsMap := make(map[string]map[string]interface{})
	for _, info := range hostInfos {
		hostResultsMap[info.Host] = make(map[string]interface{})
	}

	// OS 步骤：遍历所有节点执行
	if len(osStepsToRun) > 0 {
		for _, info := range hostInfos {
			logger.Info("-------- Host: %s (OS prep) --------", info.Host)

			for i, step := range osStepsToRun {
				ctx := &runner.StepContext{
					Executor:          &runnerExecAdapter{e: info.Executor},
					Logger:            logger,
					Params:            params,
					DryRun:            flags.DryRun,
					Precheck:          flags.Precheck,
					Results:           hostResultsMap[info.Host],
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
				// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
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
		stepIndex += len(osStepsToRun)
	}

	// YMP 步骤：遍历所有节点执行
	if lastErr == nil && len(ympStepsToRun) > 0 {
		for _, info := range hostInfos {
			logger.Info("-------- Host: %s (YMP install) --------", info.Host)

			for i, step := range ympStepsToRun {
				ctx := &runner.StepContext{
					Executor:          &runnerExecAdapter{e: info.Executor},
					Logger:            logger,
					Params:            params,
					DryRun:            flags.DryRun,
					Precheck:          flags.Precheck,
					Results:           hostResultsMap[info.Host],
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
				// 如果步骤失败（不是跳过），即使是 Optional 的也要退出
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
	}

	if lastErr != nil {
		logger.Error("YMP installation completed with errors")
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return lastErr
	}

	// 输出访问信息
	for _, info := range hostInfos {
		logger.Info("YMP access URL: http://%s:%d", info.Host, ympPort)
	}
	logger.Info("YMP service management: ymp.sh start/stop")
	logger.Info("YMP installation completed successfully")
	return nil
}

// buildYMPParams 构建 YMP 安装参数
func buildYMPParams(cmd *cobra.Command, flags GlobalFlags) map[string]interface{} {
	params := buildOSParams(false, len(flags.Targets))

	// Override OS ignore install errors if specified
	params["os_ignore_install_errors"] = ympIgnoreInstallErrors
	// YUM 模式及 ISO 参数覆盖（与 db --os-yum-mode 对齐）
	params["os_yum_mode"] = ympOSYumMode
	params["os_iso_device"] = ympOSISODevice
	params["os_iso_mountpoint"] = ympOSISOMountpoint
	params["os_yum_repo_file"] = ympOSYumRepoFile

	// YMP 用户参数
	params["ymp_user"] = ympUser
	params["ymp_user_password"] = ympUserPassword

	// YMP 安装参数：非默认端口且未显式指定安装目录时，自动拼接端口号
	installDir := ympInstallDir
	if !cmd.Flags().Changed("ymp-install-dir") && ympPort != 8090 {
		installDir = fmt.Sprintf("/opt/ymp%d", ympPort)
	}

	params["ymp_package"] = ympPackage
	params["ymp_install_dir"] = installDir
	params["ymp_port"] = ympPort
	// 其他端口根据 YMP 端口自动计算
	params["ymp_db_port"] = ympPort + 1       // 数据库端口 = YMP端口 + 1
	params["ymp_yasom_port"] = ympPort + 3    // yasom端口 = YMP端口 + 3
	params["ymp_yasagent_port"] = ympPort + 4 // yasagent端口 = YMP端口 + 4

	// JDK 参数
	params["ymp_jdk_enable"] = ympJDKEnable
	params["ymp_jdk_version"] = ympJDKVersion
	params["ymp_jdk_package"] = ympJDKPackage

	// 软件包参数
	params["ymp_instantclient_basic"] = ympInstantclientBasic
	params["ymp_instantclient_sqlplus"] = ympInstantclientSQLPlus
	params["ymp_db_package"] = ympDBPackage

	// 环境变量
	if ympOracleEnvFile == "" {
		ympOracleEnvFile = fmt.Sprintf("/home/%s/.oracle", ympUser)
	}
	params["ymp_oracle_env_file"] = ympOracleEnvFile

	// 依赖包
	params["ymp_deps_packages"] = ympDepsPackages

	// 内置数据库模式
	params["ymp_db_mode"] = ympDBMode

	// 清理
	params["ymp_cleanup"] = ympCleanup

	return params
}

// getYMPRequiredOSSteps 返回YMP安装所需的OS步骤
// YMP作为迁移工具，不需要完整的数据库OS基线配置（如内核参数、多路径等）
// 但需要基础的系统配置：用户创建、资源限制、依赖包安装、主机名、时区、时间同步、防火墙
func getYMPRequiredOSSteps() []*runner.Step {
	allOSSteps := ossteps.GetAllSteps()
	requiredStepIDs := []string{
		"B-000", // 连通性检查（必须）
		// B-001~B-004 已移除：用户和组的创建在 YMP 步骤 H-001 中完成
		"B-005", // 时区配置（建议，确保时间正确）
		// B-008 已移除：用户资源限制配置在 YMP 步骤 H-002 中完成
		// B-010, B-011, B-012 已移除：H-004/H-006 内部通过 EnsureLocalISORepo 自行处理 ISO 挂载和 repo 配置，
		//   B-012 安装的是数据库依赖包（libzstd, lz4 等），YMP 不需要；YMP 自身依赖由 H-004（libaio, lsof）和 H-006（JDK）处理
		"B-013", // chrony 配置（建议，时间同步）
		"B-014", // 禁用防火墙（如果客户无特殊要求）
		"B-027", // 主机名配置（基础配置）
		// 注意：B-015（开放防火墙端口）可以通过 --include-steps 单独添加
	}

	var ympOSSteps []*runner.Step
	for _, step := range allOSSteps {
		for _, requiredID := range requiredStepIDs {
			if step.ID == requiredID {
				ympOSSteps = append(ympOSSteps, step)
				break
			}
		}
	}

	return ympOSSteps
}
