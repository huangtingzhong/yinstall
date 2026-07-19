// om.go - yinstall om 独立子命令: migrate / secondary / status（ipchange 已 Hidden）
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/runner"
	omsteps "github.com/yinstall/internal/steps/om"
	ossteps "github.com/yinstall/internal/steps/os"
)

var (
	omCmdCluster    string
	omCmdBeginPort  int
	omCmdOSUser     string
	omCmdEnvFile    string
	omCmdStageDir   string
	omCmdCurrent    string
	omCmdNew        string
	omCmdScope      string
	omCmdNewIP      string
	omCmdToml       string
	omSkipOS        bool // 与 db 一致: 默认 false, 在 --om-new 上跑 ossteps
	omMemoryPercent int
)

var omCmd = &cobra.Command{
	Use:   "om",
	Short: "Yasom (OM) operations: migrate, secondary, status",
	Long: `Standalone OM (yasom) operations without standby expansion.

Subcommands:
  migrate     Migrate primary OM (--om-new + --om or --om-current); OS baseline on --om-new like yinstall db
  secondary   Deploy secondary yasom on --targets or whole cluster (alias: deploy-secondary)
  status      Show yasboot process yasom status

Use: yinstall om <subcommand> -l`,
}

var omMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate primary OM to another host (M1/M2)",
	Long: `Migrate primary yasom to --om-new (M1 existing node or M2 new host).

Source OM: --om-current, or global -M/--om when --om-current is omitted.
OS baseline on --om-new reuses yinstall os steps (same as yinstall db):
  - default: run full OS steps (B-*) then O-* migrate steps
  - --skip-os: only B-001 connectivity on --om-new, then O-*
  - --os-* flags match yinstall db / yinstall os`,
	RunE: runOMMigrateCmd,
}

var omDeploySecondaryCmd = &cobra.Command{
	Use:     "secondary",
	Aliases: []string{"deploy-secondary"},
	Short:   "Deploy secondary yasom on targets or all cluster hosts",
	RunE:    runOMDeploySecondaryCmd,
}

var omStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show yasom process status",
	RunE:  runOMStatusCmd,
}

var omIpchangeCmd = &cobra.Command{
	Use:    "ipchange",
	Short:  "Change yasom listen IP (yasboot ipchange yasom)",
	Hidden: true, // 暂不对外展示；仍可通过 yinstall om ipchange 直接调用
	RunE:   runOMIpchangeCmd,
}

func init() {
	omCmd.PersistentFlags().StringVar(&omCmdCluster, "db-cluster-name", "yashandb", "Cluster name (same as yinstall db: non-default --db-port infers yashandb_<port> unless set)")
	omCmd.PersistentFlags().IntVar(&omCmdBeginPort, "db-port", 1688, "Database begin port (yasom = begin-13; non-default also infers stage/cluster like yinstall db)")
	omCmd.PersistentFlags().StringVar(&omCmdOSUser, "primary-os-user", "yashan", "Product OS user")
	omCmd.PersistentFlags().StringVar(&omCmdEnvFile, "primary-env-file", "", "Product env file (default ~/.yasboot/<cluster>.env)")
	omCmd.PersistentFlags().StringVar(&omCmdStageDir, "db-stage-dir", "", "Stage dir on OM (same as yinstall db: /home/<user>/install for 1688, else install_<port>)")
	// -M/--om 为根全局参数 (GlobalFlags.OmIP)
	// 与 db/os 共用 osUserPassword 变量; migrate 上 registerAllOSFlags 遇同名 persistent 会跳过
	omCmd.PersistentFlags().StringVar(&osUserPassword, "os-user-password", defaultOSUserPassword, "Product user password (M2 host add / OS baseline)")

	omMigrateCmd.Flags().StringVar(&omCmdCurrent, "om-current", "", "Current primary OM IP (optional if global -M/--om set; with --om-new enables migrate)")
	omMigrateCmd.Flags().StringVar(&omCmdNew, "om-new", "", "Target primary OM IP (required to migrate; source from --om-current or --om)")
	omMigrateCmd.Flags().StringVar(&omNewSSHUser, "om-new-ssh-user", "", "SSH user for --om-new")
	omMigrateCmd.Flags().StringVar(&omNewSSHPassword, "om-new-ssh-password", "", "SSH password for --om-new")
	omMigrateCmd.Flags().StringVar(&omNewSSHKey, "om-new-ssh-key", "", "SSH key for --om-new")
	omMigrateCmd.Flags().BoolVar(&omSkipOS, "skip-os", false, "Skip OS baseline on --om-new (default: false; same pattern as yinstall db)")
	omMigrateCmd.Flags().IntVar(&omMemoryPercent, "db-memory-percent", 50, "Planned DB memory percent (1-100) for OS shared memory when --skip-os=false")
	registerAllOSFlags(omMigrateCmd, registerOSFlagsConfig{forDB: true})

	omDeploySecondaryCmd.Flags().StringVar(&omCmdScope, "scope", "targets", "targets|cluster")

	omIpchangeCmd.Flags().StringVar(&omCmdNewIP, "new-ip", "", "New yasom IP (required)")
	omIpchangeCmd.Flags().StringVar(&omCmdToml, "toml", "", "hosts.toml path (default stage/hosts.toml)")

	omCmd.AddCommand(omMigrateCmd)
	omCmd.AddCommand(omDeploySecondaryCmd)
	omCmd.AddCommand(omStatusCmd)
	omCmd.AddCommand(omIpchangeCmd)
}

// applyOMPathDefaults 与 yinstall db/clean 共用 applyDBUserPathDefaultsFor：按 --primary-os-user 与 --db-port 推导 stage/cluster。
func applyOMPathDefaults(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	applyDBUserPathDefaultsFor(cmd, omCmdOSUser, omCmdBeginPort, &omCmdStageDir, nil, nil, nil, &omCmdCluster)
}

func omBaseParams(cmd *cobra.Command, flags GlobalFlags) map[string]interface{} {
	applyOMPathDefaults(cmd)
	params := map[string]interface{}{
		// 与 db/os/standby 一致: 非 root SSH 时 ExecuteAsUser / 特权命令走 sudo -n
		"sudo":                      flags.UseSudo,
		"db_cluster_name":           omCmdCluster,
		"db_begin_port":             omCmdBeginPort,
		"primary_os_user":           omCmdOSUser,
		"primary_env_file":          omCmdEnvFile,
		"db_stage_dir":              omCmdStageDir,
		"os_user_password":          osUserPassword,
		"om_deploy_secondary":       true,
		"om_deploy_secondary_scope": omCmdScope,
		"om_new_ssh_user":           omNewSSHUser,
		"om_new_ssh_password":       omNewSSHPassword,
		"om_new_ssh_key":            omNewSSHKey,
	}
	if len(flags.Targets) > 0 {
		params["standby_targets"] = flags.Targets
	}
	return params
}

// mergeOMOSParams 将 buildOSParams 并入 OM params (对齐 db 引用 os)。
func mergeOMOSParams(params map[string]interface{}, cmd *cobra.Command) {
	if params == nil {
		return
	}
	// 未显式 --os-user 时与 --primary-os-user 对齐
	if cmd != nil && !cmd.Flags().Changed("os-user") && strings.TrimSpace(omCmdOSUser) != "" {
		osUser = strings.TrimSpace(omCmdOSUser)
	}
	for k, v := range buildOSParams(false, 1) {
		params[k] = v
	}
	params["primary_os_user"] = strings.TrimSpace(omCmdOSUser)
	if params["primary_os_user"] == "" {
		params["primary_os_user"] = osUser
	}
	params["db_memory_percent"] = omMemoryPercent
	params["os_sysctl_shm_use_max_ram_only"] = false
	params["skip_os"] = omSkipOS
	params["with_os"] = !omSkipOS
	params["db_skip_os"] = omSkipOS
}

func omResolveOmIP() string {
	flags := GetGlobalFlags()
	extras := []string{omCmdCurrent}
	if len(flags.Targets) > 0 {
		extras = append(extras, flags.Targets[0])
	}
	return ResolveGlobalOmIP(extras...)
}

// omMigrateAllSteps 组装 OS(B-*) + O-* 迁主步, 与 db 的 OS+域步骤拼接一致。
func omMigrateAllSteps(skipOS bool) []*runner.Step {
	var all []*runner.Step
	if !skipOS {
		all = append(all, ossteps.GetAllSteps()...)
	} else {
		for _, step := range ossteps.GetAllSteps() {
			if step.ID == ossteps.FirstStepID() {
				all = append(all, step)
				break
			}
		}
	}
	all = append(all, omsteps.GetMigrateSteps()...)
	return all
}

func runOMMigrateCmd(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintOMMigrateStepCatalog(omSkipOS)
		return nil
	}
	entryOm := flags.OmIP
	cur := omsteps.ResolveOMMigrateCurrent(omCmdCurrent, entryOm)
	mig, err := omsteps.ValidateOMMigrateParams(omCmdCurrent, omCmdNew, entryOm)
	if err != nil {
		return err
	}
	if !mig {
		return fmt.Errorf("migrate requires --om-new and source OM (--om-current or global -M/--om)")
	}
	if err := validateMemoryPercent("--db-memory-percent", omMemoryPercent); err != nil {
		return err
	}
	ResolveOSUserPassword(cmd, flags, omCmdOSUser, &osUserPassword)

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("om-migrate-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return err
	}
	defer logger.Close()

	params := omBaseParams(cmd, flags)
	mergeOMOSParams(params, cmd)
	params["om_current"] = cur
	params["om_new"] = strings.TrimSpace(omCmdNew)
	params["om_ip"] = cur
	params["om_migrate"] = true
	omCurrent = cur
	omNew = strings.TrimSpace(omCmdNew)
	omCmdCurrent = cur
	if entryOm == "" {
		globalOmIP = cur
	}

	if omSkipOS {
		logger.Info("OS baseline on --om-new: SKIPPED (connectivity only)")
	} else {
		logger.Info("OS baseline on --om-new: ENABLED (reuse yinstall os steps)")
	}

	curEx, err := createStandbyOmExecutor(omCurrent, flags, logger, omsteps.FirstStepID())
	if err != nil {
		return err
	}
	defer curEx.Close()

	topo := &standbyHostExecutors{
		primaryIP:       omCurrent,
		omIP:            omCurrent,
		primary:         curEx,
		om:              curEx,
		omSameAsPrimary: true,
	}

	steps := filterSteps(omMigrateAllSteps(omSkipOS), flags)
	logger.Info("======== yinstall om migrate: %s -> %s ========", omCurrent, omNew)
	return runOMMigrateSteps(topo, logger, params, flags, steps)
}

func runOMDeploySecondaryCmd(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		printStepSection("OM secondary", omsteps.GetDeploySecondarySteps())
		return nil
	}
	scope := strings.ToLower(strings.TrimSpace(omCmdScope))
	if scope != "targets" && scope != "cluster" {
		return fmt.Errorf("--scope must be targets or cluster")
	}
	if scope == "targets" && len(flags.Targets) == 0 {
		return fmt.Errorf("--targets is required when --scope=targets")
	}
	omIP := omResolveOmIP()
	if omIP == "" {
		return fmt.Errorf("-M/--om or --targets[0] is required")
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("om-deploy-sec-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return err
	}
	defer logger.Close()

	params := omBaseParams(cmd, flags)
	params["om_ip"] = omIP
	params["om_deploy_secondary"] = true
	params["om_deploy_secondary_scope"] = scope

	omEx, err := createStandbyOmExecutor(omIP, flags, logger, omsteps.StepIDByName("OM Deploy Secondary Gate"))
	if err != nil {
		return err
	}
	defer omEx.Close()
	topo := &standbyHostExecutors{omIP: omIP, om: omEx, primary: omEx, omSameAsPrimary: true, primaryIP: omIP}

	var hosts []*HostInfo
	for _, t := range flags.Targets {
		ex, cErr := createStandbyOmExecutor(t, flags, logger, omsteps.StepIDByName("OM Deploy Secondary Host"))
		if cErr != nil {
			return cErr
		}
		hosts = append(hosts, &HostInfo{Host: t, Executor: ex})
	}
	defer closeStandbyExecutors(hosts)

	steps := filterSteps(omsteps.GetDeploySecondarySteps(), flags)
	logger.Info("======== yinstall om secondary scope=%s ========", scope)
	return runOMDeploySecondarySteps(topo, hosts, logger, params, flags, steps)
}

func runOMStatusCmd(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		fmt.Println("om status: no step catalog (direct yasboot status)")
		return nil
	}
	omIP := omResolveOmIP()
	if omIP == "" {
		return fmt.Errorf("-M/--om or --targets is required")
	}
	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("om-status-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return err
	}
	defer logger.Close()

	ex, err := createStandbyOmExecutor(omIP, flags, logger, "OM-STATUS")
	if err != nil {
		return err
	}
	defer ex.Close()

	params := omBaseParams(cmd, flags)
	params["om_ip"] = omIP
	ctx := newStandbyStepContext(&runnerExecAdapter{e: ex}, logger, params, flags)
	rows, out, err := omsteps.YasomStatus(ctx)
	if err != nil {
		logger.Error("%v\n%s", err, out)
		return err
	}
	// 终端可见完整 status 表
	fmt.Println(strings.TrimSpace(out))
	logger.Info("yasom status hosts=%d", len(rows))
	if pri := omsteps.FindPrimaryRow(rows); pri != nil {
		logger.Info("primary=%s hostid=%s", pri.IPAddr, pri.HostID)
		fmt.Printf("primary=%s hostid=%s\n", pri.IPAddr, pri.HostID)
	}
	return nil
}

func runOMIpchangeCmd(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		printStepSection("OM ipchange", omsteps.GetIpchangeSteps())
		return nil
	}
	if strings.TrimSpace(omCmdNewIP) == "" {
		return fmt.Errorf("--new-ip is required")
	}
	omIP := omResolveOmIP()
	if omIP == "" {
		return fmt.Errorf("-M/--om or --targets is required")
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("om-ipchange-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return err
	}
	defer logger.Close()

	ex, err := createStandbyOmExecutor(omIP, flags, logger, omsteps.StepIDByName("OM Ipchange Yasom"))
	if err != nil {
		return err
	}
	defer ex.Close()

	params := omBaseParams(cmd, flags)
	params["om_ip"] = omIP
	params["om_ipchange_new_ip"] = strings.TrimSpace(omCmdNewIP)
	params["om_ipchange_toml"] = strings.TrimSpace(omCmdToml)

	steps := filterSteps(omsteps.GetIpchangeSteps(), flags)
	ctx := newStandbyStepContext(&runnerExecAdapter{e: ex}, logger, params, flags)
	for _, step := range steps {
		ctx.CurrentStepID = step.ID
		result := runner.RunStep(step, ctx)
		if !result.Success && !result.Skipped {
			return fmt.Errorf("step %s failed: %w", step.ID, result.Error)
		}
	}
	return nil
}
