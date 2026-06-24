package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	commonmssql "github.com/yinstall/internal/common/mssql"
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	mssqlsteps "github.com/yinstall/internal/steps/mssql"
	ossteps "github.com/yinstall/internal/steps/os"
	winsteps "github.com/yinstall/internal/steps/win_os"
	"github.com/yinstall/internal/winrm"
)

var (
	mssqlSkipOS                bool
	mssqlDataRoot              string
	mssqlSQLDataDir            string
	mssqlSQLLogDir             string
	mssqlSQLBackupDir          string
	mssqlProgramDir            string
	mssqlInstanceDir           string
	mssqlDatabase              string
	mssqlData                  string
	mssqlLog                   string
	mssqlBackup                string
	mssqlPort                  string
	mssqlHAEndpointPort        int
	mssqlInstance              string
	mssqlDomainMode            string
	mssqlSaPassword            string
	mssqlSetupPackage          string
	mssqlSetupUNC              string
	mssqlCUPackage             string
	mssqlCollation             string
	mssqlRemoteSA              bool
	mssqlMaxMemoryMB           int
	mssqlMemoryPercent         int
	mssqlStage                 string
	mssqlEnvFile               string
	mssqlCustomSQLScript       string
	mssqlSetupQuiet            bool
	mssqlIgnoreLocaleCheck     bool
	mssqlTopology              string
	mssqlSqlsvcAccount         string
	mssqlSqlsvcPassword        string
	mssqlPrimaryHost           string
	mssqlAGName                string
	mssqlAGListener            string
	mssqlAGListenerPort        int
	mssqlAGListenerIP          string
	mssqlAGDB                  string
	mssqlAGSeedingMode         string
	mssqlAGSeedingUNC          string
	mssqlHAStage               string
	mssqlHASkipInstall         bool
	mssqlMirrorDB              string
	mssqlMirrorRecover         bool
	mssqlMirrorDropSecondaryDB bool
	mssqlMirrorCertValidDays   int
	mssqlMirrorDropExisting    bool
	mssqlMirrorSkipSeed        bool
	mssqlMirrorWorkDir         string
	mssqlMirrorRestoreFrom     string
	mssqlMirrorBackupDir       string
	mssqlPrimarySSHUser        string
	mssqlPrimarySSHPass        string
	mssqlPrimarySSHKey         string
	mssqlForceHaCerts          bool
	mssqlPrimaryInstance       string
	mssqlReplicaInstance       string
	mssqlPrimaryPort           string
	mssqlReplicaPort           string
	mssqlPrimaryHAEndpointPort int
	mssqlReplicaHAEndpointPort int
	mssqlListInstances         bool
	mssqlReplicaRestoreDataDir string
	mssqlReplicaRestoreLogDir  string
)

const defaultWinRMPort = 5985

func buildMssqlProfile(params map[string]interface{}) commonwin.Profile {
	topology := commonmssql.Topology(mssqlTopology)
	if topology == "" {
		topology = commonmssql.TopologyStandalone
	}
	p := commonmssql.WinOSProfileForMssql(topology, params)
	p.SpnMode = commonmssql.DeriveSpnMode(commonmssql.NormalizeDomainMode(mssqlDomainMode), string(topology))
	return commonwin.ApplyParams(p, params)
}

// buildMssqlBaseParams holds SSH/Windows/SQL connection parameters shared by
// install, mirror, and ag subcommands. Subcommand-specific params are added by
// buildMssqlParams (install), buildMssqlMirrorParams (mirror), and
// buildMssqlAGParams (ag).
func buildMssqlBaseParams(flags GlobalFlags) map[string]interface{} {
	profile := buildMssqlProfile(nil)
	p := buildWinOSParams(mssqlSkipOS, profile)
	delete(p, "os_hostname")
	p["local_mode"] = flags.Local
	p["windows_transport"] = "auto"
	p["winrm_port"] = defaultWinRMPort
	layout := commonmssql.ResolveLayout(mssqlLayoutResolveParams(flags.RemoteSoftwareDir))
	portParam, _ := commonmssql.NormalizePortParam(mssqlPort)
	p["mssql_port"] = portParam
	p["mssql_ha_endpoint_port"] = mssqlHAEndpointPort
	p["mssql_instance"] = mssqlInstance
	dm := commonmssql.NormalizeDomainMode(mssqlDomainMode)
	topology := strings.TrimSpace(mssqlTopology)
	if topology == "" {
		topology = string(commonmssql.TopologyStandalone)
	}
	p["mssql_domain_mode"] = dm
	p["os_domain_mode"] = dm
	p["mssql_topology"] = topology
	p["os_spn_mode"] = commonmssql.DeriveSpnMode(dm, topology)
	if acct := strings.TrimSpace(mssqlSqlsvcAccount); acct != "" {
		p["os_service_account"] = acct
	}
	p["mssql_setup_package"] = mssqlSetupPackage
	p["mssql_setup_unc"] = mssqlSetupUNC
	p["mssql_cu_package"] = mssqlCUPackage
	p["mssql_setup_quiet"] = mssqlSetupQuiet
	p["mssql_ignore_locale_check"] = mssqlIgnoreLocaleCheck
	p["mssql_sqlsvc_account"] = mssqlSqlsvcAccount
	p["mssql_sqlsvc_password"] = mssqlSqlsvcPassword
	p["os_product_base"] = layout.AdminBase
	if strings.TrimSpace(osLocalMount) == "" {
		p["os_local_mount"] = layout.AdminBase
	}
	ports := fmt.Sprintf("%d,%d,445", commonmssql.CLIFirewallSQLPort(mssqlPort), mssqlHAEndpointPort)
	if strings.TrimSpace(osFirewallPorts) != "" {
		ports = ports + "," + osFirewallPorts
	}
	p["os_firewall_ports"] = ports
	if inst := strings.TrimSpace(mssqlInstance); inst != "" && !commonmssql.IsInstanceAuto(inst) && !strings.EqualFold(inst, commonmssql.DefaultInstance) {
		p["mssql_service_name"] = commonmssql.DefaultSQLSvcAccount(inst)
	}
	return p
}

// buildMssqlSSHParams populates primary_ssh_* and replica_ssh_* from CLI flags.
// Shared by mirror and ag subcommands.
func buildMssqlSSHParams(p map[string]interface{}, flags GlobalFlags) {
	pu := strings.TrimSpace(mssqlPrimarySSHUser)
	if pu == "" {
		pu = flags.SSHUser
	}
	if pu == "" {
		pu = "Administrator"
	}
	pp := mssqlPrimarySSHPass
	if pp == "" {
		pp = flags.SSHPassword
	}
	p["primary_ssh_user"] = pu
	p["primary_ssh_password"] = pp
	p["primary_ssh_key"] = mssqlPrimarySSHKey
	ru := strings.TrimSpace(flags.SSHUser)
	if ru == "" {
		ru = "Administrator"
	}
	p["replica_ssh_password"] = flags.SSHPassword
	p["replica_ssh_user"] = ru
}

// setMssqlSAPasswordParam writes mssql_sa_password: explicit flag, install default, or empty.
func setMssqlSAPasswordParam(p map[string]interface{}, includesInstall bool) {
	if p == nil {
		return
	}
	p["mssql_sa_password"] = commonmssql.ResolveSAPassword(mssqlSaPassword, includesInstall)
}

func buildMssqlParams(flags GlobalFlags) map[string]interface{} {
	p := buildMssqlBaseParams(flags)
	applyMssqlPathParams(p)
	p["mssql_collation"] = mssqlCollation
	p["mssql_remote_sa"] = mssqlRemoteSA
	p["mssql_max_memory_mb"] = mssqlMaxMemoryMB
	p["mssql_memory_percent"] = mssqlMemoryPercent
	p["mssql_stage"] = mssqlStage
	p["mssql_env_file"] = mssqlEnvFile
	p["mssql_custom_sql_script"] = mssqlCustomSQLScript
	return p
}

func buildMssqlAllSteps(skipOS bool, profile commonwin.Profile) []*runner.Step {
	var all []*runner.Step
	all = append(all, ossteps.StepB001CheckConnectivity())
	if !skipOS {
		all = append(all, winsteps.GetPreInstanceSteps(profile)...)
	}
	all = append(all, mssqlsteps.GetAllSteps()...)
	if !skipOS {
		all = append(all, winsteps.GetPostInstanceSteps(profile)...)
	}
	return all
}

type mssqlStepGroups struct {
	b001       *runner.Step
	preOS      []*runner.Step
	mssqlSteps []*runner.Step
	postOS     []*runner.Step
	ms001      *runner.Step
}

// mssqlInstallRequiresSAPassword is true when filtered steps include MSSQL instance
// work (MS-002+) that needs --mssql-sa-password. OS-only (W-*) and MS-001 do not.
func mssqlInstallRequiresSAPassword(steps []*runner.Step) bool {
	for _, s := range steps {
		if s == nil {
			continue
		}
		if strings.HasPrefix(s.ID, "MS-") && s.ID != "MS-001" {
			return true
		}
	}
	return false
}

func splitMssqlSteps(steps []*runner.Step) mssqlStepGroups {
	var g mssqlStepGroups
	for _, s := range steps {
		if s == nil {
			continue
		}
		switch {
		case s.ID == "B-001":
			g.b001 = s
		case s.ID == "MS-001":
			g.ms001 = s
		case strings.HasPrefix(s.ID, "W-"):
			if s.ID == "W-012" || s.ID == "W-014" {
				g.postOS = append(g.postOS, s)
			} else {
				g.preOS = append(g.preOS, s)
			}
		case strings.HasPrefix(s.ID, "MS-"):
			g.mssqlSteps = append(g.mssqlSteps, s)
		}
	}
	return g
}

func trySSHWindows(target string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	cfg := ssh.Config{
		Host:       target,
		Port:       flags.SSHPort,
		User:       flags.SSHUser,
		AuthMethod: flags.SSHAuth,
		Password:   flags.SSHPassword,
		KeyPath:    flags.SSHKeyPath,
		StepID:     stepID,
		TargetOS:   ssh.TargetOSWindows,
		Logger:     logger,
	}
	return connectSSHWithRetry(cfg, flags.SSHPassword != "", logger)
}

func applyMssqlLocalDefaults(flags *GlobalFlags) error {
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}
	if flags.Local && runtime.GOOS != "windows" {
		return fmt.Errorf("local mssql requires Windows control host; use -t HOST")
	}
	return nil
}

func applyMssqlRemoteSoftwareDefaults(cmd *cobra.Command, flags *GlobalFlags) {
	if cmd != nil && cmd.Flags().Changed("remote-software-dir") {
		return
	}
	dir := commonmssql.DefaultRemoteSoftwareDir()
	remoteSoftwareDir = dir
	flags.RemoteSoftwareDir = dir
}

func registerMssqlInstallFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&mssqlSkipOS, "skip-os", false, "Skip Windows OS baseline W-003..W-014")
	registerMssqlOSFlags(cmd)
	registerMssqlPathFlags(cmd)
	cmd.Flags().StringVar(&mssqlPort, "mssql-port", commonmssql.PortAuto, "SQL TCP port (auto or 1-65535); auto discovers from registry when --mssql-instance is auto")
	cmd.Flags().IntVar(&mssqlHAEndpointPort, "mssql-ha-endpoint-port", 5022, "HADR endpoint port")
	cmd.Flags().StringVar(&mssqlInstance, "mssql-instance", commonmssql.InstanceAuto, "SQL instance name (auto or name); auto discovers from registry (single instance) or by --mssql-port")
	cmd.Flags().StringVar(&mssqlDomainMode, "mssql-domain-mode", "workgroup", "auto|domain|workgroup (default workgroup; domain not supported yet; workgroup skips W-014 SPN)")
	cmd.Flags().StringVar(&mssqlSaPassword, "mssql-sa-password", "", "SA password for SQL install (default aaBB11@@ when installing; empty for HA/remove uses Windows auth)")
	cmd.Flags().StringVar(&mssqlSetupPackage, "mssql-setup-package", "", "Setup media: ISO path, extracted dir, or filename (auto-search -L/-R if empty)")
	cmd.Flags().StringVar(&mssqlSetupUNC, "mssql-setup-unc", "", "UNC setup media path on target")
	cmd.Flags().StringVar(&mssqlCUPackage, "mssql-cu-package", "", "CU/SP setup.exe path")
	cmd.Flags().StringVar(&mssqlCollation, "mssql-collation", "Chinese_PRC_CI_AS", "SQL collation")
	cmd.Flags().BoolVar(&mssqlRemoteSA, "mssql-remote-sa", true, "Allow remote sa login")
	cmd.Flags().IntVar(&mssqlMaxMemoryMB, "mssql-max-memory-mb", 0, "SQL max server memory in MB (0=use --mssql-memory-percent)")
	cmd.Flags().IntVar(&mssqlMemoryPercent, "mssql-memory-percent", 90, "Max server memory as percent of RAM when --mssql-max-memory-mb=0; 0=skip MS-018")
	cmd.Flags().StringVar(&mssqlStage, "stage", commonmssql.DefaultInstallStage(), "Install stage: all/a (software+instance), software/s (media only)")
	cmd.Flags().StringVar(&mssqlEnvFile, "mssql-env-file", "", "Instance profile path under SSH user home (default: ~\\{port}.ps1)")
	cmd.Flags().StringVar(&mssqlCustomSQLScript, "mssql-custom-sql-script", "", "Custom SQL script after install")
	cmd.Flags().BoolVar(&mssqlSetupQuiet, "mssql-setup-quiet", true, "setup.exe /QS")
	cmd.Flags().BoolVar(&mssqlIgnoreLocaleCheck, "mssql-ignore-locale-check", false, "Skip SQL setup media vs OS locale precheck (MS-008)")
	cmd.Flags().StringVar(&mssqlSqlsvcAccount, "mssql-sqlsvc-account", "", "Domain SQL service account (MS-012, W-014 SPN)")
	cmd.Flags().StringVar(&mssqlSqlsvcPassword, "mssql-sqlsvc-password", "", "SQL service account password (MS-012)")
}

func mssqlFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func effectiveMssqlDataRoot() string {
	return mssqlFirstNonEmpty(mssqlDataRoot, mssqlDatabase)
}

func effectiveMssqlSQLDataDir() string {
	return mssqlFirstNonEmpty(mssqlSQLDataDir, mssqlData)
}

func effectiveMssqlSQLLogDir() string {
	return mssqlFirstNonEmpty(mssqlSQLLogDir, mssqlLog)
}

func effectiveMssqlSQLBackupDir() string {
	return mssqlFirstNonEmpty(mssqlSQLBackupDir, mssqlBackup)
}

func mssqlLayoutResolveParams(softwareDir string) map[string]interface{} {
	return map[string]interface{}{
		"mssql_data_root":    effectiveMssqlDataRoot(),
		"mssql_database":     effectiveMssqlDataRoot(),
		"mssql_data_dir":     effectiveMssqlSQLDataDir(),
		"mssql_data":         effectiveMssqlSQLDataDir(),
		"mssql_log_dir":      effectiveMssqlSQLLogDir(),
		"mssql_log":          effectiveMssqlSQLLogDir(),
		"mssql_backup_dir":   effectiveMssqlSQLBackupDir(),
		"mssql_backup":       effectiveMssqlSQLBackupDir(),
		"mssql_program_dir":  strings.TrimSpace(mssqlProgramDir),
		"mssql_instance_dir": strings.TrimSpace(mssqlInstanceDir),
		"mssql_software_dir": softwareDir,
		"mssql_instance":     mssqlInstance,
	}
}

func applyMssqlPathParams(p map[string]interface{}) {
	dataRoot := effectiveMssqlDataRoot()
	dataDir := effectiveMssqlSQLDataDir()
	logDir := effectiveMssqlSQLLogDir()
	backupDir := effectiveMssqlSQLBackupDir()
	p["mssql_data_root"] = dataRoot
	p["mssql_database"] = dataRoot
	p["mssql_data_dir"] = dataDir
	p["mssql_data"] = dataDir
	p["mssql_log_dir"] = logDir
	p["mssql_log"] = logDir
	p["mssql_backup_dir"] = backupDir
	p["mssql_backup"] = backupDir
	p["mssql_program_dir"] = strings.TrimSpace(mssqlProgramDir)
	p["mssql_instance_dir"] = strings.TrimSpace(mssqlInstanceDir)
}

func applyMssqlRestorePathParams(p map[string]interface{}) {
	dataDir := strings.TrimSpace(mssqlReplicaRestoreDataDir)
	logDir := strings.TrimSpace(mssqlReplicaRestoreLogDir)
	if dataDir != "" {
		p["mssql_restore_data_dir"] = dataDir
		p["replica_mssql_restore_data_dir"] = dataDir
	}
	if logDir != "" {
		p["mssql_restore_log_dir"] = logDir
		p["replica_mssql_restore_log_dir"] = logDir
	}
}

func registerMssqlPathFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&mssqlDataRoot, "mssql-data-root", "", "Database files root (enables custom data layout under {root}/{instance})")
	cmd.Flags().StringVar(&mssqlSQLDataDir, "mssql-data-dir", "", "User database file directory")
	cmd.Flags().StringVar(&mssqlSQLLogDir, "mssql-log-dir", "", "User database log directory")
	cmd.Flags().StringVar(&mssqlSQLBackupDir, "mssql-backup-dir", "", "Backup directory")
	cmd.Flags().StringVar(&mssqlProgramDir, "mssql-program-dir", "", "SQL Server program root (Microsoft SQL Server layer)")
	cmd.Flags().StringVar(&mssqlInstanceDir, "mssql-instance-dir", "", "SQL instance program directory (INSTANCEDIR)")
	cmd.Flags().StringVar(&mssqlDatabase, "database", "", "Deprecated: use --mssql-data-root")
	cmd.Flags().StringVar(&mssqlData, "data", "", "Deprecated: use --mssql-data-dir")
	cmd.Flags().StringVar(&mssqlLog, "log", "", "Deprecated: use --mssql-log-dir")
	cmd.Flags().StringVar(&mssqlBackup, "backup", "", "Deprecated: use --mssql-backup-dir")
	_ = cmd.Flags().MarkDeprecated("database", "use --mssql-data-root")
	_ = cmd.Flags().MarkDeprecated("data", "use --mssql-data-dir")
	_ = cmd.Flags().MarkDeprecated("log", "use --mssql-log-dir")
	_ = cmd.Flags().MarkDeprecated("backup", "use --mssql-backup-dir")
}

// registerMssqlHAInstanceFlags adds per-host instance/port/endpoint flags (mirror/ag add and remove).
func registerMssqlHAInstanceFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&mssqlPrimaryInstance, "primary-mssql-instance", commonmssql.InstanceAuto,
		"Primary SQL instance name (auto or name); overrides --mssql-instance on primary host")
	cmd.Flags().StringVar(&mssqlReplicaInstance, "replica-mssql-instance", commonmssql.InstanceAuto,
		"Replica SQL instance name (auto or name); overrides --mssql-instance on replica hosts")
	cmd.Flags().StringVar(&mssqlPrimaryPort, "primary-mssql-port", commonmssql.PortAuto,
		"Primary SQL TCP port (auto or 1-65535)")
	cmd.Flags().StringVar(&mssqlReplicaPort, "replica-mssql-port", commonmssql.PortAuto,
		"Replica SQL TCP port (auto or 1-65535)")
	cmd.Flags().IntVar(&mssqlPrimaryHAEndpointPort, "primary-mssql-ha-endpoint-port", 0,
		"Primary HA/mirror endpoint port (0 = use --mssql-ha-endpoint-port)")
	cmd.Flags().IntVar(&mssqlReplicaHAEndpointPort, "replica-mssql-ha-endpoint-port", 0,
		"Replica HA/mirror endpoint port (0 = use --mssql-ha-endpoint-port; use when multiple instances share a host)")
	registerMssqlListInstancesFlag(cmd)
}

// registerMssqlHAExtensionFlags adds instance flags plus force-ha-certs (mirror/ag add/rebuild only).
func registerMssqlHAExtensionFlags(cmd *cobra.Command) {
	registerMssqlHAInstanceFlags(cmd)
	cmd.Flags().BoolVar(&mssqlForceHaCerts, "mssql-force-ha-certs", false,
		"Allow drop/recreate HA certificates and endpoints (mirror/AG); decoupled from -f/-F")
	cmd.Flags().StringVar(&mssqlReplicaRestoreDataDir, "replica-mssql-restore-data-dir", "",
		"Secondary RESTORE WITH MOVE data directory (default: target instance registry layout)")
	cmd.Flags().StringVar(&mssqlReplicaRestoreLogDir, "replica-mssql-restore-log-dir", "",
		"Secondary RESTORE WITH MOVE log directory (default: restore data dir or registry log dir)")
}

func applyMssqlHAInstanceParams(p map[string]interface{}) {
	p["mssql_force_ha_certs"] = mssqlForceHaCerts
	if inst := strings.TrimSpace(mssqlPrimaryInstance); inst != "" && !commonmssql.IsInstanceAuto(inst) {
		p["mssql_primary_instance"] = inst
	}
	if inst := strings.TrimSpace(mssqlReplicaInstance); inst != "" && !commonmssql.IsInstanceAuto(inst) {
		p["mssql_replica_instance"] = inst
	}
	if port, err := commonmssql.NormalizePortParam(mssqlPrimaryPort); err == nil && !commonmssql.IsPortAuto(port) {
		p["mssql_primary_port"] = port
	}
	if port, err := commonmssql.NormalizePortParam(mssqlReplicaPort); err == nil && !commonmssql.IsPortAuto(port) {
		p["mssql_replica_port"] = port
	}
	if mssqlPrimaryHAEndpointPort > 0 {
		p["mssql_primary_ha_endpoint_port"] = mssqlPrimaryHAEndpointPort
	}
	if mssqlReplicaHAEndpointPort > 0 {
		p["mssql_replica_ha_endpoint_port"] = mssqlReplicaHAEndpointPort
	}
}

func registerMssqlListInstancesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&mssqlListInstances, "list-instances", false,
		"List SQL Server instances from registry on target host(s) and exit")
}

func createWinRMExecutorWithCreds(target, user, password string, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	return winrm.ConnectWithRetry(winrm.Config{
		Host:     target,
		Port:     defaultWinRMPort,
		User:     user,
		Password: password,
		UseSSL:   false,
		Auth:     "negotiate",
		Logger:   logger,
		StepID:   stepID,
	}, sshConnectMaxRetries, sshConnectRetryDelay)
}

func createWinRMExecutor(target string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	return createWinRMExecutorWithCreds(target, flags.SSHUser, flags.SSHPassword, logger, stepID)
}

// runMssqlListInstancesIfRequested lists SQL instances on hosts and exits when --list-instances is set.
func runMssqlListInstancesIfRequested(flags GlobalFlags, logger *logging.Logger, hosts []string, params map[string]interface{}) (bool, error) {
	if !mssqlListInstances {
		return false, nil
	}
	if len(hosts) == 0 {
		return true, fmt.Errorf("--list-instances requires at least one -t target")
	}
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		exec, err := createWindowsExecutor(host, flags, logger, "list-instances")
		if err != nil {
			return true, fmt.Errorf("connect %s: %w", host, err)
		}
		ctx := &runner.StepContext{
			Executor:       &runnerExecAdapter{e: exec},
			Logger:         logger,
			Params:         params,
			TargetPlatform: "windows",
		}
		if err := commonmssql.ListInstancesOnHost(ctx); err != nil {
			exec.Close()
			return true, fmt.Errorf("list instances on %s: %w", host, err)
		}
		exec.Close()
	}
	return true, nil
}

func filterMssqlInstallStepsByStage(steps []*runner.Step, stage string) []*runner.Step {
	if stage == commonmssql.StageAll {
		return steps
	}
	out := make([]*runner.Step, 0, len(steps))
	for _, step := range steps {
		if step == nil {
			continue
		}
		if strings.HasPrefix(step.ID, "B-") || strings.HasPrefix(step.ID, "W-") {
			out = append(out, step)
			continue
		}
		if commonmssql.StepMatchesInstallStage(step, stage) {
			out = append(out, step)
		}
	}
	return out
}

func validateMssqlPortFlag() error {
	_, err := commonmssql.NormalizePortParam(mssqlPort)
	return err
}

func validateMssqlInstallStage(stage string, dryRun, precheck bool) error {
	normalized, err := commonmssql.ParseStage(stage)
	if err != nil {
		return err
	}
	if dryRun || precheck {
		return nil
	}
	switch normalized {
	case commonmssql.StageSoftware:
		if strings.TrimSpace(mssqlSetupPackage) == "" && strings.TrimSpace(mssqlSetupUNC) == "" {
			// local -L / remote -R auto-discovery still allowed; MS-004 validates at runtime.
		}
	}
	return nil
}

func normalizeMssqlInstallStage(raw string) (string, error) {
	return commonmssql.ParseStage(raw)
}

// ---- Shared HA/mirror/AG helpers (originally in mssql_ha.go, now shared) ----

// validateMssqlHAParams validates --primary-host + -t params for mirror/AG remove subcommands.
func validateMssqlHAParams(flags *GlobalFlags) error {
	if strings.TrimSpace(mssqlPrimaryHost) == "" {
		return fmt.Errorf("--primary-host is required")
	}
	if len(flags.Targets) == 0 {
		return fmt.Errorf("-t is required (replica host(s); primary is --primary-host only)")
	}
	primary := strings.TrimSpace(mssqlPrimaryHost)
	replicas := make([]string, 0, len(flags.Targets))
	for _, t := range flags.Targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.EqualFold(t, primary) {
			return fmt.Errorf("primary host %q must not appear in -t; use --primary-host only", t)
		}
		replicas = append(replicas, t)
	}
	if len(replicas) == 0 {
		return fmt.Errorf("at least one replica target is required in -t")
	}
	flags.Targets = replicas
	return nil
}

// RunMssqlHARemoveStepsOnHosts runs B-001 then round-robin remove steps on primary + replicas.
func RunMssqlHARemoveStepsOnHosts(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}, allSteps []*runner.Step, phaseName string) error {
	steps := ensureConnectivityStep(allSteps, filterSteps(allSteps, flags))
	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}
	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}
	planned := runner.CountNonOptionalSteps(steps)
	progress := runner.NewStepProgress(planned)
	totalSteps := progress.Total()
	sharedResults := make(map[string]interface{})

	hostInfos, stepIdx, err := runMssqlHAConnectivityPhase(steps[0], flags, params, logger, 0, totalSteps, progress, sharedResults)
	if err != nil {
		return err
	}
	defer closeHostInfos(hostInfos)

	mshSteps := steps[1:]
	if len(mshSteps) > 0 {
		logger.Info("======== Phase: %s ========", phaseName)
		res := RunRoundRobinPerHostStepsEx(mshSteps, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}
	return nil
}

func primarySSHConfig(flags GlobalFlags) PrimarySSHConfig {
	user := strings.TrimSpace(mssqlPrimarySSHUser)
	if user == "" {
		user = flags.SSHUser
	}
	pass := mssqlPrimarySSHPass
	if pass == "" {
		pass = flags.SSHPassword
	}
	key := mssqlPrimarySSHKey
	if key == "" {
		key = flags.SSHKeyPath
	}
	return PrimarySSHConfig{
		Host:     strings.TrimSpace(mssqlPrimaryHost),
		Port:     flags.SSHPort,
		User:     user,
		Password: pass,
		KeyPath:  key,
		Auth:     flags.SSHAuth,
		Local:    flags.Local && isLocalHost(mssqlPrimaryHost),
	}
}

func runMssqlHAConnectivityPhase(
	connectivityStep *runner.Step,
	flags GlobalFlags,
	params map[string]interface{},
	logger *logging.Logger,
	stepIndex int,
	totalSteps int,
	progress *runner.StepProgress,
	sharedResults map[string]interface{},
) ([]*HostInfo, int, error) {
	if connectivityStep == nil {
		return nil, stepIndex, fmt.Errorf("B-001 step missing")
	}
	logger.Info("======== Phase 1: Connectivity check ========")

	var hostInfos []*HostInfo
	precheckFailed := false

	runB001 := func(host string, exec ssh.Executor, platform string) error {
		ctx := &runner.StepContext{
			Executor:       &runnerExecAdapter{e: exec},
			Logger:         logger,
			Params:         params,
			DryRun:         flags.DryRun,
			Precheck:       flags.Precheck,
			Results:        sharedResults,
			ForceAll:       flags.ForceAll,
			ForceSteps:     flags.ForceSteps,
			StepIndex:      stepIndex,
			TotalSteps:     totalSteps,
			Progress:       progress,
			TargetPlatform: platform,
		}
		result := runner.RunStep(connectivityStep, ctx)
		if result.Error != nil && !result.Skipped {
			return result.Error
		}
		return nil
	}

	pcfg := primarySSHConfig(flags)
	primaryExec, err := createWindowsPrimaryExecutor(pcfg, logger, connectivityStep.ID)
	if err != nil {
		if flags.Precheck {
			precheckFailed = true
		} else {
			return nil, stepIndex, fmt.Errorf("connectivity failed for primary %s: %w", pcfg.Host, err)
		}
	} else {
		if err := runB001(pcfg.Host, primaryExec, "windows"); err != nil {
			primaryExec.Close()
			if flags.Precheck {
				precheckFailed = true
			} else {
				return nil, stepIndex, fmt.Errorf("connectivity check failed for primary %s: %w", pcfg.Host, err)
			}
		} else {
			hostInfos = append(hostInfos, &HostInfo{Host: pcfg.Host, Executor: primaryExec, TargetPlatform: "windows"})
		}
	}

	for _, target := range flags.Targets {
		executor, err := createWindowsExecutor(target, flags, logger, connectivityStep.ID)
		if err != nil {
			logger.Error("Failed to connect to replica %s: %v", target, err)
			if flags.Precheck {
				precheckFailed = true
				continue
			}
			closeHostInfos(hostInfos)
			return nil, stepIndex, fmt.Errorf("connectivity failed for replica %s: %w", target, err)
		}
		if err := runB001(target, executor, "windows"); err != nil {
			executor.Close()
			if flags.Precheck {
				precheckFailed = true
				continue
			}
			closeHostInfos(hostInfos)
			return nil, stepIndex, fmt.Errorf("connectivity check failed for replica %s: %w", target, err)
		}
		hostInfos = append(hostInfos, &HostInfo{Host: target, Executor: executor, TargetPlatform: "windows"})
	}

	if precheckFailed {
		return hostInfos, stepIndex + 1, fmt.Errorf("precheck connectivity failed")
	}
	if len(hostInfos) == 0 {
		return nil, stepIndex, fmt.Errorf("no reachable hosts")
	}
	return hostInfos, stepIndex + 1, nil
}

func partitionHAHostInfos(hostInfos []*HostInfo, primaryHost string) (primary *HostInfo, replicas []*HostInfo) {
	primaryHost = strings.TrimSpace(primaryHost)
	for _, h := range hostInfos {
		if h == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(h.Host), primaryHost) {
			primary = h
		} else {
			replicas = append(replicas, h)
		}
	}
	return primary, replicas
}

func runMssqlHASingleStep(step *runner.Step, host *HostInfo, params map[string]interface{}, flags GlobalFlags, logger *logging.Logger, shared map[string]interface{}, progress *runner.StepProgress) error {
	if step == nil || host == nil {
		return nil
	}
	ctx := &runner.StepContext{
		Executor:          &runnerExecAdapter{e: host.Executor},
		Logger:            logger,
		Params:            params,
		DryRun:            flags.DryRun,
		Precheck:          flags.Precheck,
		Results:           shared,
		LocalSoftwareDirs: flags.LocalSoftwareDirs,
		RemoteSoftwareDir: flags.RemoteSoftwareDir,
		ForceAll:          flags.ForceAll,
		ForceSteps:        flags.ForceSteps,
		TargetPlatform:    "windows",
		Progress:          progress,
	}
	result := runner.RunStep(step, ctx)
	if !result.Success && !result.Skipped {
		if flags.Precheck {
			return nil
		}
		return result.Error
	}
	return nil
}
