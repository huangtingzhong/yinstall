package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	commonmssql "github.com/yinstall/internal/common/mssql"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	"github.com/yinstall/internal/steps/clean"
)

// NewCleanCommand 创建 clean 子命令。
func NewCleanCommand() *cobra.Command {
	var (
		cleanType              string
		yasdbHome              string
		yasdbData              string
		yasdbLog               string
		clusterName            string
		osUser                 string
		ycmHome                string
		ympHome                string
		ympUser                string
		cleanYACDisks          string
		cleanEnvFile           string
		dbCleanPort            int
		mysqlCleanPort         int
		mysqlCleanBase         string
		mysqlCleanPackage      string
		mysqlCleanVersion      string
		mysqlCleanStage        string
		mssqlCleanPort         string
		mssqlCleanDataRoot     string
		mssqlCleanSQLDataDir   string
		mssqlCleanSQLLogDir    string
		mssqlCleanSQLBackupDir string
		mssqlCleanProgramDir   string
		mssqlCleanInstanceDir  string
		mssqlCleanDatabase     string
		mssqlCleanData         string
		mssqlCleanLog          string
		mssqlCleanBackup       string
		mssqlCleanInstance     string
		ycmCleanPort           int
		ycmCleanServiceName    string
		ympCleanPort           int
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean YashanDB/YCM/YMP installations",
		Long: `Clean YashanDB/YCM/YMP installations by stopping processes and removing directories.

Supported cleanup types:
  - db:  Clean YashanDB installation (default). Paths align with yinstall db: non-default --db-port infers *_<port> dirs when paths not overridden.
  - ycm: Clean YCM installation. Non-default --ycm-port infers /opt/ycm_<port> when --ycm-home not set (same idea as db port suffix).
  - ymp: Clean YMP installation. Non-default --ymp-port infers /opt/ymp_<port> when --ymp-home not set.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 获取全局参数
			globalFlags := GetGlobalFlags()
			if globalFlags.ListSteps {
				PrintCleanStepCatalog()
				return nil
			}

			// 校验并规范化清理类型
			cleanType = strings.ToLower(cleanType)
			if cleanType != "db" && cleanType != "ycm" && cleanType != "ymp" && cleanType != "mysql" && cleanType != "mssql" {
				fmt.Fprintf(os.Stderr, "Error: invalid cleanup type: %s (must be db, ycm, ymp, mysql, or mssql)\n", cleanType)
				return fmt.Errorf("invalid cleanup type: %s (must be db, ycm, ymp, mysql, or mssql)", cleanType)
			}

			if len(globalFlags.Targets) == 0 {
				// 未指定 --targets 时，默认本地执行（与 db/os/ycm/ymp 一致）。
				globalFlags.Local = true
				globalFlags.Targets = []string{"localhost"}
			}

			// 解析 targets：支持逗号分隔的 IPs
			var parsedTargets []string
			for _, target := range globalFlags.Targets {
				// 按逗号切分并去除空白
				ips := strings.Split(target, ",")
				for _, ip := range ips {
					ip = strings.TrimSpace(ip)
					if ip != "" {
						parsedTargets = append(parsedTargets, ip)
					}
				}
			}

			if len(parsedTargets) == 0 {
				fmt.Fprintf(os.Stderr, "Error: no valid target IP addresses provided\n")
				return fmt.Errorf("no valid target IP addresses provided")
			}

			// 校验不同类型的参数与端口
			switch cleanType {
			case "db":
				if err := validatePort("--db-port", dbCleanPort); err != nil {
					return err
				}
			case "ycm":
				if err := validatePort("--ycm-port", ycmCleanPort); err != nil {
					return err
				}
			case "ymp":
				if err := validatePort("--ymp-port", ympCleanPort); err != nil {
					return err
				}
			case "mysql":
				if err := validatePort("--mysql-port", mysqlCleanPort); err != nil {
					return err
				}
				if err := validateMysqlCleanStage(cleanType, mysqlCleanStage, cmd.Flags().Changed("stage")); err != nil {
					return err
				}
				cleanStage, err := commonmysql.ParseStage(mysqlCleanStage)
				if err != nil {
					return err
				}
				if cleanStage == commonmysql.StageSoftware && strings.TrimSpace(mysqlCleanVersion) == "" && strings.TrimSpace(mysqlCleanPackage) == "" {
					return fmt.Errorf("--mysql-version or --mysql-package is required when --stage is software")
				}
				applyMysqlPlatformDefaults(cmd, &globalFlags, &mysqlCleanBase)
			case "mssql":
				if _, err := commonmssql.NormalizePortParam(mssqlCleanPort); err != nil {
					return err
				}
				if _, err := mssqlCleanStageFromFlag(cmd, mysqlCleanStage); err != nil {
					return err
				}
				if err := applyMssqlLocalDefaults(&globalFlags); err != nil {
					return err
				}
				applyMssqlRemoteSoftwareDefaults(cmd, &globalFlags)
			}

			applyCleanPathInference(cmd, cleanType,
				dbCleanPort, &yasdbHome, &yasdbData, &yasdbLog, &clusterName,
				ycmCleanPort, &ycmHome,
				ympCleanPort, &ympHome,
			)

			// 初始化 cleanup 日志（建连前，与 db/os 一致以便记录 SSH 重试）
			rid := fmt.Sprintf("clean-%s-%s", cleanType, time.Now().Format("20060102-150405"))
			logger, err := newSessionLogger(rid, GetGlobalFlags().LogDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to initialize logger: %v\n", err)
				return fmt.Errorf("failed to initialize logger: %w", err)
			}
			defer logger.Close()

			// 创建目标主机连接（复用 createExecutor：key fallback + 重试）
			var hostInfos []*HostInfo
			for _, target := range parsedTargets {
				var exec ssh.Executor
				var err error
				if cleanType == "mssql" {
					exec, err = createWindowsExecutor(target, globalFlags, logger, "")
				} else {
					exec, err = createExecutor(target, globalFlags, logger, "")
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to create SSH executor for %s: %v\n", target, err)
					return fmt.Errorf("failed to create SSH executor for %s: %w", target, err)
				}
				hostInfos = append(hostInfos, &HostInfo{
					Host:           target,
					Executor:       exec,
					TargetPlatform: inferCleanTargetPlatform(cleanType, globalFlags),
				})
			}

			// 决定要执行的清理步骤
			var steps []*runner.Step
			switch cleanType {
			case "db":
				steps = clean.GetDBCleanSteps()
			case "ycm":
				steps = []*runner.Step{clean.GetStepByID("CLEAN-YCM")}
			case "ymp":
				steps = []*runner.Step{clean.GetStepByID("CLEAN-YMP")}
			case "mysql":
				steps = clean.GetMysqlCleanSteps()
			case "mssql":
				steps = clean.GetMssqlCleanSteps()
			}

			steps = filterSteps(steps, globalFlags)
			if len(steps) == 0 {
				fmt.Fprintf(os.Stderr, "Error: no cleanup steps to run for type %s after step filters (--include-steps / --exclude-steps / tags)\n", cleanType)
				return fmt.Errorf("no cleanup steps to run for type %s after step filters", cleanType)
			}

			// 构造参数 map
			params := make(map[string]interface{})
			params["sudo"] = globalFlags.UseSudo
			params["local_mode"] = globalFlags.Local
			params["yasdb_home"] = yasdbHome
			params["yasdb_data"] = yasdbData
			params["yasdb_log"] = yasdbLog
			params["db_cluster_name"] = clusterName
			params["os_user"] = osUser
			params["ycm_home"] = ycmHome
			if cleanType == "ycm" {
				params["ycm_port"] = ycmCleanPort
				params["ycm_service_name"] = ycmCleanServiceName
			}
			params["ymp_home"] = ympHome
			params["ymp_user"] = ympUser
			params["clean_yac_disks"] = cleanYACDisks
			if cleanType == "db" {
				params["yac_mode"] = yacMode
				params["db_begin_port"] = dbCleanPort
				if strings.TrimSpace(cleanEnvFile) != "" {
					params["clean_env_file"] = cleanEnvFile
				}
			}
			if cleanType == "mysql" {
				params["mysql_port"] = mysqlCleanPort
				params["mysql_base"] = mysqlCleanBase
				params["mysql_package"] = mysqlCleanPackage
				params["mysql_version"] = mysqlCleanVersion
				cleanStage, err := commonmysql.ParseStage(mysqlCleanStage)
				if err != nil {
					return err
				}
				params["mysql_stage"] = cleanStage
				mysqlUser := osUser
				if !cmd.Flags().Changed("os-user") {
					mysqlUser = "mysql"
				}
				params["os_user"] = mysqlUser
			}
			if cleanType == "mssql" {
				portParam, err := commonmssql.NormalizePortParam(mssqlCleanPort)
				if err != nil {
					return err
				}
				params["mssql_port"] = portParam
				cleanDataRoot := mssqlFirstNonEmpty(mssqlCleanDataRoot, mssqlCleanDatabase)
				cleanDataDir := mssqlFirstNonEmpty(mssqlCleanSQLDataDir, mssqlCleanData)
				cleanLogDir := mssqlFirstNonEmpty(mssqlCleanSQLLogDir, mssqlCleanLog)
				cleanBackupDir := mssqlFirstNonEmpty(mssqlCleanSQLBackupDir, mssqlCleanBackup)
				params["mssql_data_root"] = cleanDataRoot
				params["mssql_database"] = cleanDataRoot
				params["mssql_data_dir"] = cleanDataDir
				params["mssql_data"] = cleanDataDir
				params["mssql_log_dir"] = cleanLogDir
				params["mssql_log"] = cleanLogDir
				params["mssql_backup_dir"] = cleanBackupDir
				params["mssql_backup"] = cleanBackupDir
				params["mssql_program_dir"] = strings.TrimSpace(mssqlCleanProgramDir)
				params["mssql_instance_dir"] = strings.TrimSpace(mssqlCleanInstanceDir)
				params["mssql_instance"] = mssqlCleanInstance
				params["windows_transport"] = "auto"
				cleanStage, err := mssqlCleanStageFromFlag(cmd, mysqlCleanStage)
				if err != nil {
					return err
				}
				params["mssql_stage"] = cleanStage
			}

			defer func() {
				for _, info := range hostInfos {
					info.Executor.Close()
				}
			}()

			progress := runner.NewStepProgress(runner.CountNonOptionalSteps(steps))
			logger.Info("======== %s cleanup on %d target(s) ========", strings.ToUpper(cleanType), len(hostInfos))

			phaseResult := RunPerHostStepsEx(steps, hostInfos, params, globalFlags, logger, 0, progress.Total(), nil, nil, progress)
			if phaseResult.PrecheckFailed {
				return fmt.Errorf("precheck failed during %s cleanup", cleanType)
			}
			if phaseResult.LastError != nil {
				return phaseResult.LastError
			}

			logger.Info("All %s cleanup tasks completed successfully", cleanType)
			return nil
		},
	}

	// 注册 flags
	cmd.Flags().StringVar(&cleanType, "type", "db", "Cleanup type: db, ycm, or ymp (default: db)")

	// DB 专用 flags
	cmd.Flags().StringVar(&yasdbHome, "yasdb-home", "/data/yashan/yasdb_home", "YashanDB installation directory (for DB cleanup)")
	cmd.Flags().StringVar(&yasdbData, "yasdb-data", "/data/yashan/yasdb_data", "YashanDB data directory (for DB cleanup)")
	cmd.Flags().StringVar(&yasdbLog, "yasdb-log", "/data/yashan/log", "YashanDB log directory (for DB cleanup)")
	cmd.Flags().StringVar(&clusterName, "cluster-name", "yashandb", "YashanDB cluster name (for DB cleanup)")
	cmd.Flags().MarkHidden("cluster-name")
	cmd.Flags().StringVar(&osUser, "os-user", "yashan", "OS user for YashanDB installation (for DB cleanup)")
	cmd.Flags().StringVar(&cleanEnvFile, "env-file", "", "Explicit path to YashanDB env file on remote host (auto-discovered if omitted; sourced before DB commands)")
	cmd.Flags().StringVar(&cleanYACDisks, "clean-yac-disks", "", "Clean YAC shared disks: 'auto' to query via ycsctl, or comma-separated paths like '/dev/mapper/sys1,/dev/mapper/sys2'")
	registerYACModeFlag(cmd)
	cmd.Flags().IntVar(&dbCleanPort, "db-port", 1688, "Database begin port (for DB cleanup): like yinstall db, non-default port infers yasdb_home/data/log_* and cluster name unless paths explicitly set")

	// YCM 专用 flags
	cmd.Flags().StringVar(&ycmHome, "ycm-home", "/opt/ycm", "YCM installation directory (for YCM cleanup, default: /opt/ycm)")
	cmd.Flags().IntVar(&ycmCleanPort, "ycm-port", 9060, "YCM web port: when not default (9060) and --ycm-home unchanged, infer /opt/ycm_<port>")
	cmd.Flags().StringVar(&ycmCleanServiceName, "ycm-service-name", "", "systemd unit to remove (default: derived from --ycm-port and --ycm-home)")

	// YMP 专用 flags
	cmd.Flags().StringVar(&ympHome, "ymp-home", "/opt/ymp", "YMP installation directory (for YMP cleanup, default: /opt/ymp)")
	cmd.Flags().IntVar(&ympCleanPort, "ymp-port", 8090, "YMP web port: when not default (8090) and --ymp-home unchanged, infer /opt/ymp_<port>")
	cmd.Flags().StringVar(&ympUser, "ymp-user", "ymp", "YMP user name (for YMP cleanup, default: ymp)")

	cmd.Flags().IntVar(&mysqlCleanPort, "mysql-port", 3306, "MySQL port (for MySQL cleanup)")
	cmd.Flags().StringVar(&mysqlCleanBase, "mysql-base", "/mysql/app/mysql", "MySQL base directory (for MySQL cleanup)")
	cmd.Flags().StringVar(&mysqlCleanPackage, "mysql-package", "", "MySQL package path used to infer version for cleanup layout")
	cmd.Flags().StringVar(&mysqlCleanVersion, "mysql-version", "", "MySQL version for cleanup layout (optional if --mysql-package set)")
	cmd.Flags().StringVar(&mysqlCleanStage, "stage", commonmysql.DefaultCleanStage(), "Cleanup stage: mysql instance/i|software/s|all/a; mssql all/a|software/s (default all, keeps ISO under -R)")
	for _, name := range []string{"mysql-port", "mysql-base", "mysql-package", "mysql-version", "stage"} {
		if f := cmd.Flags().Lookup(name); f != nil {
			f.Hidden = true
		}
	}
	cmd.Flags().StringVar(&mssqlCleanPort, "mssql-port", commonmssql.PortAuto, "MSSQL port (auto or 1-65535; for MSSQL cleanup)")
	cmd.Flags().StringVar(&mssqlCleanDataRoot, "mssql-data-root", "", "Database files root (for MSSQL cleanup)")
	cmd.Flags().StringVar(&mssqlCleanSQLDataDir, "mssql-data-dir", "", "User database directory to clean")
	cmd.Flags().StringVar(&mssqlCleanSQLLogDir, "mssql-log-dir", "", "Transaction log directory to clean")
	cmd.Flags().StringVar(&mssqlCleanSQLBackupDir, "mssql-backup-dir", "", "Backup directory to clean")
	cmd.Flags().StringVar(&mssqlCleanProgramDir, "mssql-program-dir", "", "SQL program root to clean")
	cmd.Flags().StringVar(&mssqlCleanInstanceDir, "mssql-instance-dir", "", "SQL instance program directory to clean")
	cmd.Flags().StringVar(&mssqlCleanDatabase, "database", "", "Deprecated: use --mssql-data-root")
	cmd.Flags().StringVar(&mssqlCleanData, "data", "", "Deprecated: use --mssql-data-dir")
	cmd.Flags().StringVar(&mssqlCleanLog, "log", "", "Deprecated: use --mssql-log-dir")
	cmd.Flags().StringVar(&mssqlCleanBackup, "backup", "", "Deprecated: use --mssql-backup-dir")
	cmd.Flags().StringVar(&mssqlCleanInstance, "mssql-instance", commonmssql.InstanceAuto, "MSSQL instance name (auto or name); auto discovers from registry (single instance) or by --mssql-port")
	for _, name := range []string{
		"mssql-port", "mssql-data-root", "mssql-data-dir", "mssql-log-dir", "mssql-backup-dir",
		"mssql-program-dir", "mssql-instance-dir",
		"database", "data", "log", "backup", "mssql-instance",
	} {
		if f := cmd.Flags().Lookup(name); f != nil {
			f.Hidden = true
		}
	}
	cmd.Example = `  # Clean YashanDB on multiple nodes (default type)
  yinstall clean --targets 10.10.10.125,10.10.10.126

  # Clean YashanDB with non-default port (auto-infers yasdb_home_2688 etc.)
  yinstall clean --db-port 2688 --targets 10.10.10.125

  # Single-node YAC cleanup (explicit --yac; shared disks via auto or manual paths)
  yinstall clean -t 10.10.10.125 --yac --clean-yac-disks auto \
    --yasdb-home /data/yashan/yasdb_home/23.4.7.106 --yasdb-data /data/yashan/yasdb_data

  # Clean YCM on single node
  yinstall clean -t ycm --targets 10.10.10.125 --ycm-home /opt/ycm

  # Clean YMP on multiple nodes
  yinstall clean -t ymp --targets 10.10.10.125,10.10.10.126 \
    --ymp-home /opt/ymp`

	return cmd
}

func inferCleanTargetPlatform(cleanType string, flags GlobalFlags) string {
	if cleanType == "mssql" {
		return "windows"
	}
	return inferTargetPlatformFromFlags(flags)
}

// applyCleanPathInference 与 yinstall db 一致：非默认端口且未显式覆盖 flag 时，推断 home/data/log/cluster 路径。
// YCM：非默认 YCM Web 端口且未指定 --ycm-home 时推断 /opt/ycm_<port>。YMP：非默认 YMP 端口时推断 /opt/ymp_<port>。
func applyCleanPathInference(cmd *cobra.Command, cleanType string,
	dbPort int,
	yasdbHome, yasdbData, yasdbLog, clusterName *string,
	ycmPort int, ycmHome *string,
	ympPort int, ympHome *string,
) {
	switch cleanType {
	case "db":
		if dbPort != 1688 {
			if !cmd.Flags().Changed("yasdb-home") {
				*yasdbHome = fmt.Sprintf("/data/yashan/yasdb_home_%d", dbPort)
			}
			if !cmd.Flags().Changed("yasdb-data") {
				*yasdbData = fmt.Sprintf("/data/yashan/yasdb_data_%d", dbPort)
			}
			if !cmd.Flags().Changed("yasdb-log") {
				*yasdbLog = fmt.Sprintf("/data/yashan/log_%d", dbPort)
			}
			if !cmd.Flags().Changed("cluster-name") {
				*clusterName = fmt.Sprintf("yashandb_%d", dbPort)
			}
		}
	case "ycm":
		if ycmPort != 9060 && !cmd.Flags().Changed("ycm-home") {
			*ycmHome = fmt.Sprintf("/opt/ycm_%d", ycmPort)
		}
	case "ymp":
		if ympPort != 8090 && !cmd.Flags().Changed("ymp-home") {
			*ympHome = fmt.Sprintf("/opt/ymp_%d", ympPort)
		}
		// mysql: base/home 与端口无关；data/other 由 ResolveLayout 写入 oradata/{port}/（见 common/mysql/mysql.go）
	}
}

func mssqlCleanStageFromFlag(cmd *cobra.Command, raw string) (string, error) {
	if !cmd.Flags().Changed("stage") {
		raw = commonmssql.DefaultCleanStage()
	}
	return commonmssql.ParseStage(raw)
}
