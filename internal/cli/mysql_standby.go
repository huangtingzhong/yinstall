package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	mysqlstandby "github.com/yinstall/internal/steps/mysql_standby"
	ossteps "github.com/yinstall/internal/steps/os"
)

var (
	mysqlStandbyPrimaryHost     string
	mysqlStandbyPrimaryPort     int
	mysqlStandbyPrimarySSHUser  string
	mysqlStandbyPrimarySSHPass  string
	mysqlStandbyPrimarySSHKey   string
	mysqlStandbyPrimaryRootPass string
	mysqlStandbyReplicaPort     int
	mysqlStandbySyncMethod      string
	mysqlStandbyRepUser         string
	mysqlStandbyRepPassword     string
	mysqlStandbyRepSSL          bool
	mysqlStandbyChannelName     string
	mysqlStandbySkipOS          bool
	mysqlStandbyEnableSemiSync  bool
	mysqlStandbyCleanupOnFail   bool
	mysqlStandbyReplicateDoDB   string
	mysqlStandbyReplicateIgnDB  string
	mysqlStandbyReadOnly        string
	mysqlStandbyCloneTimeout    int
	mysqlStandbyCloneReadyTO    int
	mysqlStandbyDumpFile        string
	mysqlStandbyDumpUser        string
	mysqlStandbyDumpPassword    string
	mysqlStandbyDumpReadyTO     int
)

var mysqlStandbyCmd = &cobra.Command{
	Use:   "standby",
	Short: "Add MySQL replica to existing primary",
	Long: `Add MySQL replica instance to an existing primary:
  - --stage all/a: install replica software + instance + replication (default)
  - --stage software/s: install replica binaries only (MR-007 + MR-018)
  - --stage instance/i: replica instance + sync; software must already exist
  - Data sync: clone (default) or remote mysqldump on replica (--sync-method dump)

Global -l/--list-steps prints MR-* catalog.`,
	RunE:         runMysqlStandby,
	SilenceUsage: true,
}

func init() {
	mysqlStandbyCmd.Flags().BoolVar(&mysqlStandbySkipOS, "skip-os", true, "Skip replica OS baseline (default true)")
	registerMysqlInstallFlags(mysqlStandbyCmd)
	if f := mysqlStandbyCmd.Flags().Lookup("stage"); f != nil {
		f.DefValue = commonmysql.DefaultStandbyStage()
		f.Usage = "Standby stage: all/a (software+replication), software/s (binary only), instance/i (instance+sync; software must exist)"
	}
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyPrimaryHost, "primary-host", "", "Primary MySQL host (required)")
	mysqlStandbyCmd.Flags().IntVar(&mysqlStandbyPrimaryPort, "primary-port", 3306, "Primary MySQL port")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyPrimarySSHUser, "primary-ssh-user", "", "Primary SSH user (defaults to --ssh-user)")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyPrimarySSHPass, "primary-ssh-password", "", "Primary SSH password")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyPrimarySSHKey, "primary-ssh-key", "", "Primary SSH key path")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyPrimaryRootPass, "primary-root-password", "", "Primary MySQL root password")
	mysqlStandbyCmd.Flags().IntVar(&mysqlStandbyReplicaPort, "replica-port", 0, "Replica MySQL port (required)")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbySyncMethod, "sync-method", "clone", "Data sync: clone or dump")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyRepUser, "rep-user", commonmysql.DefaultReplicationUser, "Replication user name")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyRepPassword, "rep-password", "", "Replication password (required)")
	mysqlStandbyCmd.Flags().BoolVar(&mysqlStandbyRepSSL, "rep-ssl", false, "CREATE USER ... REQUIRE SSL")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyChannelName, "channel-name", "", "Replication channel name")
	mysqlStandbyCmd.Flags().BoolVar(&mysqlStandbyEnableSemiSync, "enable-semi-sync", false, "Enable semi-sync after replication (default off; not configured unless set)")
	mysqlStandbyCmd.Flags().BoolVar(&mysqlStandbyCleanupOnFail, "standby-cleanup-on-failure", false, "Run MR-017 cleanup on failure")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyReplicateDoDB, "replicate-do-db", "", "Comma-separated DB whitelist")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyReplicateIgnDB, "replicate-ignore-db", "", "Comma-separated DB blacklist")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyReadOnly, "mysql-read-only", "", "Set read_only in replica cnf when 'on'")
	mysqlStandbyCmd.Flags().IntVar(&mysqlStandbyCloneTimeout, "clone-timeout", 0, "CLONE data transfer timeout in seconds (default unlimited; 0=unlimited up to 7d)")
	mysqlStandbyCmd.Flags().IntVar(&mysqlStandbyCloneReadyTO, "clone-ready-timeout", 3600, "Wait for mysqld ready after clone restart in seconds (default 1h; 0=use 1h default)")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyDumpFile, "dump-file", "", "Dump file path on replica (default: <replica -R soft dir>/yinstall_mysql_dump_<primary-port>.sql)")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyDumpUser, "dump-user", "", "Remote mysqldump user on primary (default: --rep-user; MR-004 grants extra privileges)")
	mysqlStandbyCmd.Flags().StringVar(&mysqlStandbyDumpPassword, "dump-password", "", "Remote mysqldump password when --dump-user is set (default: --rep-password)")
	mysqlStandbyCmd.Flags().IntVar(&mysqlStandbyDumpReadyTO, "dump-ready-timeout", 600, "Wait for mysqld ready after dump restore in seconds (default 10m; 0=use 10m default)")
}

func runMysqlStandby(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintMySQLStandbyStepCatalog()
		return nil
	}
	if err := validateMysqlStandbyParams(flags, mysqlStage); err != nil {
		return err
	}
	stage, err := commonmysql.ParseStage(mysqlStage)
	if err != nil {
		return err
	}

	if isLocalHost(mysqlStandbyPrimaryHost) {
		allLocal := true
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

	applyInstallArchiveDefault(cmd)
	applyMysqlPlatformDefaults(cmd, &flags, &mysqlBase)

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("mysql-standby-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	params := buildMysqlStandbyParams(flags, stage)
	params["local_mode"] = flags.Local
	params["sudo"] = flags.UseSudo
	shared := make(map[string]interface{})

	primaryUser := mysqlStandbyPrimarySSHUser
	if primaryUser == "" {
		primaryUser = flags.SSHUser
	}
	primaryPass := mysqlStandbyPrimarySSHPass
	if primaryPass == "" {
		primaryPass = flags.SSHPassword
	}
	primaryKey := mysqlStandbyPrimarySSHKey
	if primaryKey == "" {
		primaryKey = flags.SSHKeyPath
	}

	primaryExec, err := createPrimaryExecutor(PrimarySSHConfig{
		Host:     mysqlStandbyPrimaryHost,
		Port:     flags.SSHPort,
		User:     primaryUser,
		Password: primaryPass,
		KeyPath:  primaryKey,
		Auth:     flags.SSHAuth,
		Local:    flags.Local && isLocalHost(mysqlStandbyPrimaryHost),
	}, logger, "")
	if err != nil {
		return fmt.Errorf("connect primary: %w", err)
	}
	defer primaryExec.Close()

	allSteps := mysqlstandby.GetAllSteps()
	if mysqlStandbyEnableSemiSync {
		allSteps = append(allSteps, mysqlstandby.SemiSyncSteps()...)
	}
	filtered := filterSteps(allSteps, flags)
	if len(filtered) == 0 {
		logger.Info("No steps after filtering")
		return nil
	}

	logger.Info("Starting MySQL standby (RunID: %s)", rid)
	logger.Info("Standby stage: %s", stage)
	logger.Info("Primary: %s:%d", mysqlStandbyPrimaryHost, mysqlStandbyPrimaryPort)
	logger.Info("Replica targets: %v port=%d", flags.Targets, mysqlStandbyReplicaPort)

	// Phase A: primary MR-001, MR-002
	if err := runMysqlStandbyPrimarySteps(filtered, map[string]bool{"MR-001": true, "MR-002": true}, primaryExec, logger, params, flags, shared); err != nil {
		return err
	}
	mergeShared(params, shared)
	if v, ok := shared["primary_mysql_version"].(string); ok && v != "" {
		params["primary_mysql_version"] = v
	}

	var replicaHosts []*HostInfo
	for _, target := range flags.Targets {
		exec, err := createExecutor(target, flags, logger, "")
		if err != nil {
			return fmt.Errorf("connect replica %s: %w", target, err)
		}
		replicaHosts = append(replicaHosts, &HostInfo{Host: target, Executor: exec})
	}
	defer func() {
		for _, h := range replicaHosts {
			h.Executor.Close()
		}
	}()

	// Phase B: MR-006, MR-007; MR-018 only for software/all
	phaseB := map[string]bool{"MR-006": true, "MR-007": true}
	if commonmysql.StandbyIncludesSoftwareInstall(stage) {
		phaseB["MR-018"] = true
	}
	if err := runMysqlStandbyReplicaSteps(filtered, phaseB, replicaHosts, nil, logger, params, flags, shared); err != nil {
		return err
	}
	mergeShared(params, shared)
	mergeReplicaSoftwareParams(params, shared)

	if commonmysql.StandbyIncludesReplicationSetup(stage) {
		// Phase C: MR-003, MR-004, MR-005
		if err := runMysqlStandbyPrimarySteps(filtered, map[string]bool{"MR-003": true, "MR-004": true, "MR-005": true}, primaryExec, logger, params, flags, shared); err != nil {
			return err
		}
		mergeShared(params, shared)

		// Phase D: MR-009 then MR-008 on replica (cnf before instance init)
		if err := runMysqlStandbyReplicaSteps(filtered, map[string]bool{"MR-009": true}, replicaHosts, nil, logger, params, flags, shared); err != nil {
			return err
		}
		if err := runMysqlStandbyReplicaSteps(filtered, map[string]bool{"MR-008": true}, replicaHosts, nil, logger, params, flags, shared); err != nil {
			return err
		}

		// Phase E/F/G on replica
		replicaPhase := map[string]bool{}
		for _, id := range []string{"MR-010", "MR-011", "MR-013", "MR-014", "MR-015"} {
			replicaPhase[id] = true
		}
		if err := runMysqlStandbyReplicaSteps(filtered, replicaPhase, replicaHosts, primaryExec, logger, params, flags, shared); err != nil {
			if mysqlStandbyCleanupOnFail {
				_ = runMysqlStandbyReplicaSteps(filtered, map[string]bool{"MR-017": true}, replicaHosts, primaryExec, logger, params, flags, shared)
			}
			return err
		}
	}

	// MR-016 semi-sync on primary then replica
	if mysqlStandbyEnableSemiSync {
		p := copyParams(params)
		p["semi_sync_role"] = "source"
		if err := runMysqlStandbyPrimarySteps(filtered, map[string]bool{"MR-016": true}, primaryExec, logger, p, flags, shared); err != nil {
			return err
		}
		p2 := copyParams(params)
		p2["semi_sync_role"] = "replica"
		_ = runMysqlStandbyReplicaSteps(filtered, map[string]bool{"MR-016": true}, replicaHosts, primaryExec, logger, p2, flags, shared)
	}

	logger.Info("MySQL standby completed successfully")
	return nil
}

func validateMysqlStandbyParams(flags GlobalFlags, stageRaw string) error {
	stage, err := commonmysql.ParseStage(stageRaw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(mysqlStandbyPrimaryHost) == "" {
		return fmt.Errorf("--primary-host is required")
	}
	if len(flags.Targets) == 0 {
		return fmt.Errorf("--targets is required (replica host)")
	}
	if mysqlStandbyReplicaPort <= 0 {
		return fmt.Errorf("--replica-port is required")
	}
	if err := validatePorts(map[string]int{
		"--primary-port": mysqlStandbyPrimaryPort,
		"--replica-port": mysqlStandbyReplicaPort,
	}); err != nil {
		return err
	}
	if flags.DryRun || flags.Precheck {
		return nil
	}
	if commonmysql.StandbyIncludesReplicationSetup(stage) {
		if mysqlStandbyRepPassword == "" {
			return fmt.Errorf("--rep-password is required")
		}
		if mysqlStandbyPrimaryRootPass == "" {
			return fmt.Errorf("--primary-root-password is required")
		}
		sm := strings.ToLower(mysqlStandbySyncMethod)
		if sm != "clone" && sm != "dump" {
			return fmt.Errorf("--sync-method must be clone or dump")
		}
		return nil
	}
	if mysqlStandbyPrimaryRootPass == "" {
		return fmt.Errorf("--primary-root-password is required when --stage is software")
	}
	return nil
}

func buildMysqlStandbyParams(flags GlobalFlags, stage string) map[string]interface{} {
	p := buildMysqlParams(len(flags.Targets), flags, stage)
	p["primary_host"] = mysqlStandbyPrimaryHost
	p["primary_port"] = mysqlStandbyPrimaryPort
	p["primary_root_password"] = mysqlStandbyPrimaryRootPass
	p["replica_port"] = mysqlStandbyReplicaPort
	p["mysql_port"] = mysqlStandbyReplicaPort
	p["sync_method"] = strings.ToLower(mysqlStandbySyncMethod)
	p["rep_user"] = strings.TrimSpace(mysqlStandbyRepUser)
	if p["rep_user"] == "" {
		p["rep_user"] = commonmysql.DefaultReplicationUser
	}
	p["rep_password"] = mysqlStandbyRepPassword
	p["rep_ssl"] = mysqlStandbyRepSSL
	p["channel_name"] = mysqlStandbyChannelName
	if mysqlStandbyEnableSemiSync {
		p["enable_semi_sync"] = true
	}
	p["standby_cleanup_on_failure"] = mysqlStandbyCleanupOnFail
	p["replicate_do_db"] = mysqlStandbyReplicateDoDB
	p["replicate_ignore_db"] = mysqlStandbyReplicateIgnDB
	p["mysql_read_only"] = mysqlStandbyReadOnly
	p["mysql_root_password"] = mysqlRootPassword
	if strings.TrimSpace(mysqlRootPassword) == "" {
		p["mysql_root_password"] = mysqlStandbyPrimaryRootPass
	}
	p["mysql_stage"] = stage
	p["standby_stage"] = stage
	p["mysql_skip_os"] = mysqlStandbySkipOS
	p["skip_os"] = mysqlStandbySkipOS
	p["clone_timeout"] = mysqlStandbyCloneTimeout
	p["clone_ready_timeout"] = mysqlStandbyCloneReadyTO
	if strings.TrimSpace(mysqlStandbyDumpFile) != "" {
		p["dump_file"] = mysqlStandbyDumpFile
	}
	p["dump_user"] = strings.TrimSpace(mysqlStandbyDumpUser)
	p["dump_password"] = mysqlStandbyDumpPassword
	p["replica_hosts"] = flags.Targets
	if rd := strings.TrimSpace(flags.RemoteSoftwareDir); rd != "" {
		p["replica_soft_dir"] = rd
	}
	p["replica_platform"] = inferReplicaTargetPlatform(flags, p)
	p["dump_ready_timeout"] = mysqlStandbyDumpReadyTO
	return p
}

func runMysqlStandbyPrimarySteps(filtered []*runner.Step, want map[string]bool, ex ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, shared map[string]interface{}) error {
	stepParams := copyParams(params)
	stepParams["data_sync_role"] = "primary"
	ctx := newMysqlStandbyStepContext(&runnerExecAdapter{e: ex}, logger, stepParams, flags)
	ctx.TargetPlatform = inferPrimaryTargetPlatform(flags, params)
	ctx.Results = shared
	for _, step := range filtered {
		if !want[step.ID] {
			continue
		}
		ctx.CurrentStepID = step.ID
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

func runMysqlStandbyReplicaSteps(filtered []*runner.Step, want map[string]bool, hosts []*HostInfo, primaryExec ssh.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags, shared map[string]interface{}) error {
	for _, h := range hosts {
		hostParams := copyParams(params)
		hostParams["data_sync_role"] = "replica"
		ctx := newMysqlStandbyStepContext(&runnerExecAdapter{e: h.Executor}, logger, hostParams, flags)
		if h.TargetPlatform != "" {
			ctx.TargetPlatform = h.TargetPlatform
		} else {
			ctx.TargetPlatform = inferReplicaTargetPlatform(flags, hostParams)
		}
		ctx.Results = shared
		for _, step := range filtered {
			if !want[step.ID] {
				continue
			}
			ctx.CurrentStepID = step.ID
			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				if flags.Precheck {
					continue
				}
				return fmt.Errorf("step %s on %s failed: %w", step.ID, h.Host, result.Error)
			}
		}
	}
	return nil
}

func mergeShared(params map[string]interface{}, shared map[string]interface{}) {
	for k, v := range shared {
		params[k] = v
	}
}

func mergeReplicaSoftwareParams(params, shared map[string]interface{}) {
	if v, ok := shared["replica_mysql_version"].(string); ok && strings.TrimSpace(v) != "" {
		params["mysql_version"] = strings.TrimSpace(v)
	}
	if v, ok := shared["replica_mysql_home"].(string); ok && strings.TrimSpace(v) != "" {
		params["mysql_home"] = strings.TrimSpace(v)
	}
	if v, ok := shared["mysql_package"].(string); ok && strings.TrimSpace(v) != "" {
		params["mysql_package"] = strings.TrimSpace(v)
	}
}

func copyParams(p map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}

// OS steps for standby replica when skip-os=false (B-001 only when skip-os=true handled in install path)
func mysqlStandbyOSSteps(skipOS bool) []*runner.Step {
	if skipOS {
		for _, s := range ossteps.GetAllSteps() {
			if s.ID == "B-001" {
				return []*runner.Step{s}
			}
		}
		return nil
	}
	return filterOSStepsForMySQL(ossteps.GetAllSteps())
}
