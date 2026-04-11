// ycm.go - YCM 安装命令实现
// 本文件实现 yinstall ycm 命令，用于安装 YCM（YashanDB Cloud Manager）
// 流程：OS 基线配置（可选）→ YCM 安装步骤（G-001 ~ G-010）

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
	ycmsteps "github.com/yinstall/internal/steps/ycm"
)

var (
	// YCM OS 控制
	ycmSkipOS              bool   // 是否跳过 OS 基线配置，默认 true
	ycmIgnoreInstallErrors bool   // 忽略软件包安装错误
	ycmOSYumMode           string // YUM 模式：online/local-iso/none
	ycmOSISODevice         string // ISO 文件路径、文件名或块设备
	ycmOSISOMountpoint     string // ISO 挂载目录
	ycmOSYumRepoFile       string // YUM repo 文件路径

	// YCM OS 用户参数
	ycmOSUser         string
	ycmOSUserPassword string
	ycmOSGroup        string

	// YCM 安装参数
	ycmPackage    string // YCM 安装包路径/文件名（必填）
	ycmInstallDir string // 安装目录（默认 /opt）
	ycmDeployFile string // deploy.yml 路径

	// YCM 端口参数
	ycmPort              int
	ycmPrometheusPort    int
	ycmLokiHTTPPort      int
	ycmLokiGRPCPort      int
	ycmYasdbExporterPort int

	// YCM 数据库后端参数
	ycmDBDriver        string // sqlite3 或 yashandb
	ycmDBURL           string // YashanDB 连接 URL
	ycmDBLibPath       string // 客户端 lib 路径
	ycmDBAdminUser     string // 数据库管理员用户名
	ycmDBAdminPassword string // 数据库管理员密码

	// YCM 依赖包
	ycmDepsPackages string
)

var ycmCmd = &cobra.Command{
	Use:   "ycm",
	Short: "Install YCM (YashanDB Cloud Manager)",
	Long: `Install YCM (YashanDB Cloud Manager):
  - OS baseline preparation (optional, controlled by --skip-os, default: skip)
    - Create user and groups
    - Configure sudo
    - Set timezone and NTP
    - Disable/configure firewall
    - Mount ISO and install dependencies
  - Install YCM dependencies (libnsl)
  - Extract YCM package
  - Set directory ownership
  - Verify deploy configuration
  - Configure ports
  - Check port availability
  - Deploy YCM (sqlite3 or yashandb backend)
  - Verify processes, ports and web access`,
	RunE:         runYCM,
	SilenceUsage: true,
}

func init() {
	// OS 控制
	ycmCmd.Flags().BoolVar(&ycmSkipOS, "skip-os", true, "Skip OS baseline preparation (default: true)")
	ycmCmd.Flags().BoolVar(&ycmIgnoreInstallErrors, "os-ignore-install-errors", false, "Ignore package installation errors and continue (only show warnings)")
	ycmCmd.Flags().StringVar(&ycmOSYumMode, "os-yum-mode", "none", "YUM/DNF mode: none (use system repos), local-iso (use mounted ISO repo only), online (internet repos)")
	ycmCmd.Flags().StringVar(&ycmOSISODevice, "os-iso-device", "/dev/cdrom", "ISO file path/name or block device used when --os-yum-mode=local-iso (auto-searched if filename only)")
	ycmCmd.Flags().StringVar(&ycmOSISOMountpoint, "os-iso-mountpoint", "/media", "Mount point for ISO when --os-yum-mode=local-iso")
	ycmCmd.Flags().StringVar(&ycmOSYumRepoFile, "os-yum-repo-file", "/etc/yum.repos.d/local.repo", "YUM repo file path for local-iso mode")

	// OS 用户参数
	ycmCmd.Flags().StringVar(&ycmOSUser, "os-user", "yashan", "Product user name")
	ycmCmd.Flags().StringVar(&ycmOSUserPassword, "os-user-password", "aaBB11@@33$$", "Product user password")
	ycmCmd.Flags().StringVar(&ycmOSGroup, "os-group", "yashan", "Primary group name")

	// YCM 安装参数
	ycmCmd.Flags().StringVar(&ycmPackage, "ycm-package", "", "YCM installation package path (optional, auto-searched if not specified)")
	ycmCmd.Flags().StringVar(&ycmInstallDir, "ycm-install-dir", "/opt", "YCM installation directory")
	ycmCmd.Flags().StringVar(&ycmDeployFile, "ycm-deploy-file", "", "YCM deploy config file path (default: <ycm-install-dir>/ycm/etc/deploy.yml)")

	// YCM 端口参数
	ycmCmd.Flags().IntVar(&ycmPort, "ycm-port", 9060, "YCM Web service port")
	ycmCmd.Flags().IntVar(&ycmPrometheusPort, "ycm-prometheus-port", 9061, "Prometheus port")
	ycmCmd.Flags().IntVar(&ycmLokiHTTPPort, "ycm-loki-http-port", 9062, "Loki HTTP port")
	ycmCmd.Flags().IntVar(&ycmLokiGRPCPort, "ycm-loki-grpc-port", 9063, "Loki gRPC port")
	ycmCmd.Flags().IntVar(&ycmYasdbExporterPort, "ycm-yasdb-exporter-port", 9064, "YasDB exporter port")

	// YCM 数据库后端参数
	ycmCmd.Flags().StringVar(&ycmDBDriver, "ycm-db-driver", "sqlite3", "Database backend (sqlite3 or yashandb)")
	ycmCmd.Flags().StringVar(&ycmDBURL, "ycm-db-url", "", "YashanDB connection URL (required when driver=yashandb)")
	ycmCmd.Flags().StringVar(&ycmDBLibPath, "ycm-db-lib-path", "", "YashanDB client lib path (optional)")
	ycmCmd.Flags().StringVar(&ycmDBAdminUser, "ycm-db-admin-user", "yasman", "YashanDB admin username")
	ycmCmd.Flags().StringVar(&ycmDBAdminPassword, "ycm-db-admin-password", "", "YashanDB admin password (required when driver=yashandb)")

	// YCM 依赖包
	ycmCmd.Flags().StringVar(&ycmDepsPackages, "ycm-deps-packages", "libnsl", "YCM dependency packages")
}

// runYCM 执行 YCM 安装流程
func runYCM(cmd *cobra.Command, args []string) error {
	if err := validatePorts(map[string]int{
		"--ycm-port":                ycmPort,
		"--ycm-prometheus-port":     ycmPrometheusPort,
		"--ycm-loki-http-port":      ycmLokiHTTPPort,
		"--ycm-loki-grpc-port":      ycmLokiGRPCPort,
		"--ycm-yasdb-exporter-port": ycmYasdbExporterPort,
	}); err != nil {
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

	// In local mode, do not inject default os-user-password unless explicitly set by user.
	// This avoids unnecessary "login credential" parameters in local execution.
	if flags.Local && !cmd.Flags().Changed("os-user-password") {
		ycmOSUserPassword = ""
	}

	// ycm_package 可以为空，会在 PreCheck 阶段自动寻找最新版本

	// YashanDB 模式参数校验
	if ycmDBDriver == "yashandb" {
		if ycmDBURL == "" {
			return fmt.Errorf("--ycm-db-url is required when --ycm-db-driver=yashandb")
		}
		if ycmDBAdminPassword == "" && !flags.DryRun && !flags.Precheck {
			return fmt.Errorf("--ycm-db-admin-password is required when --ycm-db-driver=yashandb")
		}
	}

	// 初始化日志
	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("ycm-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting YCM installation (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)
	logger.Info("YCM package: %s", ycmPackage)
	logger.Info("Install directory: %s", ycmInstallDir)
	logger.Info("Database driver: %s", ycmDBDriver)

	if ycmSkipOS {
		logger.Info("OS baseline preparation: SKIPPED")
	} else {
		logger.Info("OS baseline preparation: ENABLED")
	}

	// 构建参数
	params := buildYCMParams(flags)

	// 收集所有步骤
	var allSteps []*runner.Step

	// 添加 OS 步骤（如果启用）
	if !ycmSkipOS {
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

	// 添加 YCM 步骤
	ycmSteps := ycmsteps.GetAllSteps()
	allSteps = append(allSteps, ycmSteps...)

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

	// 分离 OS 步骤和 YCM 步骤
	var osStepsToRun []*runner.Step
	var ycmStepsToRun []*runner.Step
	for _, step := range otherSteps {
		if strings.HasPrefix(step.ID, "B-") {
			osStepsToRun = append(osStepsToRun, step)
		} else {
			ycmStepsToRun = append(ycmStepsToRun, step)
		}
	}

	var lastErr error

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
					Results:           make(map[string]interface{}),
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

	// YCM 步骤：遍历所有节点执行
	if lastErr == nil && len(ycmStepsToRun) > 0 {
		for _, info := range hostInfos {
			logger.Info("-------- Host: %s (YCM install) --------", info.Host)

			for i, step := range ycmStepsToRun {
				ctx := &runner.StepContext{
					Executor:          &runnerExecAdapter{e: info.Executor},
					Logger:            logger,
					Params:            params,
					DryRun:            flags.DryRun,
					Precheck:          flags.Precheck,
					Results:           make(map[string]interface{}),
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
		logger.Error("YCM installation completed with errors")
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return lastErr
	}

	// 输出访问信息
	for _, info := range hostInfos {
		logger.Info("YCM access URL: http://%s:%d", info.Host, ycmPort)
	}
	logger.Info("Default credentials: admin / admin (change on first login)")
	logger.Info("YCM installation completed successfully")
	return nil
}

// buildYCMParams 构建 YCM 安装参数
func buildYCMParams(flags GlobalFlags) map[string]interface{} {
	// 复用 OS 参数构建
	params := buildOSParams(false, len(flags.Targets))

	// 覆盖 OS 用户参数（YCM 可能用不同用户）
	if ycmOSUser != "" {
		params["os_user"] = ycmOSUser
	}
	if ycmOSUserPassword != "" {
		params["os_user_password"] = ycmOSUserPassword
	}
	if ycmOSGroup != "" {
		params["os_group"] = ycmOSGroup
	}

	// Override OS ignore install errors if specified
	params["os_ignore_install_errors"] = ycmIgnoreInstallErrors
	// YUM 模式及 ISO 参数覆盖（与 db --os-yum-mode 对齐）
	params["os_yum_mode"] = ycmOSYumMode
	params["os_iso_device"] = ycmOSISODevice
	params["os_iso_mountpoint"] = ycmOSISOMountpoint
	params["os_yum_repo_file"] = ycmOSYumRepoFile

	// YCM 安装参数
	params["ycm_package"] = ycmPackage
	params["ycm_install_dir"] = ycmInstallDir
	// ycm_deploy_file: 若用户未指定，动态根据 ycm_install_dir 计算
	deployFile := ycmDeployFile
	if deployFile == "" {
		deployFile = ycmInstallDir + "/ycm/etc/deploy.yml"
	}
	params["ycm_deploy_file"] = deployFile

	// YCM 端口参数
	params["ycm_port"] = ycmPort
	params["ycm_prometheus_port"] = ycmPrometheusPort
	params["ycm_loki_http_port"] = ycmLokiHTTPPort
	params["ycm_loki_grpc_port"] = ycmLokiGRPCPort
	params["ycm_yasdb_exporter_port"] = ycmYasdbExporterPort

	// YCM 数据库后端参数
	params["ycm_db_driver"] = ycmDBDriver
	params["ycm_db_url"] = ycmDBURL
	params["ycm_db_lib_path"] = ycmDBLibPath
	params["ycm_db_admin_user"] = ycmDBAdminUser
	params["ycm_db_admin_password"] = ycmDBAdminPassword

	// YCM 依赖包参数
	params["ycm_deps_packages"] = ycmDepsPackages

	return params
}
