// standby.go - 添加备库命令实现
// 本文件实现 yinstall standby 命令，用于在已有主库基础上新增备库节点

package cli

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	dbsteps "github.com/yinstall/internal/steps/db"
	omsteps "github.com/yinstall/internal/steps/om"
	ossteps "github.com/yinstall/internal/steps/os"
	standbysteps "github.com/yinstall/internal/steps/standby"
)

func standbyStepID(name string) string {
	return standbysteps.StepIDByName(name)
}

var (
	// 主库连接参数
	primaryIP              string // 主库 IP(可省略: 从 cluster status 自动发现)
	primarySSHUser         string // 主库 SSH 用户名
	primarySSHPassword     string // 主库 SSH 密码
	primarySSHKey          string // 主库 SSH 私钥路径
	omIP                   string // OM 主机 IP(可省略: 从 om_addr 自动发现); SSH 凭证用全局参数
	omCurrent              string // --om-current: 当前主 OM；与 omNew 成对启用迁主
	omNew                  string // --om-new: 目标主 OM
	omDeploySecondary      bool   // --om-secondary: 扩备后在目标节点拉起备 OM（默认 true）
	omDeploySecondaryScope string // --om-secondary-scope: targets|cluster
	omNewSSHUser           string
	omNewSSHPassword       string
	omNewSSHKey            string

	// 主库数据库用户和环境变量参数
	primaryOSUser  string // 主库运行 yashan 的用户，默认 yashan
	primaryEnvFile string // 主库环境变量文件路径，默认 .bashrc（相对用户家目录）或自动检测

	// 操作系统配置控制（OS 用户/基线参数与 db 共用 os.go 包级变量，见 registerAllOSFlags）
	skipOS bool // 是否跳过备库操作系统配置，默认 true

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
	standbyRestartPrimary   bool // CE：改 REPLICATION_ADDR 后是否允许整集群重启；默认 false（生产安全）
	standbySyncTimeoutSec   int  // E-014 等待 standby 角色出现的超时秒数；0=软成功（旧行为）

	// 多实例支持（YAC 模式使用 yacMode，与 db/clean 共用 registerYACModeFlag）
	standbyBeginPort int // 数据库起始端口（用于多实例场景的环境变量文件命名）

	standbyYasbootGenExtraArgs string // 追加到 yasboot config node gen 的额外参数
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
		logger.Info("OM stage directory (--db-stage-dir): %s", params["db_stage_dir"])
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
	logger.Info("OM stage directory (default): %s", def)
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
	Short: "Add standby node(s) or a CE standby group to an existing primary",
	Long: `Add standby database capacity to an existing primary.

Paths:
  - SE primary (default): yasboot config node gen + host add + node add (single/multi standby nodes).
  - CE/YAC primary with --yac: yasboot config group gen -t ce + host add + group add
    (N-node YAC standby or single-node YAC standby). Requires --yac-inter-cidr, --yac-systemdg,
    --yac-datadg, --yac-vips (VIP count must equal standby node count).
    --yac-*dg values are comma-separated disk paths (e.g. /dev/yfs/sys1,/dev/yfs/sys2);
    legacy role:/dev/... is still accepted. YFS diskgroup names stay SYSTEM/DG0 (not renameable).

Topology:
  - OM host: yasom + stage dir; yasboot gen/host add/node|group add run here (SSH uses global -u/-p/--ssh-port).
  - Primary host: read-write database; archive SQL and primary-side checks run here (--primary-ip or auto-discover).
  - When OM and primary differ (e.g. after failover), provide --primary-ip for the current primary;
    OM IP is auto-read from om_addr (or set global -M/--om). At least one of --primary-ip / --om is required.

Typical flow:
  - Phase 1: primary connectivity (E-001) + OM status/stage (E-002) + primary SQL checks (E-003–E-004)
  - Phase 1.5 (optional): migrate primary OM when --om-new is set with --om-current or global --om
    (OS/B-* on --om-new first when present in filtered steps, then O-*)
  - Phase 2 (standby): optional OS baseline on --targets when --skip-os=false
  - Phase 3–4: OM expansion (E-010–E-013) + primary sync check (E-014)
  - Phase 4.5 (default): deploy secondary yasom on --targets (--om-secondary; disable with --om-secondary=false)
  - Phase 5–6: standby env/autostart + cluster summary

--standby-node-count defaults to len(--targets).
OS flags: when --skip-os=false, all --os-* options match yinstall db. Default --skip-os=true runs B-001 only.
Global -l/--list-steps prints the OS + OM (migrate/secondary) + standby step catalog.`,
	Example: `  # SE: single standby (OM and primary on same host; secondary yasom on target by default)
  yinstall standby --primary-ip 10.0.0.1 -t 10.0.0.2 --skip-os

  # Expand standby but skip secondary OM
  yinstall standby --primary-ip 10.10.10.172 -t 10.10.10.182 --skip-os --om-secondary=false

  # Only deploy secondary OM on existing cluster nodes (no expansion steps)
  yinstall standby --primary-ip 10.10.10.172 -t 10.10.10.182,10.10.10.183 --skip-os \
    -s O-011-O-012

  # Migrate primary OM (M1): --om + --om-new (omit --om-current)
  yinstall standby --om 10.10.10.172 -t 10.10.10.182 --skip-os \
    --om-new 10.10.10.173

  # Migrate primary OM (M1) with explicit --om-current
  yinstall standby --primary-ip 10.10.10.172 -t 10.10.10.182 --skip-os \
    --om-current 10.10.10.172 --om-new 10.10.10.173

  # Migrate primary OM to a new host (M2: host add then promote), then expand
  yinstall standby --om 10.10.10.172 -t 10.10.10.182 --skip-os \
    --om-new 10.10.10.190 --os-user-password '...'

  # CE: YAC primary -> 2-node YAC standby (default: no primary restart; set REPLICATION_ADDR in maintenance first)
  yinstall standby --primary-ip 10.10.10.172 -t 10.10.10.182,10.10.10.183 --yac \
    --yac-public-network 10.10.10.0/24 --yac-inter-cidr 10.10.234.0/24 \
    --yac-systemdg '/dev/yfs/sys1,/dev/yfs/sys2,/dev/yfs/sys3' \
    --yac-datadg '/dev/yfs/data1,/dev/yfs/data2' \
    --yac-vips '10.10.10.184/24,10.10.10.185/24' --db-admin-password '...' --skip-os

  # CE lab/maintenance only: allow yinstall to write SPFILE and restart primary cluster
  yinstall standby ... --yac --standby-restart-primary

  # CE: YAC primary -> single-node YAC standby
  yinstall standby --primary-ip 10.10.10.172 -t 10.10.10.182 --yac \
    --yac-inter-cidr 10.10.234.0/24 \
    --yac-systemdg '/dev/yfs/sys1,/dev/yfs/sys2,/dev/yfs/sys3' \
    --yac-datadg '/dev/yfs/data1,/dev/yfs/data2' \
    --yac-vips '10.10.10.184/24' --db-admin-password '...' --skip-os

  # After failover: primary moved, OM still on original host (OM auto from om_addr)
  yinstall standby --primary-ip 10.0.0.2 -t 10.0.0.3 --skip-os

  # Bootstrap from OM only; discover current primary via cluster status
  yinstall standby --om 10.0.0.1 -t 10.0.0.3 --skip-os`,
	RunE:         runStandby,
	SilenceUsage: true, // 报错时不显示帮助信息
}

func init() {
	// 主库 / OM 连接参数
	standbyCmd.Flags().StringVar(&primaryIP, "primary-ip", "", "Primary database IP (optional if -M/--om set; else auto-discover from cluster status)")
	// -M/--om 为根全局参数 (GlobalFlags.OmIP)；此处不再重复注册
	standbyCmd.Flags().StringVar(&omCurrent, "om-current", "", "Current primary OM IP (optional if -M/--om set; with --om-new migrates OM before expansion)")
	standbyCmd.Flags().StringVar(&omNew, "om-new", "", "Target primary OM IP; requires --om-current or --om; existing cluster node (M1) or new host (M2)")
	standbyCmd.Flags().BoolVar(&omDeploySecondary, "om-secondary", true, "Deploy secondary yasom after expansion (default: true; --om-secondary=false to skip)")
	standbyCmd.Flags().StringVar(&omDeploySecondaryScope, "om-secondary-scope", "targets", "OM secondary scope: targets (--targets only) or cluster (all non-primary hosts from yasom status)")

	standbyCmd.Flags().StringVar(&omNewSSHUser, "om-new-ssh-user", "", "SSH user for --om-new (default: global --ssh-user)")
	standbyCmd.Flags().StringVar(&omNewSSHPassword, "om-new-ssh-password", "", "SSH password for --om-new (default: global --ssh-password)")
	standbyCmd.Flags().StringVar(&omNewSSHKey, "om-new-ssh-key", "", "SSH key path for --om-new (default: global --ssh-key-path)")
	standbyCmd.Flags().StringVar(&primarySSHUser, "primary-ssh-user", "", "Primary SSH user (defaults to --ssh-user)")
	standbyCmd.Flags().StringVar(&primarySSHPassword, "primary-ssh-password", "", "Primary SSH password (defaults to --ssh-password)")
	standbyCmd.Flags().StringVar(&primarySSHKey, "primary-ssh-key", "", "Primary SSH key path (defaults to --ssh-key-path)")

	// 主库数据库用户和环境变量参数
	standbyCmd.Flags().StringVar(&primaryOSUser, "primary-os-user", "yashan", "Primary database user (default: yashan)")
	standbyCmd.Flags().StringVar(&primaryEnvFile, "primary-env-file", "", "Primary environment file path (default: auto-detect from .yasboot or .bashrc)")

	// 操作系统配置（与 db 共用 OS 变量；--skip-os=false 时 B-002–B-029 生效）
	standbyCmd.Flags().BoolVar(&skipOS, "skip-os", true, "Skip standby OS baseline on --targets (default: true; false runs full OS steps)")
	registerAllOSFlags(standbyCmd, registerOSFlagsConfig{forDB: true})
	registerYACModeFlag(standbyCmd)
	registerYACNetworkFlags(standbyCmd, true)

	// 数据库参数
	standbyCmd.Flags().StringVar(&standbyClusterName, "db-cluster-name", "yashandb", "Database cluster name (must match primary)")
	standbyCmd.Flags().IntVar(&dbMemoryPercent, "db-memory-percent", 50, "Planned DB memory percent (1-100) for OS shared memory when --skip-os=false")
	standbyCmd.Flags().StringVar(&standbyAdminPassword, "db-admin-password", "", "Database SYS admin password (required for CE group add; optional for SE)")
	standbyCmd.Flags().StringVar(&standbyInstallPath, "db-home-path", "", "Standby install path for yasboot (default: same as yinstall db: /data/<primary-os-user>/yasdb_home for port 1688, else yasdb_home_<port>)")
	standbyCmd.Flags().StringVar(&standbyDataPath, "db-data-path", "", "Standby data path for yasboot (default: same as yinstall db: /data/<primary-os-user>/yasdb_data or .../yasdb_data_<port>)")
	standbyCmd.Flags().StringVar(&standbyLogPath, "db-log-path", "", "Standby log path for yasboot (default: same as yinstall db: /data/<primary-os-user>/log or .../log_<port>)")
	standbyCmd.Flags().StringVar(&standbyStageDir, "db-stage-dir", "", "Stage directory on OM host (must exist; default /home/<user>/install for 1688, else install_<port>)")
	standbyCmd.Flags().StringVar(&standbyDepsPackage, "db-deps-package", "", "SSL deps package path (optional)")
	standbyCmd.Flags().IntVar(&standbyNodeCount, "standby-node-count", 0, "Standby node count for yasboot gen (default: len(--targets); single standby recommended)")

	// 扩容控制
	standbyCmd.Flags().BoolVar(&standbyCleanupOnFailure, "standby-cleanup-on-failure", false, "On expansion failure, auto-run safe cleanup (CE: group remove --clean; SE: node remove --clean). Also unlocked by global -F. Dangerous.")
	standbyCmd.Flags().BoolVar(&standbyRestartPrimary, "standby-restart-primary", false, "CE only: after writing REPLICATION_ADDR to SPFILE, also stop/start primary cluster (default: false — still writes SPFILE, but expansion stops until you restart in a maintenance window)")
	standbyCmd.Flags().IntVar(&standbySyncTimeoutSec, "standby-sync-timeout", 120, "Seconds to wait for standby role in cluster status after expansion (0=soft success if not yet visible)")

	standbyCmd.Flags().IntVar(&standbyBeginPort, "db-port", 1688, "Database begin port for yasboot expansion (default 1688; omit flag to use primary v$parameter.LISTEN_ADDR port)")
	standbyCmd.Flags().StringVar(&standbyYasbootGenExtraArgs, "yasboot-gen-extra-args", "", "Extra arguments appended to yasboot config node gen (SE) or config group gen (CE) (space-separated)")
}

// standbyHostExecutors 扩容时的 primary / OM 双执行器(可同机复用)。
type standbyHostExecutors struct {
	primaryIP       string
	omIP            string
	primary         ssh.Executor
	om              ssh.Executor
	omSameAsPrimary bool
}

func (t *standbyHostExecutors) close() {
	if t == nil {
		return
	}
	if t.primary != nil {
		_ = t.primary.Close()
	}
	if t.om != nil && !t.omSameAsPrimary {
		_ = t.om.Close()
	}
}

// runStandby 执行添加备库流程
func runStandby(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintStandbyStepCatalog(skipOS)
		return nil
	}

	// 全局 -M/--om → 运行时 omIP（迁主过程中可能被改写）
	if strings.TrimSpace(omIP) == "" {
		omIP = flags.OmIP
	}
	// 仅启用迁主（有 --om-new）时，用 --om 填补空的 --om-current；勿在日常扩备时写入 omCurrent
	if strings.TrimSpace(omNew) != "" && strings.TrimSpace(omCurrent) == "" {
		omCurrent = omsteps.ResolveOMMigrateCurrent("", omIP)
	}

	// 参数校验
	if err := validateStandbyParams(flags); err != nil {
		return err
	}

	omMigrate, err := omsteps.ValidateOMMigrateParams(omCurrent, omNew, omIP)
	if err != nil {
		return err
	}
	if omMigrate {
		// 迁移模式下入口 OM 固定为解析后的源 OM
		omCurrent = omsteps.ResolveOMMigrateCurrent(omCurrent, omIP)
		omIP = omCurrent
	}

	if err := validateMemoryPercent("--db-memory-percent", dbMemoryPercent); err != nil {
		return err
	}

	ResolveOSUserPassword(cmd, flags, osUser, &osUserPassword)

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

	// 入口机与全部 targets 均为 localhost 时，在建连前推导本地执行
	entryHost := strings.TrimSpace(primaryIP)
	if entryHost == "" {
		entryHost = strings.TrimSpace(omIP)
	}
	if isLocalHost(entryHost) {
		allLocal := true
		if p := strings.TrimSpace(primaryIP); p != "" && !isLocalHost(p) {
			allLocal = false
		}
		if o := strings.TrimSpace(omIP); o != "" && !isLocalHost(o) {
			allLocal = false
		}
		for _, t := range flags.Targets {
			if !isLocalHost(t) {
				allLocal = false
				break
			}
		}
		if allLocal {
			flags.Local = true
		}
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

	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting standby installation (RunID: %s)", rid)
	logger.Info("Standby targets: %v", flags.Targets)

	if skipOS {
		logger.Info("Standby OS baseline: SKIPPED")
	} else {
		logger.Info("Standby OS baseline: ENABLED")
	}

	if yacMode {
		logger.Info("YAC mode: ENABLED (CE standby group path when primary is CE; ycsrootagent autostart)")
		if err := standbysteps.RequireCEAdminPassword(standbyAdminPassword); err != nil {
			return err
		}
		if err := standbysteps.ValidateStandbyCEParams(yacInterCIDR, yacSystemDG, yacDataDG, yacVIPs, standbyNodeCount); err != nil {
			return err
		}
	} else {
		logger.Info("YAC mode: DISABLED (SE node expansion path)")
	}
	if omMigrate {
		logger.Info("OM migrate: ENABLED (%s -> %s)", strings.TrimSpace(omCurrent), strings.TrimSpace(omNew))
	} else {
		logger.Info("OM migrate: SKIPPED (set --om-new with --om-current or --om to enable)")
	}
	if omDeploySecondary {
		scope := strings.ToLower(strings.TrimSpace(omDeploySecondaryScope))
		if scope == "" {
			scope = "targets"
			omDeploySecondaryScope = scope
		}
		if scope != "targets" && scope != "cluster" {
			return fmt.Errorf("--om-secondary-scope must be targets or cluster")
		}
		logger.Info("OM secondary: ENABLED (scope=%s)", scope)
	} else {
		logger.Info("OM secondary: SKIPPED (--om-secondary=false)")
	}

	// 构建参数(拓扑解析后会回写 primary_ip / om_ip)
	params := buildStandbyParams(flags)

	topo, err := resolveStandbyHostExecutors(cmd, flags, logger, params)
	if err != nil {
		return err
	}
	defer topo.close()

	primaryIP = topo.primaryIP
	omIP = topo.omIP
	params["primary_ip"] = topo.primaryIP
	params["om_ip"] = topo.omIP

	// 本地模式下，除非用户显式指定，否则不注入默认的备库 os-user-password。
	if flags.Local && !cmd.Flags().Changed("os-user-password") {
		osUserPassword = ""
	}

	// 远程模式下 E-011 yasboot config node gen 需以产品用户 SSH 到备库 targets
	if !flags.Local && osUserPassword == "" && !flags.DryRun && !flags.Precheck {
		return fmt.Errorf("--os-user-password is required for yasboot config node gen (SSH password of product user on standby targets)")
	}

	logger.Info("Primary (read-write DB): %s", topo.primaryIP)
	logger.Info("OM (yasom/stage): %s", topo.omIP)
	if topo.omSameAsPrimary {
		logger.Info("OM and primary are the same host; reusing one SSH session")
	}

	if cn, ok := params["db_cluster_name"].(string); ok && cn != "" {
		logger.Info("Cluster name: %s", cn)
	} else {
		logger.Info("Cluster name: %s", standbyClusterName)
	}

	if err := tryFillBeginPortFromPrimary(cmd, topo.primary, logger, params, flags); err != nil {
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
			if step.ID == ossteps.FirstStepID() {
				allSteps = append(allSteps, step)
				break
			}
		}
	}

	// 添加备库扩容步骤
	standbySteps := standbysteps.GetAllSteps()
	allSteps = append(allSteps, standbySteps...)

	// OM 迁主步骤（仅当 --om-current/--om-new 成对启用）
	if omMigrate {
		allSteps = append(allSteps, omsteps.GetMigrateSteps()...)
	}
	// P2 部署备 OM（--om-secondary，默认启用）
	if omDeploySecondary {
		allSteps = append(allSteps, omsteps.GetDeploySecondarySteps()...)
	}

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

	// 阶段 1：Primary 连通性 + OM stage/status + Primary SQL 检查
	logger.Info("======== Phase 1: Primary/OM connectivity and status check ========")
	if err := checkPrimaryStatus(topo.primary, topo.om, logger, params, steps, flags); err != nil {
		return err
	}

	// 阶段 1.5：可选 OM 迁主（成对 --om-current/--om-new）
	if omMigrate {
		logger.Info("======== Phase 1.5: Migrate primary OM ========")
		if err := runOMMigrateSteps(topo, logger, params, flags, steps); err != nil {
			return err
		}
	}

	// 仅 OM 迁主/部署备 OM（无 E/B 扩备步）时：可选跑 Phase 4.5 后结束
	if !standbyHasExpansionSteps(steps) {
		if omDeploySecondary && standbyHasDeploySecondarySteps(steps) {
			logger.Info("======== Phase 2 (light): connect targets for OM secondary deploy ========")
			standbyHosts, pErr := prepareStandbyNodes(flags, logger, params, osStepsFiltered, steps)
			if pErr != nil {
				return pErr
			}
			defer closeStandbyExecutors(standbyHosts)
			logger.Info("======== Phase 4.5: Deploy secondary OM on targets ========")
			if err := runOMDeploySecondarySteps(topo, standbyHosts, logger, params, flags, steps); err != nil {
				return err
			}
		} else {
			logger.Info("No OS/standby expansion steps after filter; skipping Phase 2+")
		}
		logger.Info("Standby installation completed successfully")
		return nil
	}

	// 阶段 2：备库节点连通性检查和 OS 配置
	logger.Info("======== Phase 2: Standby nodes preparation ========")
	standbyHosts, err := prepareStandbyNodes(flags, logger, params, osStepsFiltered, steps)
	if err != nil {
		return err
	}
	defer closeStandbyExecutors(standbyHosts)

	// 跳过 E-002 时仍需解析 CE/SE，再做备侧网段/VIP 校验（写回同一 params）
	if resolved, _ := params["standby_ce_path_resolved"].(bool); !resolved {
		pathCtx := newStandbyStepContext(&runnerExecAdapter{e: topo.om}, logger, params, flags)
		if err := standbysteps.EnsureStandbyCEPath(pathCtx, ""); err != nil {
			return fmt.Errorf("resolve standby CE/SE path: %w", err)
		}
	}
	if useCE, _ := params["standby_ce_path"].(bool); useCE {
		if err := validateStandbyCEYACOnTargets(standbyHosts, params, logger); err != nil {
			return err
		}
	}

	// 阶段 3：检查归档路径和网络连通性(Primary)
	logger.Info("======== Phase 3: Archive destination check and network connectivity ========")
	if err := checkArchiveDestination(topo.primary, logger, params, steps, flags); err != nil {
		return err
	}
	if err := checkNetworkConnectivity(topo.primary, standbyHosts, logger, params, steps, flags); err != nil {
		return err
	}

	// 阶段 4：OM 上清理残留并扩容; Primary 上做同步检查(E-014)
	logger.Info("======== Phase 4: Expansion on OM + sync check on primary ========")
	if err := checkAndCleanupExistingNodes(topo.om, logger, params, steps, flags); err != nil {
		return err
	}
	if err := executeExpansionSteps(topo.om, topo.primary, logger, params, flags, steps, standbyStepsFiltered); err != nil {
		handleStandbyExpansionFailure(topo.om, logger, params, flags, err)
		return err
	}

	// 阶段 4.5：可选在扩备目标上部署备 OM
	if omDeploySecondary && standbyHasDeploySecondarySteps(steps) {
		logger.Info("======== Phase 4.5: Deploy secondary OM on targets ========")
		if err := runOMDeploySecondarySteps(topo, standbyHosts, logger, params, flags, steps); err != nil {
			return err
		}
	}

	// 阶段 5：备库后续配置（环境变量、自启动）
	logger.Info("======== Phase 5: Standby post-configuration ========")
	if err := configureStandbyPostSteps(standbyHosts, logger, params, flags, steps); err != nil {
		return err
	}

	// 阶段 6：显示集群状态(OM 侧 yasboot)
	logger.Info("======== Phase 6: Show cluster status ========")
	if err := showClusterStatus(topo.om, logger, params, steps, flags); err != nil {
		return err
	}

	// 阶段 7：可选清理（E-018，在 OM）
	if err := runStandbyOptionalCleanup(topo.om, logger, params, steps, flags); err != nil {
		return err
	}

	logger.Info("Standby installation completed successfully")
	return nil
}

// validateStandbyParams 校验必填参数
func validateStandbyParams(flags GlobalFlags) error {
	if strings.TrimSpace(primaryIP) == "" && strings.TrimSpace(omIP) == "" {
		return fmt.Errorf("at least one of --primary-ip or -M/--om is required")
	}

	if len(flags.Targets) == 0 {
		return fmt.Errorf("--targets is required (standby node IP addresses)")
	}

	if err := validatePort("--db-port", standbyBeginPort); err != nil {
		return err
	}

	return nil
}

// resolveStandbyHostExecutors 解析 primary/OM IP 并建立 SSH(OM 用全局凭证, primary 可用 primary-ssh-*)。
func resolveStandbyHostExecutors(_ *cobra.Command, flags GlobalFlags, logger *logging.Logger, params map[string]interface{}) (*standbyHostExecutors, error) {
	userPrimary := strings.TrimSpace(primaryIP)
	userOM := strings.TrimSpace(omIP)
	topo := &standbyHostExecutors{}

	connectPrimary := func(host string) (ssh.Executor, error) {
		primaryIP = host
		ex, err := createStandbyPrimaryExecutor(flags, logger, "")
		if err != nil {
			return nil, fmt.Errorf("failed to connect to primary %s: %w", host, err)
		}
		return ex, nil
	}
	connectOM := func(host string) (ssh.Executor, error) {
		ex, err := createStandbyOmExecutor(host, flags, logger, "")
		if err != nil {
			return nil, fmt.Errorf("failed to connect to OM %s: %w", host, err)
		}
		return ex, nil
	}

	// 入口机: 优先 --primary-ip, 否则 -M/--om
	var bootstrap ssh.Executor
	var bootstrapHost string
	var err error
	if userPrimary != "" {
		bootstrapHost = userPrimary
		bootstrap, err = connectPrimary(userPrimary)
		if err != nil {
			return nil, err
		}
		topo.primary = bootstrap
		topo.primaryIP = userPrimary
	} else {
		bootstrapHost = userOM
		bootstrap, err = connectOM(userOM)
		if err != nil {
			return nil, err
		}
		topo.om = bootstrap
		topo.omIP = userOM
	}

	trySyncClusterNameFromPrimaryEnv(bootstrap, logger, params, flags)

	clusterName, _ := params["db_cluster_name"].(string)
	if strings.TrimSpace(clusterName) == "" {
		clusterName = standbyClusterName
		params["db_cluster_name"] = clusterName
	}
	osUser := primaryOSUser
	if strings.TrimSpace(osUser) == "" {
		osUser = "yashan"
	}
	bootCtx := newStandbyStepContext(&runnerExecAdapter{e: bootstrap}, logger, params, flags)

	resolvedOM := userOM
	if resolvedOM == "" {
		h, omErr := standbysteps.ResolveOmHostFromRemoteEnv(bootCtx, osUser, clusterName)
		if omErr != nil {
			topo.close()
			return nil, fmt.Errorf("auto-discover OM from om_addr failed on %s: %w (set -M/--om)", bootstrapHost, omErr)
		}
		resolvedOM = h
		logger.Info("OM IP auto-discovered from om_addr: %s", resolvedOM)
	} else {
		logger.Info("OM IP from -M/--om: %s", resolvedOM)
	}

	resolvedPrimary := userPrimary
	if resolvedPrimary == "" {
		if flags.DryRun {
			topo.close()
			return nil, fmt.Errorf("--primary-ip is required in dry-run when auto-discover is needed")
		}
		ip, dErr := standbysteps.DiscoverPrimaryIPOnRemote(bootCtx, osUser, "", clusterName)
		if dErr != nil {
			topo.close()
			return nil, fmt.Errorf("auto-discover primary from cluster status failed on %s: %w (set --primary-ip)", bootstrapHost, dErr)
		}
		resolvedPrimary = ip
		logger.Info("Primary IP auto-discovered from cluster status: %s", resolvedPrimary)
	} else {
		logger.Info("Primary IP from --primary-ip: %s", resolvedPrimary)
		if !flags.DryRun {
			if disc, dErr := standbysteps.DiscoverPrimaryIPOnRemote(bootCtx, osUser, "", clusterName); dErr == nil && disc != "" && !standbysteps.SameHostIP(disc, resolvedPrimary) {
				logger.Warn("cluster status primary=%s differs from --primary-ip=%s; archive SQL (E-014) will use --primary-ip — ensure it is read-write or switchover first", disc, resolvedPrimary)
			}
		}
	}

	topo.primaryIP = resolvedPrimary
	topo.omIP = resolvedOM
	same := standbysteps.SameHostIP(resolvedOM, resolvedPrimary)

	// 确保 primary Executor
	if topo.primary == nil {
		if same && topo.om != nil {
			topo.primary = topo.om
			topo.omSameAsPrimary = true
		} else {
			ex, cErr := connectPrimary(resolvedPrimary)
			if cErr != nil {
				topo.close()
				return nil, cErr
			}
			topo.primary = ex
			trySyncClusterNameFromPrimaryEnv(topo.primary, logger, params, flags)
		}
	}

	// 确保 OM Executor
	if same {
		if topo.om != nil && topo.om != topo.primary {
			_ = topo.om.Close()
		}
		topo.om = topo.primary
		topo.omSameAsPrimary = true
	} else if topo.om == nil {
		ex, cErr := connectOM(resolvedOM)
		if cErr != nil {
			topo.close()
			return nil, cErr
		}
		topo.om = ex
	} else if !standbysteps.SameHostIP(topo.om.Host(), resolvedOM) {
		_ = topo.om.Close()
		ex, cErr := connectOM(resolvedOM)
		if cErr != nil {
			topo.close()
			return nil, cErr
		}
		topo.om = ex
	}

	return topo, nil
}

// buildStandbyParams 构建备库参数
func buildStandbyParams(flags GlobalFlags) map[string]interface{} {
	params := buildOSParams(yacMode, len(flags.Targets))
	// 与 db/os 一致: 非 root SSH 时切产品用户 / 特权命令依赖 params["sudo"]
	params["sudo"] = flags.UseSudo
	params["db_memory_percent"] = dbMemoryPercent
	params["os_sysctl_shm_use_max_ram_only"] = false
	params["with_os"] = !skipOS

	params["ssh_port"] = flags.SSHPort
	params["yasboot_ssh_port"] = flags.YasbootSSHPort

	// 主库 / OM 参数(拓扑解析后可能回写 primary_ip / om_ip)
	params["primary_ip"] = primaryIP
	params["om_ip"] = omIP
	params["om_current"] = strings.TrimSpace(omCurrent)
	params["om_new"] = strings.TrimSpace(omNew)
	if mig, _ := omsteps.ValidateOMMigrateParams(omCurrent, omNew, omIP); mig {
		params["om_migrate"] = true
		// 保证 params 中源 OM 已解析
		cur := omsteps.ResolveOMMigrateCurrent(omCurrent, omIP)
		params["om_current"] = cur
		params["om_ip"] = cur
	} else {
		params["om_migrate"] = false
	}
	params["om_deploy_secondary"] = omDeploySecondary
	params["om_deploy_secondary_scope"] = strings.TrimSpace(omDeploySecondaryScope)
	if params["om_deploy_secondary_scope"] == "" {
		params["om_deploy_secondary_scope"] = "targets"
	}
	params["om_new_ssh_user"] = strings.TrimSpace(omNewSSHUser)
	params["om_new_ssh_password"] = omNewSSHPassword
	params["om_new_ssh_key"] = strings.TrimSpace(omNewSSHKey)
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

	params["yac_mode"] = yacMode
	params["db_begin_port"] = standbyBeginPort
	params["yac_inter_cidr"] = yacInterCIDR
	params["yac_public_network"] = yacPublicNetwork
	params["yac_vips"] = yacVIPs
	diskFound := strings.TrimSpace(yacDiskFoundPath)
	if diskFound == "" {
		diskFound = dbsteps.DefaultYACDiskFoundPath
	}
	params["yac_disk_found_path"] = diskFound

	// 备库特定参数
	params["standby_node_count"] = standbyNodeCount
	params["standby_targets"] = flags.Targets
	params["standby_targets_str"] = strings.Join(flags.Targets, ",")
	params["standby_cleanup_on_failure"] = standbyCleanupOnFailure
	params["standby_restart_primary"] = standbyRestartPrimary
	params["standby_sync_timeout_sec"] = standbySyncTimeoutSec
	params["skip_os"] = skipOS
	params[dbsteps.ParamYasbootGenExtraArgs] = standbyYasbootGenExtraArgs

	return params
}

// createStandbyPrimaryExecutor 创建主库执行器(可用 primary-ssh-* 覆盖全局凭证)
func createStandbyPrimaryExecutor(flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	user := primarySSHUser
	if user == "" {
		user = flags.SSHUser
	}
	pass := primarySSHPassword
	if pass == "" {
		pass = flags.SSHPassword
	}
	key := primarySSHKey
	if key == "" {
		key = flags.SSHKeyPath
	}
	return createPrimaryExecutor(PrimarySSHConfig{
		Host:     primaryIP,
		Port:     flags.SSHPort,
		User:     user,
		Password: pass,
		KeyPath:  key,
		Auth:     flags.SSHAuth,
		Local:    flags.Local || isLocalHost(primaryIP),
	}, logger, stepID)
}

// createStandbyOmExecutor 创建 OM 执行器(默认全局 SSH 凭证)。
func createStandbyOmExecutor(host string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	return createStandbyOmExecutorWithCreds(host, flags, logger, stepID, "", "", "")
}

// createStandbyOmExecutorWithCreds 可用覆盖凭证连接 OM 主机（用于 --om-new-ssh-*）。
func createStandbyOmExecutorWithCreds(host string, flags GlobalFlags, logger *logging.Logger, stepID, user, pass, key string) (ssh.Executor, error) {
	if strings.TrimSpace(user) == "" {
		user = flags.SSHUser
	}
	if pass == "" {
		pass = flags.SSHPassword
	}
	if strings.TrimSpace(key) == "" {
		key = flags.SSHKeyPath
	}
	return createPrimaryExecutor(PrimarySSHConfig{
		Host:     host,
		Port:     flags.SSHPort,
		User:     user,
		Password: pass,
		KeyPath:  key,
		Auth:     flags.SSHAuth,
		Local:    flags.Local || isLocalHost(host),
	}, logger, stepID)
}

// pickOMStageExecutor 在 CUR/NEW 中选择存在 hosts.toml 的主机（stage 常不随主 OM 迁移）。
func pickOMStageExecutor(cur, nw ssh.Executor, stageDir string, logger *logging.Logger) ssh.Executor {
	if strings.TrimSpace(stageDir) == "" {
		stageDir = "/home/yashan/install"
	}
	check := fmt.Sprintf("test -f %s/hosts.toml", stageDir)
	try := func(ex ssh.Executor, label string) bool {
		if ex == nil {
			return false
		}
		res, err := ex.Execute(check, true) // 非 root SSH 需 sudo 才能探测 /home/<product>/install
		ok := err == nil && res != nil && res.GetExitCode() == 0
		if ok {
			logger.Info("OM stage hosts.toml found on %s (%s)", label, stageDir)
		}
		return ok
	}
	// 优先 NEW（回迁时 stage 常在目标机），再 CUR
	if try(nw, "new OM") {
		return nw
	}
	if try(cur, "current OM") {
		return cur
	}
	logger.Warn("OM stage hosts.toml not found on CUR/NEW; default to current OM for O-008")
	return cur
}

// omStageListing 在远端列出 stage 目录文件名。
func omStageListing(ex ssh.Executor, stageDir string) ([]string, error) {
	if ex == nil {
		return nil, fmt.Errorf("executor is nil")
	}
	res, err := ex.Execute(fmt.Sprintf("ls -1 %s 2>/dev/null", stageDir), true)
	if err != nil {
		return nil, err
	}
	if res == nil || res.GetExitCode() != 0 {
		return nil, fmt.Errorf("cannot list stage dir %s on %s", stageDir, ex.Host())
	}
	return omsteps.ParseLSNames(res.GetStdout()), nil
}

// syncOMStageToHost 将 stage 从 src 同步到 dst。
// full: 备 OM 部署时用 (包+toml)；toml: 迁主后只用 (刷新 hosts.toml 等)。
func syncOMStageToHost(src, dst ssh.Executor, stageDir, productUser, srcRootPassword string, mode omsteps.OMStageSyncMode, logger *logging.Logger) error {
	if src == nil || dst == nil {
		return fmt.Errorf("sync OM stage: src/dst executor required")
	}
	stageDir = strings.TrimSpace(stageDir)
	if stageDir == "" {
		stageDir = "/home/yashan/install"
	}
	productUser = strings.TrimSpace(productUser)
	if productUser == "" {
		productUser = "yashan"
	}
	if mode == "" {
		mode = omsteps.OMStageSyncFull
	}
	if standbysteps.SameHostIP(src.Host(), dst.Host()) {
		logger.Info("OM stage sync skipped: source and dest are the same host (%s)", dst.Host())
		return nil
	}

	dstNames, _ := omStageListing(dst, stageDir)
	if mode == omsteps.OMStageSyncFull && omsteps.IsOMStageListingReady(dstNames) {
		logger.Info("OM stage already complete on %s (%s); skip full sync", dst.Host(), stageDir)
		return nil
	}

	srcNames, err := omStageListing(src, stageDir)
	if err != nil {
		return fmt.Errorf("OM stage missing on source %s (%s): %w", src.Host(), stageDir, err)
	}
	if mode == omsteps.OMStageSyncFull && !omsteps.IsOMStageListingReady(srcNames) {
		return fmt.Errorf("OM stage on source %s incomplete (need hosts.toml + package under %s); listing=%v",
			src.Host(), stageDir, srcNames)
	}
	if mode == omsteps.OMStageSyncTOML && !omsteps.IsOMStageTomlReady(srcNames) {
		return fmt.Errorf("OM stage on source %s missing hosts.toml under %s", src.Host(), stageDir)
	}

	toCopy := omsteps.FilterOMStageNames(srcNames, mode)
	if len(toCopy) == 0 {
		return fmt.Errorf("OM stage sync mode=%s: no files to copy from %s", mode, src.Host())
	}

	logger.Info("OM stage sync mode=%s: %s -> %s (%d files)", mode, src.Host(), dst.Host(), len(toCopy))
	prep := fmt.Sprintf("mkdir -p %s && chown -R %s:%s $(dirname %s) %s 2>/dev/null; true",
		stageDir, productUser, productUser, stageDir, stageDir)
	if _, err := dst.Execute(prep, true); err != nil {
		return fmt.Errorf("prepare stage dir on %s: %w", dst.Host(), err)
	}

	// full 模式优先 sshpass 整目录拉取
	if mode == omsteps.OMStageSyncFull && srcRootPassword != "" {
		hasPass, _ := dst.Execute("command -v sshpass >/dev/null 2>&1", true)
		if hasPass != nil && hasPass.GetExitCode() == 0 {
			pull := fmt.Sprintf(
				`export SSHPASS=%s; sshpass -e scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r root@%s:%s/. %s/ && chown -R %s:%s %s`,
				shellSingleQuote(srcRootPassword), src.Host(), stageDir, stageDir, productUser, productUser, stageDir)
			logger.Info("OM stage full sync via sshpass pull on %s", dst.Host())
			res, err := dst.Execute(pull, true)
			if err == nil && res != nil && res.GetExitCode() == 0 {
				names, lerr := omStageListing(dst, stageDir)
				if lerr == nil && omsteps.IsOMStageListingReady(names) {
					logger.Info("OM stage full sync done (sshpass) on %s", dst.Host())
					return nil
				}
			}
			logger.Warn("OM stage sshpass pull failed; falling back to control-host copy")
		}
	}

	if err := copyOMStageViaControl(src, dst, stageDir, toCopy, productUser, mode, logger); err != nil {
		return err
	}
	after, err := omStageListing(dst, stageDir)
	if err != nil {
		return err
	}
	if mode == omsteps.OMStageSyncTOML {
		if !omsteps.IsOMStageTomlReady(after) {
			return fmt.Errorf("OM stage toml sync incomplete on %s; listing=%v", dst.Host(), after)
		}
		if !omsteps.IsOMStagePackageReady(after) {
			logger.Warn("OM stage on %s has toml but no install package; deploy OM secondary (full stage sync) before expansion if needed", dst.Host())
		}
		return nil
	}
	if !omsteps.IsOMStageListingReady(after) {
		return fmt.Errorf("OM stage full sync incomplete on %s; listing=%v", dst.Host(), after)
	}
	return nil
}

func shellSingleQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}

// copyOMStageViaControl 经控制机逐文件拷贝; names 已是 basename 列表。
func copyOMStageViaControl(src, dst ssh.Executor, stageDir string, names []string, productUser string, mode omsteps.OMStageSyncMode, logger *logging.Logger) error {
	tmpDir, err := os.MkdirTemp("", "yinstall-om-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	uctx := &ssh.UploadContext{Logger: logger, StepID: "OM-STAGE", Host: dst.Host()}
	copied := 0
	for _, base := range names {
		base = filepath.Base(strings.TrimSpace(base))
		if base == "" {
			continue
		}
		remote := path.Join(stageDir, base)
		local := filepath.Join(tmpDir, base)
		logger.Info("OM stage copy (%s): %s:%s -> control -> %s:%s", mode, src.Host(), remote, dst.Host(), remote)
		if err := src.Download(remote, local); err != nil {
			return fmt.Errorf("download %s from %s: %w", remote, src.Host(), err)
		}
		if err := dst.Upload(local, remote, uctx); err != nil {
			return fmt.Errorf("upload %s to %s: %w", remote, dst.Host(), err)
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("no stage files copied from %s (mode=%s)", src.Host(), mode)
	}
	chown := fmt.Sprintf("chown -R %s:%s %s", productUser, productUser, stageDir)
	if _, err := dst.Execute(chown, true); err != nil {
		logger.Warn("chown stage on %s: %v", dst.Host(), err)
	}
	logger.Info("OM stage sync done (control copy, mode=%s, %d files) on %s", mode, copied, dst.Host())
	return nil
}

// resolveOMStageSyncParams 从 standby/om params 取 stage 路径与产品用户。
func resolveOMStageSyncParams(params map[string]interface{}) (stageDir, user string) {
	stageDir = strings.TrimSpace(fmt.Sprint(params["db_stage_dir"]))
	if stageDir == "" || stageDir == "<nil>" {
		stageDir = "/home/yashan/install"
	}
	user = strings.TrimSpace(fmt.Sprint(params["primary_os_user"]))
	if user == "" || user == "<nil>" {
		user = strings.TrimSpace(fmt.Sprint(params["os_user"]))
	}
	if user == "" || user == "<nil>" {
		user = "yashan"
	}
	return stageDir, user
}

// standbyHasExpansionSteps 判断过滤后是否仍有 OS(B-*) 或扩备(E-*) 步骤。
func standbyHasExpansionSteps(steps []*runner.Step) bool {
	for _, s := range steps {
		if s == nil {
			continue
		}
		if strings.HasPrefix(s.ID, "B-") || strings.HasPrefix(s.ID, "E-") {
			return true
		}
	}
	return false
}

// standbyHasDeploySecondarySteps 判断过滤列表是否含 P2 部署备 OM 步骤。
func standbyHasDeploySecondarySteps(steps []*runner.Step) bool {
	for _, s := range steps {
		if s == nil {
			continue
		}
		if s.Name == "OM Deploy Secondary Gate" || s.Name == "OM Deploy Secondary Host" {
			return true
		}
		for _, t := range s.Tags {
			if t == "deploy-secondary" {
				return true
			}
		}
	}
	return false
}

// runOMDeploySecondarySteps Phase 4.5: Gate 在主 OM 执行，Host 步对每个 -t 循环 RunStep。
func runOMDeploySecondarySteps(topo *standbyHostExecutors, hosts []*HostInfo, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, filtered []*runner.Step) error {
	var gate, hostStep *runner.Step
	for _, s := range filtered {
		if s == nil {
			continue
		}
		switch s.Name {
		case "OM Deploy Secondary Gate":
			gate = s
		case "OM Deploy Secondary Host":
			hostStep = s
		}
	}
	if gate == nil && hostStep == nil {
		logger.Warn("OM deploy secondary requested but no O deploy steps after filter; skipping Phase 4.5")
		return nil
	}

	results := make(map[string]interface{})
	omCtx := newStandbyStepContext(&runnerExecAdapter{e: topo.om}, logger, params, flags)
	omCtx.Results = results

	if gate != nil {
		omCtx.CurrentStepID = gate.ID
		setStandbyStepProgress(omCtx, filtered, gate)
		result := runner.RunStep(gate, omCtx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				// continue hosts in precheck mode
			} else {
				return fmt.Errorf("OM deploy secondary gate failed: %w", result.Error)
			}
		}
	}

	if hostStep == nil {
		return nil
	}

	priIP, _ := results["om_primary_ip"].(string)
	if priIP == "" {
		priIP = strings.TrimSpace(fmt.Sprint(params["om_ip"]))
	}

	scope := strings.ToLower(strings.TrimSpace(fmt.Sprint(params["om_deploy_secondary_scope"])))
	if scope == "" {
		scope = "targets"
	}

	type hostTarget struct {
		Host string
		Exec ssh.Executor
		OS   *runner.OSInfo
		own  bool
	}
	var targets []hostTarget

	if scope == "cluster" {
		rows, _, err := omsteps.YasomStatus(omCtx)
		if err != nil {
			return fmt.Errorf("OM deploy secondary cluster scope: status failed: %w", err)
		}
		for _, ip := range omsteps.ListSecondaryCandidateIPs(rows) {
			if priIP != "" && standbysteps.SameHostIP(ip, priIP) {
				continue
			}
			// 复用已连接的 standbyHosts
			var found *HostInfo
			for _, h := range hosts {
				if h != nil && standbysteps.SameHostIP(h.Host, ip) {
					found = h
					break
				}
			}
			if found != nil {
				targets = append(targets, hostTarget{Host: found.Host, Exec: found.Executor, OS: found.OSInfo})
				continue
			}
			ex, cErr := createStandbyOmExecutor(ip, flags, logger, hostStep.ID)
			if cErr != nil {
				return fmt.Errorf("connect %s for OM secondary deploy: %w", ip, cErr)
			}
			targets = append(targets, hostTarget{Host: ip, Exec: ex, own: true})
		}
	} else {
		for _, h := range hosts {
			if h == nil || h.Executor == nil {
				continue
			}
			targets = append(targets, hostTarget{Host: h.Host, Exec: h.Executor, OS: h.OSInfo})
		}
	}
	defer func() {
		for _, t := range targets {
			if t.own && t.Exec != nil {
				_ = t.Exec.Close()
			}
		}
	}()

	if len(targets) == 0 {
		return fmt.Errorf("OM deploy secondary: no target hosts (scope=%s)", scope)
	}

	for _, h := range targets {
		if priIP != "" && standbysteps.SameHostIP(h.Host, priIP) {
			logger.Info("Skip OM secondary deploy on primary OM host %s", h.Host)
			continue
		}
		params["om_secondary_host"] = h.Host
		ctx := newStandbyStepContext(&runnerExecAdapter{e: h.Exec}, logger, params, flags)
		ctx.Results = results
		ctx.OSInfo = h.OS
		ctx.CurrentStepID = hostStep.ID
		setStandbyStepProgress(ctx, filtered, hostStep)
		logger.Info("-------- OM secondary deploy: %s --------", h.Host)
		result := runner.RunStep(hostStep, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				continue
			}
			return fmt.Errorf("OM deploy secondary on %s failed: %w", h.Host, result.Error)
		}
		// 备 OM 部署后全量同步 stage (包+toml)；已有 secondary 被 skip 时仍同步 (补齐缺包机)
		if !flags.Precheck && !flags.DryRun && result.Success && topo.om != nil {
			stageDir, user := resolveOMStageSyncParams(params)
			srcPass := strings.TrimSpace(flags.SSHPassword)
			if err := syncOMStageToHost(topo.om, h.Exec, stageDir, user, srcPass, omsteps.OMStageSyncFull, logger); err != nil {
				return fmt.Errorf("OM stage full sync to secondary %s failed: %w", h.Host, err)
			}
		}
	}
	delete(params, "om_secondary_host")
	logger.Info("OM deploy secondary on targets finished (scope=%s)", scope)
	return nil
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

// runOMMigrateSteps Phase 1.5: 先在 --om-new 上跑 OS(B-*, 复用 ossteps, 对齐 db), 再按 O-* 迁主；成功后切换 topo.om。
func runOMMigrateSteps(topo *standbyHostExecutors, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, filtered []*runner.Step) error {
	var osSteps, omSteps []*runner.Step
	for _, s := range filtered {
		if s == nil {
			continue
		}
		if strings.HasPrefix(s.ID, "B-") {
			osSteps = append(osSteps, s)
			continue
		}
		if !strings.HasPrefix(s.ID, "O-") {
			continue
		}
		// 仅迁主标签；勿把 deploy-secondary / ipchange 塞进 Phase 1.5
		for _, t := range s.Tags {
			if t == "migrate" {
				omSteps = append(omSteps, s)
				break
			}
		}
	}
	if len(omSteps) == 0 && len(osSteps) == 0 {
		logger.Warn("OM migrate requested but no B-*/O-* steps after -s/-e filter; skipping Phase 1.5")
		return nil
	}
	if len(omSteps) == 0 {
		logger.Warn("OM migrate: no O-* steps after filter; running OS-only on --om-new if present")
	}

	curIP := strings.TrimSpace(fmt.Sprint(params["om_current"]))
	newIP := strings.TrimSpace(fmt.Sprint(params["om_new"]))
	if curIP == "" {
		curIP = strings.TrimSpace(omCurrent)
	}
	if newIP == "" {
		newIP = strings.TrimSpace(omNew)
	}
	if curIP == "" || newIP == "" {
		return fmt.Errorf("om migrate: om_current/om_new missing in params")
	}

	// CUR 使用已解析的 OM 执行器；若 topo.om 不是 CUR，重建
	curExec := topo.om
	if curExec == nil || !standbysteps.SameHostIP(curExec.Host(), curIP) {
		ex, err := createStandbyOmExecutor(curIP, flags, logger, omsteps.FirstStepID())
		if err != nil {
			return fmt.Errorf("connect current OM %s: %w", curIP, err)
		}
		if topo.om != nil && !topo.omSameAsPrimary {
			_ = topo.om.Close()
		}
		topo.om = ex
		topo.omIP = curIP
		topo.omSameAsPrimary = standbysteps.SameHostIP(topo.primaryIP, curIP)
		if topo.omSameAsPrimary {
			_ = ex.Close()
			topo.om = topo.primary
			curExec = topo.primary
		} else {
			curExec = ex
		}
	}

	newExec, err := createStandbyOmExecutorWithCreds(newIP, flags, logger, omsteps.FirstStepID(),
		omNewSSHUser, omNewSSHPassword, omNewSSHKey)
	if err != nil {
		return fmt.Errorf("connect new OM %s: %w", newIP, err)
	}
	newOwned := true
	defer func() {
		if newOwned && newExec != nil {
			_ = newExec.Close()
		}
	}()

	results := make(map[string]interface{})
	curCtx := newStandbyStepContext(&runnerExecAdapter{e: curExec}, logger, params, flags)
	curCtx.Results = results
	newCtx := newStandbyStepContext(&runnerExecAdapter{e: newExec}, logger, params, flags)
	newCtx.Results = results

	// OS 基线在 --om-new 上执行 (复用 ossteps / RunPerHostStepsEx, 与 db 相同)
	// 若 om-new 同时也是 standby --targets, 留给 Phase 2 做全量 OS, 此处只跑连通性避免重复
	if len(osSteps) > 0 {
		for _, t := range flags.Targets {
			if standbysteps.SameHostIP(t, newIP) {
				onlyConn := len(osSteps) == 1 && osSteps[0].ID == ossteps.FirstStepID()
				if !onlyConn {
					logger.Info("OM migrate: --om-new %s is also a standby target; defer full OS to Phase 2 (run B-001 only here)", newIP)
					var connOnly []*runner.Step
					for _, s := range osSteps {
						if s.ID == ossteps.FirstStepID() {
							connOnly = append(connOnly, s)
							break
						}
					}
					osSteps = connOnly
				}
				break
			}
		}
		if len(osSteps) > 0 {
			logger.Info("======== OM migrate OS baseline on %s (%d steps) ========", newIP, len(osSteps))
			hostInfos := []*HostInfo{{Host: newIP, Executor: newExec}}
			total := len(filtered)
			osResult := RunPerHostStepsEx(osSteps, hostInfos, params, flags, logger, 0, total, results, nil, nil)
			if osResult != nil && osResult.LastError != nil && !flags.Precheck {
				return fmt.Errorf("OM migrate OS baseline on %s failed: %w", newIP, osResult.LastError)
			}
			if len(hostInfos) > 0 && hostInfos[0].OSInfo != nil {
				newCtx.OSInfo = hostInfos[0].OSInfo
			}
		}
	}

	if len(omSteps) == 0 {
		return nil
	}

	// hosts.toml 常留在首次安装的 stage 机（不一定是当前主 OM）
	stageDir := ""
	if v, ok := params["db_stage_dir"].(string); ok {
		stageDir = strings.TrimSpace(v)
	}
	stageExec := pickOMStageExecutor(curExec, newExec, stageDir, logger)
	stageCtx := curCtx
	if stageExec == newExec {
		stageCtx = newCtx
	} else if stageExec != curExec {
		stageCtx = newStandbyStepContext(&runnerExecAdapter{e: stageExec}, logger, params, flags)
		stageCtx.Results = results
	}

	// 按 Name 决定执行主机（与设计表一致）
	onNew := map[string]bool{
		"OM Host Prepare":      true,
		"OM Recover Secondary": true,
		"OM Recover Primary":   true,
		"OM Sync":              true,
	}

	logger.Info("OM migrate steps: %d (current=%s new=%s)", len(omSteps), curIP, newIP)
	lastOK := ""
	for _, step := range omSteps {
		ctx := curCtx
		switch {
		case step.Name == "OM Update Hosts TOML":
			ctx = stageCtx
		case onNew[step.Name]:
			ctx = newCtx
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			// --precheck 也必须非 0 退出，避免步骤 PreCheck 已失败却假绿
			if flags.Precheck {
				return fmt.Errorf("precheck failed")
			}
			logger.Error("OM migrate failed at %s; attempting best-effort rollback (lastOK=%s)", step.Name, lastOK)
			if rbErr := omsteps.AttemptMigrateRollback(curCtx, newCtx, curIP, newIP, lastOK); rbErr != nil {
				logger.Warn("OM migrate rollback: %v", rbErr)
			}
			return fmt.Errorf("OM migrate step %s failed: %w", step.ID, result.Error)
		}
		if result.Success {
			lastOK = step.Name
		}
	}

	// 迁主后仅同步 toml (安装包应在部署备 OM 时已全量同步)
	if !flags.Precheck && !flags.DryRun {
		stageDir, user := resolveOMStageSyncParams(params)
		srcPass := strings.TrimSpace(flags.SSHPassword)
		if err := syncOMStageToHost(stageExec, newExec, stageDir, user, srcPass, omsteps.OMStageSyncTOML, logger); err != nil {
			return fmt.Errorf("OM stage toml sync to %s failed: %w", newIP, err)
		}
	}

	// 切到新主 OM：复用 newExec（或与 primary 同机）
	params["om_ip"] = newIP
	omIP = newIP
	oldOm := topo.om
	if standbysteps.SameHostIP(topo.primaryIP, newIP) {
		if newOwned && newExec != nil {
			_ = newExec.Close()
			newOwned = false
		}
		topo.om = topo.primary
		topo.omSameAsPrimary = true
	} else {
		topo.om = newExec
		topo.omSameAsPrimary = false
		newOwned = false
	}
	topo.omIP = newIP
	if oldOm != nil && oldOm != topo.primary && oldOm != topo.om {
		_ = oldOm.Close()
	}
	logger.Info("OM context switched to %s for subsequent phases", newIP)
	return nil
}

// checkPrimaryStatus 运行 E-001～E-004: E-002(stage/yasboot) 在 OM, 其余在 Primary
func checkPrimaryStatus(primaryExec, omExec ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Checking primary on %s and OM/stage on %s", primaryExec.Host(), omExec.Host())

	primaryCtx := newStandbyStepContext(&runnerExecAdapter{e: primaryExec}, logger, params, flags)
	omCtx := newStandbyStepContext(&runnerExecAdapter{e: omExec}, logger, params, flags)

	omStepID := standbyStepID("Check Primary Status")
	primaryPhase := map[string]bool{
		standbyStepID("Check Primary Connectivity"): true,
		omStepID:                            true,
		standbyStepID("Check Archive Mode"): true,
		standbyStepID("Check Replication Address"): true,
	}
	for _, step := range filtered {
		if !primaryPhase[step.ID] {
			continue
		}
		ctx := primaryCtx
		if step.ID == omStepID {
			ctx = omCtx
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				continue
			}
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
	}

	return nil
}

// validateStandbyCEYACOnTargets 在备库节点上校验 YAC 网段与 VIP（复用 db.ValidateYACNetworksOnHosts / ValidateYACVIPsConfigured）。
func validateStandbyCEYACOnTargets(standbyHosts []*HostInfo, params map[string]interface{}, logger *logging.Logger) error {
	if len(standbyHosts) == 0 {
		return fmt.Errorf("CE standby path: no standby hosts connected for YAC network/VIP validation")
	}
	hostExecs := make([]dbsteps.HostExec, 0, len(standbyHosts))
	for _, info := range standbyHosts {
		if info == nil || info.Executor == nil {
			continue
		}
		hostExecs = append(hostExecs, dbsteps.HostExec{
			Host:     info.Host,
			Executor: &c001ExecAdapter{e: &runnerExecAdapter{e: info.Executor}},
		})
	}
	if len(hostExecs) == 0 {
		return fmt.Errorf("CE standby path: no usable standby executors for YAC validation")
	}

	inter := ""
	public := ""
	if v, ok := params["yac_inter_cidr"].(string); ok {
		inter = v
	}
	if v, ok := params["yac_public_network"].(string); ok {
		public = v
	}
	logger.Info("CE standby: validating YAC networks on %d standby target(s)...", len(hostExecs))
	if err := dbsteps.ValidateYACNetworksOnHosts(hostExecs, inter, public, logger); err != nil {
		return fmt.Errorf("CE standby YAC network validation failed: %w", err)
	}

	var vips []string
	switch v := params["yac_vips"].(type) {
	case []string:
		vips = v
	case []interface{}:
		for _, x := range v {
			if s, ok := x.(string); ok {
				vips = append(vips, s)
			}
		}
	}
	logger.Info("CE standby: validating VIP list on standby targets (ping from %s)...", hostExecs[0].Host)
	if err := dbsteps.ValidateYACVIPsConfigured(hostExecs, vips, logger); err != nil {
		return fmt.Errorf("CE standby VIP validation failed: %w", err)
	}
	logger.Info("CE standby: YAC network and VIP validation passed on standby targets")
	return nil
}

// prepareStandbyNodes 准备备库节点（OS 基线 + 备库侧 E-005～E-007；各步仅当出现在 filtered 中才执行，且按 E-005→E-006→E-007 顺序）
func prepareStandbyNodes(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}, osSteps []*runner.Step, filtered []*runner.Step) ([]*HostInfo, error) {
	var hostInfos []*HostInfo
	precheckFailed := false

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
			if step.ID == ossteps.FirstStepID() && result.Success {
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
				if flags.Precheck {
					precheckFailed = true
					// 当前主机上继续执行后续步骤
					continue
				}
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
		standbyPrepStepIDs := []string{
			standbyStepID("Check Standby Connectivity"),
			standbyStepID("Check Standby Begin Port Available"),
			standbyStepID("Check Standby Expansion Paths"),
		}
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
					if flags.Precheck {
						precheckFailed = true
						// 当前主机上继续执行后续步骤
						break
					}
					return nil, fmt.Errorf("step %s failed on %s: %w", step.ID, target, result.Error)
				}
				break
			}
		}
	}

	if flags.Precheck && precheckFailed {
		return hostInfos, fmt.Errorf("precheck failed")
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
		if step.ID != standbyStepID("Check Archive Destination") {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				return fmt.Errorf("precheck failed")
			}
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		break
	}

	return nil
}

// checkAndCleanupExistingNodes 在 OM 上检查并清理已存在的节点（仅当 filtered 含 E-010）
func checkAndCleanupExistingNodes(omExecutor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Checking and cleaning up existing nodes on OM if needed")

	ctx := newStandbyStepContext(&runnerExecAdapter{e: omExecutor}, logger, params, flags)

	for _, step := range filtered {
		if step.ID != standbyStepID("Check and Cleanup Existing Nodes") {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				return fmt.Errorf("precheck failed")
			}
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
		if step.ID != standbyStepID("Check Network Connectivity") {
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
				if flags.Precheck {
					return fmt.Errorf("precheck failed")
				}
				return fmt.Errorf("network connectivity check failed: %w", result.Error)
			}
		}
		break
	}

	return nil
}

// handleStandbyExpansionFailure 扩备失败：始终打印解决方案；--standby-cleanup-on-failure 或 -F 时自动安全清理。
func handleStandbyExpansionFailure(omExec ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, expandErr error) {
	if expandErr == nil || flags.Precheck || flags.DryRun {
		return
	}
	auto := standbyCleanupOnFailure || flags.ForceAll
	clusterName, _ := params["db_cluster_name"].(string)
	useCE, _ := params["standby_ce_path"].(bool)
	logger.Error("Expansion failed: %v", expandErr)
	if useCE {
		logger.Error("%s", standbysteps.FormatCEExpansionFailureRemediation(clusterName, auto))
	} else {
		logger.Error("SE expansion failed. Remediation: yasboot node remove -c %s --clean (or -n <node_id> --clean), then retry.", clusterName)
		if !auto {
			logger.Error("Tip: pass --standby-cleanup-on-failure (or -F) to auto-run safe cleanup after failure.")
		}
	}
	if !auto {
		return
	}
	logger.Info("======== Auto cleanup after expansion failure ========")
	params["standby_cleanup_on_failure"] = true
	ctx := newStandbyStepContext(&runnerExecAdapter{e: omExec}, logger, params, flags)
	if cErr := standbysteps.RunFailedExpansionCleanup(ctx, true); cErr != nil {
		logger.Error("Auto cleanup error: %v", cErr)
	}
}

// executeExpansionSteps E-011～E-013 在 OM 执行; E-014 同步检查在 Primary 执行
func executeExpansionSteps(omExec, primaryExec ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, orderedFiltered []*runner.Step, standbySteps []*runner.Step) error {
	logger.Info("Executing expansion on OM %s; sync check on primary %s", omExec.Host(), primaryExec.Host())

	omCtx := newStandbyStepContext(&runnerExecAdapter{e: omExec}, logger, params, flags)
	primaryCtx := newStandbyStepContext(&runnerExecAdapter{e: primaryExec}, logger, params, flags)

	omStepIDs := map[string]bool{
		standbyStepID("Generate Expansion Config"):   true,
		standbyStepID("Install Software on Standby"): true,
		standbyStepID("Add Standby Instance"):        true,
	}
	syncStepID := standbyStepID("Check Sync Status")

	for _, step := range standbySteps {
		var ctx *runner.StepContext
		switch {
		case omStepIDs[step.ID]:
			ctx = omCtx
		case step.ID == syncStepID:
			ctx = primaryCtx
		default:
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, orderedFiltered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				return fmt.Errorf("precheck failed")
			}
			if step.ID == syncStepID {
				return fmt.Errorf("step %s failed on primary %s (need read-write primary; set --primary-ip to current primary or switchover first): %w",
					step.ID, primaryExec.Host(), result.Error)
			}
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
	}

	return nil
}

// configureStandbyPostSteps 配置备库后续步骤（仅执行 filtered 中的 E-015、E-016、E-017，顺序与过滤列表一致）
func configureStandbyPostSteps(standbyHosts []*HostInfo, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, filtered []*runner.Step) error {
	postPhase := map[string]bool{
		standbyStepID("Configure Standby Env Vars"):  true,
		standbyStepID("Configure Standby Autostart"): true,
		standbyStepID("Verify Expansion"):            true,
	}
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
				if flags.Precheck {
					return fmt.Errorf("precheck failed")
				}
				return fmt.Errorf("step %s failed on %s: %w", step.ID, host.Host, result.Error)
			}
		}
	}

	return nil
}

// showClusterStatus 显示集群状态（仅当 filtered 含 E-019）
func showClusterStatus(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	logger.Info("Showing cluster status (via OM/yasboot host)")
	ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)
	for _, step := range filtered {
		if step.ID != standbyStepID("Show Cluster Status") {
			continue
		}
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				return fmt.Errorf("precheck failed")
			}
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		return nil
	}
	logger.Warn("%s not in filtered step list, skipping cluster status display", standbyStepID("Show Cluster Status"))
	return nil
}

// runStandbyOptionalCleanup 若 filtered 含 E-018，在主库执行；步骤为 Optional，PreCheck 不满足时跳过
func runStandbyOptionalCleanup(executor ssh.Executor, logger *logging.Logger, params map[string]interface{}, filtered []*runner.Step, flags GlobalFlags) error {
	for _, step := range filtered {
		if step.ID != standbyStepID("Cleanup Failed Expansion") {
			continue
		}
		logger.Info("======== Phase 7: Optional cleanup (E-018) ========")
		ctx := newStandbyStepContext(&runnerExecAdapter{e: executor}, logger, params, flags)
		ctx.CurrentStepID = step.ID
		setStandbyStepProgress(ctx, filtered, step)
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			if flags.Precheck {
				return fmt.Errorf("precheck failed")
			}
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
		return nil
	}
	return nil
}
