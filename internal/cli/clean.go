package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	"github.com/yinstall/internal/steps/clean"
)

// NewCleanCommand 创建 clean 子命令。
func NewCleanCommand() *cobra.Command {
	var (
		cleanType     string
		yasdbHome     string
		yasdbData     string
		yasdbLog      string
		clusterName   string
		osUser        string
		ycmHome       string
		ympHome       string
		ympUser       string
		cleanYACDisks string
		detailedSteps bool
		dbCleanPort   int
		ycmCleanPort  int
		ympCleanPort  int
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean YashanDB/YCM/YMP installations",
		Long: `Clean YashanDB/YCM/YMP installations by stopping processes and removing directories.

Supported cleanup types:
  - db:  Clean YashanDB installation (default). Paths align with yinstall db: non-default --db-port infers *_<port> dirs when paths not overridden.
  - ycm: Clean YCM installation. Non-default --ycm-port infers /opt/ycm_<port> when --ycm-home not set (same idea as db port suffix).
  - ymp: Clean YMP installation. Non-default --ymp-port infers /opt/ymp_<port> when --ymp-home not set.

Examples:
  # Clean YashanDB on multiple nodes (default type). For standby nodes, use the SAME --yasdb-home/--yasdb-data
  # as yinstall db / yinstall standby (--db-home-path / --db-data-path), or yasboot host add may fail with "should be empty".
  yinstall clean --targets 10.10.10.125,10.10.10.126 \
    --yasdb-home /data/yashan/yasdb_home --yasdb-data /data/yashan/yasdb_data \
    --yasdb-log /data/yashan/log --cluster-name yashandb

  # Clean YCM on single node
  yinstall clean -t ycm --targets 10.10.10.125 --ycm-home /opt/ycm

  # Clean YMP on multiple nodes
  yinstall clean -t ymp --targets 10.10.10.125,10.10.10.126 \
    --ymp-home /opt/ymp`,
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
			if cleanType != "db" && cleanType != "ycm" && cleanType != "ymp" {
				fmt.Fprintf(os.Stderr, "Error: invalid cleanup type: %s (must be db, ycm, or ymp)\n", cleanType)
				return fmt.Errorf("invalid cleanup type: %s (must be db, ycm, or ymp)", cleanType)
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
			}

			applyCleanPathInference(cmd, cleanType,
				dbCleanPort, &yasdbHome, &yasdbData, &yasdbLog, &clusterName,
				ycmCleanPort, &ycmHome,
				ympCleanPort, &ympHome,
			)

			// 创建 target hosts
			var targetHosts []runner.TargetHost
			for _, target := range parsedTargets {
				cfg := ssh.Config{
					Host:       target,
					Port:       globalFlags.SSHPort,
					User:       globalFlags.SSHUser,
					AuthMethod: globalFlags.SSHAuth,
					Password:   globalFlags.SSHPassword,
					KeyPath:    globalFlags.SSHKeyPath,
					Timeout:    30 * time.Second,
				}

				// 如果用户没有提供密码，使用fallback逻辑
				var exec ssh.Executor
				var err error

				// localhost targets：本地清理（不走 SSH）
				if isLocalHost(target) {
					cfg.AuthMethod = "local"
					exec, err = ssh.NewExecutor(cfg)
				} else if globalFlags.SSHPassword == "" {
					exec, err = ssh.NewExecutorWithFallback(cfg, globalFlags.SSHKeyPath)
				} else {
					exec, err = ssh.NewExecutor(cfg)
				}

				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to create SSH executor for %s: %v\n", target, err)
					return fmt.Errorf("failed to create SSH executor for %s: %w", target, err)
				}
				targetHosts = append(targetHosts, runner.TargetHost{
					Host:     target,
					Executor: &runnerExecAdapter{e: exec},
				})
			}

			// 决定要执行的清理步骤
			var steps []*runner.Step
			switch cleanType {
			case "db":
				if detailedSteps {
					// 使用详细步骤
					steps = clean.GetDBCleanSteps()
				} else {
					// 使用单一步骤
					steps = []*runner.Step{clean.GetStepByID("CLEAN-DB")}
				}
			case "ycm":
				steps = []*runner.Step{clean.GetStepByID("CLEAN-YCM")}
			case "ymp":
				steps = []*runner.Step{clean.GetStepByID("CLEAN-YMP")}
			}

			steps = filterSteps(steps, globalFlags)
			if len(steps) == 0 {
				fmt.Fprintf(os.Stderr, "Error: no cleanup steps to run for type %s after step filters (--include-steps / --exclude-steps / tags)\n", cleanType)
				return fmt.Errorf("no cleanup steps to run for type %s after step filters", cleanType)
			}

			// 构造参数 map
			params := make(map[string]interface{})
			params["yasdb_home"] = yasdbHome
			params["yasdb_data"] = yasdbData
			params["yasdb_log"] = yasdbLog
			params["db_cluster_name"] = clusterName
			params["os_user"] = osUser
			params["ycm_home"] = ycmHome
			params["ymp_home"] = ympHome
			params["ymp_user"] = ympUser
			params["clean_yac_disks"] = cleanYACDisks
			if cleanType == "db" {
				params["db_begin_port"] = dbCleanPort
			}

			// 初始化 cleanup 日志
			rid := fmt.Sprintf("clean-%s-%s", cleanType, time.Now().Format("20060102-150405"))
			logger, err := logging.NewLogger(rid, GetGlobalFlags().LogDir, AppVersion, AppAuthor, AppContact)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to initialize logger: %v\n", err)
				return fmt.Errorf("failed to initialize logger: %w", err)
			}
			defer logger.Close()

			// 在所有 target hosts 上执行清理
			fmt.Printf("Starting %s cleanup on %d target(s)...\n", strings.ToUpper(cleanType), len(targetHosts))
			logger.Info("Starting %s cleanup on %d target(s)...", strings.ToUpper(cleanType), len(targetHosts))

			for _, th := range targetHosts {
				fmt.Printf("\n=== Cleaning %s on %s ===\n", strings.ToUpper(cleanType), th.Host)
				logger.Info("=== Cleaning %s on %s ===", strings.ToUpper(cleanType), th.Host)

				// 针对当前主机构造 StepContext
				ctx := &runner.StepContext{
					Executor:    th.Executor,
					TargetHosts: []runner.TargetHost{th},
					Params:      params,
					Logger:      logger,
					DryRun:      globalFlags.DryRun,
					Precheck:    globalFlags.Precheck,
					ForceAll:    globalFlags.ForceAll,
					ForceSteps:  globalFlags.ForceSteps,
				}

				// 依次执行全部步骤
				precheckFailed := false
				for i, step := range steps {
					fmt.Printf("\n[%d/%d] Executing: %s\n", i+1, len(steps), step.Name)
					logger.Info("[%d/%d] Executing: %s", i+1, len(steps), step.Name)

					result := runner.RunStep(step, ctx)
					if !result.Success {
						// 判断是否为跳过（skip）而非失败
						if result.Skipped {
							fmt.Printf("[SKIP] %s skipped on %s: %v\n", step.Name, th.Host, result.Error)
							logger.Info("[SKIP] %s skipped on %s: %v", step.Name, th.Host, result.Error)
							continue
						}

						// precheck 模式：继续执行剩余步骤，最后再汇总错误并退出
						if globalFlags.Precheck {
							precheckFailed = true
							if result.Error != nil {
								fmt.Printf("[PRECHECK-FAIL] %s failed on %s: %v\n", step.Name, th.Host, result.Error)
								logger.Error("[PRECHECK-FAIL] %s failed on %s: %v", step.Name, th.Host, result.Error)
							} else {
								fmt.Printf("[PRECHECK-FAIL] %s failed on %s\n", step.Name, th.Host)
								logger.Error("[PRECHECK-FAIL] %s failed on %s", step.Name, th.Host)
							}
							// 继续执行后续步骤
							continue
						}

						if result.Error != nil {
							fmt.Printf("[ERROR] %s failed on %s: %v\n", step.Name, th.Host, result.Error)
							logger.Error("[ERROR] %s failed on %s: %v", step.Name, th.Host, result.Error)
							return result.Error
						}
						fmt.Printf("[ERROR] %s failed on %s\n", step.Name, th.Host)
						logger.Error("[ERROR] %s failed on %s", step.Name, th.Host)
						return fmt.Errorf("%s failed on %s", step.Name, th.Host)
					}

					fmt.Printf("[OK] %s completed on %s\n", step.Name, th.Host)
					logger.Info("[OK] %s completed on %s", step.Name, th.Host)
				}

				if globalFlags.Precheck && precheckFailed {
					return fmt.Errorf("precheck failed on %s", th.Host)
				}

				fmt.Printf("[OK] All cleanup tasks completed successfully on %s\n", th.Host)
			}

			fmt.Printf("\n=== All cleanup tasks completed successfully ===\n")
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
	cmd.Flags().StringVar(&osUser, "os-user", "yashan", "OS user for YashanDB installation (for DB cleanup)")
	cmd.Flags().StringVar(&cleanYACDisks, "clean-yac-disks", "", "Clean YAC shared disks: 'auto' to query via ycsctl, or comma-separated paths like '/dev/mapper/sys1,/dev/mapper/sys2'")
	cmd.Flags().BoolVar(&detailedSteps, "detailed-steps", false, "Use detailed cleanup steps (DB only, allows step-by-step execution)")
	cmd.Flags().IntVar(&dbCleanPort, "db-port", 1688, "Database begin port (for DB cleanup): like yinstall db, non-default port infers yasdb_home/data/log_* and cluster name unless paths explicitly set")

	// YCM 专用 flags
	cmd.Flags().StringVar(&ycmHome, "ycm-home", "/opt/ycm", "YCM installation directory (for YCM cleanup, default: /opt/ycm)")
	cmd.Flags().IntVar(&ycmCleanPort, "ycm-port", 9060, "YCM web port: when not default (9060) and --ycm-home unchanged, infer /opt/ycm_<port>")

	// YMP 专用 flags
	cmd.Flags().StringVar(&ympHome, "ymp-home", "/opt/ymp", "YMP installation directory (for YMP cleanup, default: /opt/ymp)")
	cmd.Flags().IntVar(&ympCleanPort, "ymp-port", 8090, "YMP web port: when not default (8090) and --ymp-home unchanged, infer /opt/ymp_<port>")
	cmd.Flags().StringVar(&ympUser, "ymp-user", "ymp", "YMP user name (for YMP cleanup, default: ymp)")

	return cmd
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
	}
}
