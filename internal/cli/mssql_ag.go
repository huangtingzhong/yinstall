package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	commonmssql "github.com/yinstall/internal/common/mssql"
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	agsteps "github.com/yinstall/internal/steps/mssql_ag"
	ossteps "github.com/yinstall/internal/steps/os"
	winsteps "github.com/yinstall/internal/steps/win_os"
)

var mssqlAGCmd = &cobra.Command{
	Use:   "ag",
	Short: "Configure MSSQL Always On availability group",
	Long: `Configure MSSQL Always On availability group across a primary
(--primary-host) and replicas (-t). Requires a pre-existing WSFC cluster
(configured externally). Three modes:

  Add node (default):
    yinstall mssql ag --primary-host PRIMARY -t REPLICA --mssql-ag-db DB [--stage ha]

  Remove node:
    yinstall mssql ag remove --primary-host PRIMARY -t REPLICA

  Rebuild (no --primary-host; -t[0] is primary, -t[1:] are replicas):
    yinstall mssql ag -t PRIMARY,REPLICA --mssql-ag-db DB [--stage ha]
    Rebuild is idempotent: skips when current topology already matches -t.

AG uses cert-based HADR endpoint auth. --mssql-domain-mode controls SPN/OS
baseline (workgroup skips W-014 SPN registration).`,
	RunE:         runMssqlAG,
	SilenceUsage: true,
}

var mssqlAGRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove Always On availability group",
	Long: `Remove Always On availability group artifacts.

Runs on primary (--primary-host) and replica nodes (-t):
  - A-052: DROP AVAILABILITY GROUP on primary
  - A-053: optional DROP DATABASE on secondary (--ag-drop-secondary-db)

WSFC cluster is left untouched (configure/cleanup externally).`,
	RunE:         runMssqlAGRemove,
	SilenceUsage: true,
}

func init() {
	registerMssqlAGFlags(mssqlAGCmd)
	registerMssqlAGRemoveFlags(mssqlAGRemoveCmd)
	mssqlAGCmd.AddCommand(mssqlAGRemoveCmd)
}

func registerMssqlAGFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&mssqlSkipOS, "skip-os", false, "Skip Windows OS baseline on replica(s); primary never runs W-* in ag")
	registerMssqlOSFlags(cmd)
	cmd.Flags().StringVar(&mssqlSaPassword, "mssql-sa-password", "", "SA password for SQL install (default aaBB11@@ when installing; empty for HA/remove uses Windows auth)")
	cmd.Flags().StringVar(&mssqlDomainMode, "mssql-domain-mode", "workgroup", "auto|domain|workgroup (default workgroup; domain not supported yet; workgroup skips W-014 SPN)")
	cmd.Flags().StringVar(&mssqlPort, "mssql-port", commonmssql.PortAuto, "SQL TCP port (auto or 1-65535)")
	cmd.Flags().IntVar(&mssqlHAEndpointPort, "mssql-ha-endpoint-port", 5022, "HADR endpoint port")
	cmd.Flags().StringVar(&mssqlInstance, "mssql-instance", commonmssql.InstanceAuto, "SQL instance name (auto or name)")
	cmd.Flags().StringVar(&mssqlHAStage, "stage", commonmssql.DefaultHAStage(), "AG stage: all/a, software/s, ha/h")
	cmd.Flags().BoolVar(&mssqlHASkipInstall, "skip-install", false, "Skip replica SQL install (same as --stage ha/h)")
	cmd.Flags().StringVar(&mssqlPrimaryHost, "primary-host", "", "Primary SQL host (required for add; omit for rebuild mode where -t[0] is primary)")
	cmd.Flags().StringVar(&mssqlPrimarySSHUser, "primary-ssh-user", "", "Primary SSH user (default: --ssh-user)")
	cmd.Flags().StringVar(&mssqlPrimarySSHPass, "primary-ssh-password", "", "Primary SSH password (default: --ssh-password)")
	cmd.Flags().StringVar(&mssqlPrimarySSHKey, "primary-ssh-key", "", "Primary SSH key path (default: --ssh-key-path)")
	cmd.Flags().StringVar(&mssqlAGName, "mssql-ag-name", "AG1", "Availability group name")
	cmd.Flags().StringVar(&mssqlAGListener, "mssql-ag-listener", "", "AG listener DNS name (default: ag-name-lst)")
	cmd.Flags().IntVar(&mssqlAGListenerPort, "mssql-ag-listener-port", 1433, "AG listener port")
	cmd.Flags().StringVar(&mssqlAGListenerIP, "mssql-ag-listener-ip", "", "AG listener VIP; omit to skip listener creation (A-013)")
	cmd.Flags().StringVar(&mssqlAGDB, "mssql-ag-db", "", "Comma-separated databases to add to AG")
	cmd.Flags().StringVar(&mssqlAGSeedingMode, "mssql-ag-seeding-mode", "manual", "AG seeding: manual|automatic")
	cmd.Flags().StringVar(&mssqlAGSeedingUNC, "mssql-ag-seeding-unc", "", "UNC path for automatic AG seeding")
	registerMssqlPathFlags(cmd)
	registerMssqlHAExtensionFlags(cmd)
}

func registerMssqlAGRemoveFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&mssqlPrimaryHost, "primary-host", "", "Primary SQL host (required; not in -t)")
	cmd.Flags().StringVar(&mssqlPrimarySSHUser, "primary-ssh-user", "", "Primary SSH user (default: --ssh-user)")
	cmd.Flags().StringVar(&mssqlPrimarySSHPass, "primary-ssh-password", "", "Primary SSH password (default: --ssh-password)")
	cmd.Flags().StringVar(&mssqlPrimarySSHKey, "primary-ssh-key", "", "Primary SSH key path (default: --ssh-key-path)")
	cmd.Flags().StringVar(&mssqlAGName, "mssql-ag-name", "AG1", "Availability group name to remove")
	cmd.Flags().StringVar(&mssqlDomainMode, "mssql-domain-mode", "workgroup", "auto|domain|workgroup (default workgroup; domain not supported yet)")
	cmd.Flags().StringVar(&mssqlAGDB, "mssql-ag-db", "", "Comma-separated AG database names to drop on secondary")
	cmd.Flags().BoolVar(&mssqlMirrorDropSecondaryDB, "ag-drop-secondary-db", false, "Drop AG database(s) on secondary only after remove")
	cmd.Flags().BoolVar(&mssqlMirrorDropSecondaryDB, "mirror-drop-secondary-db", false, "Drop AG database(s) on secondary only after remove (deprecated: use --ag-drop-secondary-db)")
	_ = cmd.Flags().MarkDeprecated("mirror-drop-secondary-db", "use --ag-drop-secondary-db")
	cmd.Flags().StringVar(&mssqlSaPassword, "mssql-sa-password", "", "SA password for SQL install (default aaBB11@@ when installing; empty for HA/remove uses Windows auth)")
	cmd.Flags().StringVar(&mssqlPort, "mssql-port", commonmssql.PortAuto, "SQL TCP port (auto or 1-65535)")
	cmd.Flags().StringVar(&mssqlInstance, "mssql-instance", commonmssql.InstanceAuto, "SQL instance name (auto or name)")
	registerMssqlHAInstanceFlags(cmd)
}

// buildMssqlAGParams assembles the params map for AG add/rebuild.
func buildMssqlAGParams(flags GlobalFlags) map[string]interface{} {
	p := buildMssqlBaseParams(flags)
	mssqlTopology = string(commonmssql.TopologyAGWSFC)
	p["mssql_topology"] = string(commonmssql.TopologyAGWSFC)
	p["mssql_ha_mode"] = commonmssql.HAModeAG
	p["mssql_ag_name"] = mssqlAGName
	p["mssql_ag_listener"] = mssqlAGListener
	p["mssql_ag_listener_port"] = mssqlAGListenerPort
	p["mssql_ag_listener_ip"] = mssqlAGListenerIP
	p["mssql_ag_db"] = mssqlAGDB
	p["mssql_ag_seeding_mode"] = mssqlAGSeedingMode
	p["mssql_ag_seeding_unc"] = mssqlAGSeedingUNC
	applyMssqlPathParams(p)
	applyMssqlRestorePathParams(p)
	buildMssqlSSHParams(p, flags)
	applyMssqlHAInstanceParams(p)
	commonmssql.MergeOSFirewallSQLPorts(p)
	return p
}

func runMssqlAG(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintMssqlAGStepCatalog(mssqlSkipOS)
		return nil
	}
	if err := applyMssqlLocalDefaults(&flags); err != nil {
		return err
	}
	if err := validateMssqlAGParams(&flags); err != nil {
		return err
	}
	if err := validateMssqlPortFlag(); err != nil {
		return err
	}
	applyMssqlRemoteSoftwareDefaults(cmd, &flags)

	haStage, err := commonmssql.ParseHAStage(mssqlHAStage)
	if err != nil {
		return err
	}
	if mssqlHASkipInstall {
		haStage = commonmssql.HAStageHA
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("mssql-ag-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logger.Close()

	params := buildMssqlAGParams(flags)
	setMssqlSAPasswordParam(params, commonmssql.HAIncludesSoftwareInstall(haStage))
	params["mssql_ha_stage"] = haStage

	primaryHost, replicaHosts, rebuildMode := resolveAGHosts(flags)
	if rebuildMode {
		mssqlPrimaryHost = primaryHost // set global so primarySSHConfig() works
		flags.Targets = replicaHosts   // primary is handled via primarySSHConfig separately
	}
	params["mssql_primary_host"] = primaryHost
	params["mssql_replica_hosts"] = append([]string(nil), replicaHosts...)
	params["mssql_rebuild_mode"] = rebuildMode

	logger.Info("MSSQL AG: stage=%s primary=%s replicas=%v rebuild=%v",
		haStage, primaryHost, replicaHosts, rebuildMode)

	listHosts := append([]string{primaryHost}, replicaHosts...)
	if done, err := runMssqlListInstancesIfRequested(flags, logger, listHosts, params); err != nil {
		return err
	} else if done {
		return nil
	}

	return RunMssqlAGOnHosts(flags, logger, params, haStage, rebuildMode)
}

func runMssqlAGRemove(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintMssqlAGRemoveStepCatalog()
		return nil
	}
	if err := applyMssqlLocalDefaults(&flags); err != nil {
		return err
	}
	if err := validateMssqlHAParams(&flags); err != nil {
		return err
	}
	if err := validateMssqlPortFlag(); err != nil {
		return err
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("mssql-ag-remove-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logger.Close()

	params := buildMssqlAGParams(flags)
	injectMssqlAGRemoveParams(params, flags.Targets)
	setMssqlSAPasswordParam(params, false)

	logger.Info("MSSQL AG remove: primary=%s replicas=%v ag=%s drop_secondary_db=%v",
		mssqlPrimaryHost, flags.Targets, mssqlAGName, mssqlMirrorDropSecondaryDB)

	allSteps := buildMssqlAGRemoveSteps()
	return RunMssqlHARemoveStepsOnHosts(flags, logger, params, allSteps, "MSSQL Always On remove (A)")
}

func validateMssqlAGParams(flags *GlobalFlags) error {
	primary := strings.TrimSpace(mssqlPrimaryHost)
	if primary == "" {
		if len(flags.Targets) < 2 {
			return fmt.Errorf("ag rebuild mode requires -t PRIMARY,REPLICA[,..] (at least 2 hosts) when --primary-host is omitted")
		}
		for _, t := range flags.Targets {
			if strings.TrimSpace(t) == "" {
				return fmt.Errorf("-t contains an empty target")
			}
		}
		return nil
	}
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
		return fmt.Errorf("at least one replica target is required in -t (or use rebuild mode: -t PRIMARY,REPLICA)")
	}
	flags.Targets = replicas
	if strings.EqualFold(strings.TrimSpace(mssqlAGSeedingMode), "automatic") {
		if strings.TrimSpace(mssqlAGSeedingUNC) == "" {
			return fmt.Errorf("--mssql-ag-seeding-unc is required when --mssql-ag-seeding-mode=automatic")
		}
	}
	return nil
}

func resolveAGHosts(flags GlobalFlags) (string, []string, bool) {
	if strings.TrimSpace(mssqlPrimaryHost) != "" {
		return strings.TrimSpace(mssqlPrimaryHost), append([]string(nil), flags.Targets...), false
	}
	primary := strings.TrimSpace(flags.Targets[0])
	replicas := make([]string, 0, len(flags.Targets)-1)
	for _, t := range flags.Targets[1:] {
		if t = strings.TrimSpace(t); t != "" {
			replicas = append(replicas, t)
		}
	}
	return primary, replicas, true
}

func injectMssqlAGRemoveParams(p map[string]interface{}, replicaHosts []string) {
	p["mssql_primary_host"] = strings.TrimSpace(mssqlPrimaryHost)
	p["mssql_replica_hosts"] = append([]string(nil), replicaHosts...)
	p["mssql_ha_mode"] = commonmssql.HAModeAG
	p["mssql_ag_name"] = mssqlAGName
	p["mssql_ag_db"] = mssqlAGDB
	p["mirror_drop_secondary_db"] = mssqlMirrorDropSecondaryDB
}

func buildMssqlAGAllSteps(skipOS bool, profile commonwin.Profile) []*runner.Step {
	var all []*runner.Step
	all = append(all, ossteps.StepB001CheckConnectivity())
	if !skipOS {
		all = append(all, winsteps.GetPreInstanceSteps(profile)...)
	}
	all = append(all, agsteps.GetAGAddSteps()...)
	if !skipOS {
		all = append(all, winsteps.GetPostInstanceSteps(profile)...)
	}
	return all
}

type mssqlAGStepGroups struct {
	b001   *runner.Step
	preOS  []*runner.Step
	ag     []*runner.Step
	postOS []*runner.Step
}

func splitMssqlAGSteps(steps []*runner.Step) mssqlAGStepGroups {
	var g mssqlAGStepGroups
	for _, s := range steps {
		if s == nil {
			continue
		}
		switch {
		case s.ID == "B-001":
			g.b001 = s
		case strings.HasPrefix(s.ID, "W-"):
			if s.ID == "W-012" || s.ID == "W-014" {
				g.postOS = append(g.postOS, s)
			} else {
				g.preOS = append(g.preOS, s)
			}
		case strings.HasPrefix(s.ID, "A-"):
			g.ag = append(g.ag, s)
		}
	}
	return g
}

// ensureAGExistingReplicasConnected detects replicas already in the AG (via
// sys.availability_replicas on primary) that are not in hostInfos (typically
// existing secondaries when -t only lists the new node). It resolves their IPs
// from the primary's hosts file, connects, and adds them to hostInfos so they
// participate in the round-robin (hosts update, cert exchange, etc).
// Returns updated hostInfos and hosts newly added (for follow-up A-001).
func ensureAGExistingReplicasConnected(
	hostInfos []*HostInfo,
	params map[string]interface{},
	flags GlobalFlags,
	logger *logging.Logger,
	progress *runner.StepProgress,
	sharedResults map[string]interface{},
) ([]*HostInfo, []*HostInfo) {
	if flags.DryRun || flags.Precheck {
		return hostInfos, nil
	}
	primaryInfo, _ := partitionHAHostInfos(hostInfos, strings.TrimSpace(params["mssql_primary_host"].(string)))
	if primaryInfo == nil || primaryInfo.Executor == nil {
		return hostInfos, nil
	}

	agName, _ := params["mssql_ag_name"].(string)
	if strings.TrimSpace(agName) == "" {
		agName = "AG1"
	}

	// Query existing AG replicas on primary.
	agCtx := &runner.StepContext{
		Executor:       &runnerExecAdapter{e: primaryInfo.Executor},
		Logger:         logger,
		Params:         params,
		TargetPlatform: "windows",
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(agCtx, "ensure-existing-replicas", commonmssql.AGReplicaServerNamesSQL(agName))
	if err != nil {
		logger.Info("ensureAGExistingReplicas: AG %s query failed (may not exist yet): %v", agName, err)
		return hostInfos, nil
	}
	agNames := commonmssql.ParseAGReplicaServerNames(stdout)
	if len(agNames) == 0 {
		return hostInfos, nil
	}

	var added []*HostInfo
	// Check which replicas are already in hostInfos.
	existingIPs := make(map[string]bool)
	for _, hi := range hostInfos {
		if hi != nil {
			existingIPs[strings.TrimSpace(hi.Host)] = true
		}
	}

	// For each AG replica NOT already connected, resolve IP from primary's hosts file.
	for _, name := range agNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		// Resolve IP via Get-Content hosts on primary.
		ip := resolveReplicaIP(agCtx, name, logger)
		if ip == "" {
			logger.Warn("ensureAGExistingReplicas: cannot resolve IP for AG replica %s, skip", name)
			continue
		}
		if existingIPs[ip] {
			continue
		}

		// Connect and add to hostInfos.
		connID := "existing_ag_replica_" + ip
		executor, err := createWindowsExecutor(ip, flags, logger, connID)
		if err != nil {
			logger.Warn("ensureAGExistingReplicas: cannot connect to %s (%s): %v, skip", name, ip, err)
			continue
		}
		logger.Info("ensureAGExistingReplicas: added existing replica %s (%s) to round-robin", name, ip)
		commonmssql.StoreHAReplicaServerName(sharedResults, ip, name)
		hi := &HostInfo{Host: ip, Executor: executor, TargetPlatform: "windows"}
		hostInfos = append(hostInfos, hi)
		added = append(added, hi)
		existingIPs[ip] = true
	}
	commonmssql.MergeAGTopologyHosts(params, haHostIPs(hostInfos)...)
	return hostInfos, added
}

func haHostIPs(hostInfos []*HostInfo) []string {
	var out []string
	for _, hi := range hostInfos {
		if hi != nil && strings.TrimSpace(hi.Host) != "" {
			out = append(out, strings.TrimSpace(hi.Host))
		}
	}
	return out
}

// resolveReplicaIP looks up a Windows computer name in the primary's hosts
// file to find its IP address.
func resolveReplicaIP(ctx *runner.StepContext, name string, logger *logging.Logger) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	ps := fmt.Sprintf(`$h='C:\Windows\System32\drivers\etc\hosts'; if (Test-Path $h) { $line=(Select-String -Path $h -Pattern ('\b'+[regex]::Escape('%s')+'\b') -SimpleMatch:$false | Select-Object -First 1); if ($line) { ($line.Line -split '\s+')[0] } }`, strings.ReplaceAll(name, "'", "''"))
	out, err := commonmssql.RunHAPowerShellScalar(ctx, "resolve-replica-ip "+name, ps)
	if err != nil {
		logger.Warn("resolveReplicaIP: PS failed for %s: %v", name, err)
		return ""
	}
	ip := strings.TrimSpace(out)
	// Validate it looks like an IP.
	if strings.Count(ip, ".") == 3 {
		return ip
	}
	return ""
}

// RunMssqlAGOnHosts runs B-001 → A-002 → pre W-* → A-003/004 → A-005..A-015 → post W-*.
func RunMssqlAGOnHosts(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}, haStage string, rebuildMode bool) error {
	profile := commonmssql.WinOSProfileForMssql(commonmssql.TopologyAGWSFC, params)
	allSteps := buildMssqlAGAllSteps(mssqlSkipOS, profile)
	steps := ensureConnectivityStep(allSteps, filterSteps(allSteps, flags))
	if len(steps) == 0 && !commonmssql.HAIncludesSoftwareInstall(haStage) {
		logger.Info("No steps to execute after filtering")
		return nil
	}
	groups := splitMssqlAGSteps(steps)
	logger.Info("Steps to execute: %d (ha stage=%s, rebuild=%v)", len(steps), haStage, rebuildMode)
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	planned := runner.CountNonOptionalSteps(steps)
	if commonmssql.HAIncludesSoftwareInstall(haStage) {
		planned += 3
	}
	progress := runner.NewStepProgress(planned)
	totalSteps := progress.Total()
	sharedResults := make(map[string]interface{})

	hostInfos, stepIdx, err := runMssqlHAConnectivityPhase(groups.b001, flags, params, logger, 0, totalSteps, progress, sharedResults)
	if err != nil {
		return err
	}
	defer closeHostInfos(hostInfos)

	primaryHost := strings.TrimSpace(params["mssql_primary_host"].(string))
	primaryInfo, replicaInfos := partitionHAHostInfos(hostInfos, primaryHost)
	if primaryInfo == nil {
		return fmt.Errorf("primary host %s not in connectivity results", primaryHost)
	}

	commonmssql.MergeAGTopologyHosts(params, haHostIPs(hostInfos)...)

	logger.Info("======== Phase: Resolve instance (A-001) ========")
	for _, hi := range hostInfos {
		if err := runMssqlHASingleStep(agsteps.StepA001ResolveInstance(), hi, params, flags, logger, sharedResults, progress); err != nil {
			return fmt.Errorf("A-001 on %s: %w", hi.Host, err)
		}
	}

	logger.Info("======== Phase: Primary SQL check (A-002) ========")
	if err := runMssqlHASingleStep(agsteps.StepA002CheckPrimary(), primaryInfo, params, flags, logger, sharedResults, progress); err != nil {
		return err
	}

	// When adding a new replica to an existing AG, include existing replicas
	// in the round-robin so they get hosts file updates and cert exchanges with
	// the new node. Without this, existing secondaries miss out on required updates.
	var addedExisting []*HostInfo
	hostInfos, addedExisting = ensureAGExistingReplicasConnected(hostInfos, params, flags, logger, progress, sharedResults)
	if len(addedExisting) > 0 {
		logger.Info("======== Phase: Resolve instance (A-001) for existing AG replicas ========")
		for _, hi := range addedExisting {
			if err := runMssqlHASingleStep(agsteps.StepA001ResolveInstance(), hi, params, flags, logger, sharedResults, progress); err != nil {
				return fmt.Errorf("A-001 on %s (existing AG replica): %w", hi.Host, err)
			}
		}
	}

	if len(groups.preOS) > 0 && !mssqlSkipOS {
		if len(replicaInfos) == 0 {
			return fmt.Errorf("no replica hosts in -t for OS baseline")
		}
		logger.Info("======== Phase: Windows OS pre-instance (replica only) ========")
		res := RunPerHostStepsEx(groups.preOS, replicaInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx += len(groups.preOS)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}

	if commonmssql.HAIncludesSoftwareInstall(haStage) {
		if len(replicaInfos) == 0 {
			return fmt.Errorf("no replica hosts in -t for install stage %q", haStage)
		}
		logger.Info("======== Phase: Replica SQL install (A-003/004) ========")
		installSteps := []*runner.Step{agsteps.StepA003PlanReplicaInstall(), agsteps.StepA004InstallReplica()}
		for _, rh := range replicaInfos {
			logger.Info("-------- Replica host: %s --------", rh.Host)
			for _, step := range installSteps {
				if err := runMssqlHASingleStep(step, rh, params, flags, logger, sharedResults, progress); err != nil {
					return fmt.Errorf("step %s on %s: %w", step.ID, rh.Host, err)
				}
			}
		}
	}

	if !commonmssql.HAIncludesHASetup(haStage) {
		logger.Info("MSSQL AG stage %q completed (replica install only)", haStage)
		return nil
	}

	if len(groups.ag) > 0 {
		logger.Info("======== Phase: MSSQL AG (A) ========")
		res := RunRoundRobinPerHostStepsEx(groups.ag, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx += len(groups.ag)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}

	if len(groups.postOS) > 0 && !mssqlSkipOS {
		if len(replicaInfos) == 0 {
			return fmt.Errorf("no replica hosts in -t for OS post-instance verify")
		}
		logger.Info("======== Phase: Windows OS post-instance (replica only) ========")
		res := RunPerHostStepsEx(groups.postOS, replicaInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}
	return nil
}

func buildMssqlAGRemoveSteps() []*runner.Step {
	out := make([]*runner.Step, 0, 5)
	out = append(out, ossteps.StepB001CheckConnectivity())
	out = append(out, agsteps.GetAGRemoveSteps()...)
	return out
}

// PrintMssqlAGStepCatalog lists mssql ag steps.
func PrintMssqlAGStepCatalog(skipOS bool) {
	profile := commonmssql.WinOSProfileForMssql(commonmssql.TopologyAGWSFC, nil)
	steps := buildMssqlAGAllSteps(skipOS, profile)
	printStepSection("Primary SQL check", []*runner.Step{agsteps.StepA002CheckPrimary()})
	printStepSection("Connectivity", []*runner.Step{steps[0]})
	if !skipOS {
		var pre []*runner.Step
		for _, s := range steps {
			if strings.HasPrefix(s.ID, "W-") && s.ID != "W-012" && s.ID != "W-014" {
				pre = append(pre, s)
			}
		}
		printStepSection("Windows OS pre-instance", pre)
	}
	printStepSection("Replica SQL install (optional)", []*runner.Step{
		agsteps.StepA003PlanReplicaInstall(),
		agsteps.StepA004InstallReplica(),
	})
	var a []*runner.Step
	for _, s := range steps {
		if strings.HasPrefix(s.ID, "A-") {
			a = append(a, s)
		}
	}
	printStepSection("MSSQL AG (A)", a)
	if !skipOS {
		post := winsteps.GetPostInstanceSteps(profile)
		printStepSection("Windows OS post-instance", post)
	}
	fmt.Fprintln(os.Stdout, "Note: --stage all (default) runs install+AG; software/s install only; ha/h or --skip-install skips install")
	fmt.Fprintln(os.Stdout, "Note: AG requires pre-existing WSFC cluster (configure externally)")
	fmt.Fprintln(os.Stdout, "")
}

// PrintMssqlAGRemoveStepCatalog lists mssql ag remove steps.
func PrintMssqlAGRemoveStepCatalog() {
	steps := buildMssqlAGRemoveSteps()
	printStepSection("Connectivity", []*runner.Step{steps[0]})
	printStepSection("MSSQL Always On remove (A)", agsteps.GetAGRemoveSteps())
	fmt.Fprintln(os.Stdout, "")
}
