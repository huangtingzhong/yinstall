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
	mirrsteps "github.com/yinstall/internal/steps/mssql_mirror"
	ossteps "github.com/yinstall/internal/steps/os"
	winsteps "github.com/yinstall/internal/steps/win_os"
)

var mssqlMirrorCmd = &cobra.Command{
	Use:   "mirror",
	Short: "Configure MSSQL database mirroring",
	Long: `Configure MSSQL database mirroring between a primary (--primary-host) and
replicas (-t). Three modes:

  Add node (default):
    yinstall mssql mirror --primary-host PRIMARY -t REPLICA [--stage ha]

  Remove node:
    yinstall mssql mirror remove --primary-host PRIMARY -t REPLICA

  Rebuild (no --primary-host; -t[0] is primary, -t[1:] are replicas):
    yinstall mssql mirror -t PRIMARY,REPLICA [--stage ha]
    Rebuild is idempotent: skips when current topology already matches -t.

Stages (--stage): all/a (install replica SQL + mirror), software/s (replica
install only), ha/h (mirror only; replica SQL must already exist).
--skip-install is equivalent to --stage ha/h.`,
	RunE:         runMssqlMirror,
	SilenceUsage: true,
}

var mssqlMirrorRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove database mirroring",
	Long: `Remove database mirroring partnership (SET PARTNER OFF) on primary.

By default the secondary keeps mirror database copies (--mirror-recover-secondary=true).
Use --mirror-drop-secondary-db to DROP those databases on the secondary only.`,
	RunE:         runMssqlMirrorRemove,
	SilenceUsage: true,
}

func init() {
	registerMssqlMirrorFlags(mssqlMirrorCmd)
	registerMssqlMirrorRemoveFlags(mssqlMirrorRemoveCmd)
	mssqlMirrorCmd.AddCommand(mssqlMirrorRemoveCmd)
}

func registerMssqlMirrorFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&mssqlSkipOS, "skip-os", false, "Skip Windows OS baseline on replica(s); primary never runs W-* in mirror")
	registerMssqlOSFlags(cmd)
	cmd.Flags().StringVar(&mssqlSaPassword, "mssql-sa-password", "", "SA password for SQL install (default aaBB11@@ when installing; empty for HA/remove uses Windows auth)")
	cmd.Flags().StringVar(&mssqlDomainMode, "mssql-domain-mode", "workgroup", "auto|domain|workgroup (default workgroup; cert-based mirroring)")
	cmd.Flags().StringVar(&mssqlPort, "mssql-port", commonmssql.PortAuto, "SQL TCP port (auto or 1-65535)")
	cmd.Flags().IntVar(&mssqlHAEndpointPort, "mssql-ha-endpoint-port", 5022, "Mirroring endpoint port")
	cmd.Flags().StringVar(&mssqlInstance, "mssql-instance", commonmssql.InstanceAuto, "SQL instance name (auto or name)")
	cmd.Flags().StringVar(&mssqlHAStage, "stage", commonmssql.DefaultHAStage(), "Mirror stage: all/a, software/s, ha/h")
	cmd.Flags().BoolVar(&mssqlHASkipInstall, "skip-install", false, "Skip replica SQL install (same as --stage ha/h)")
	cmd.Flags().StringVar(&mssqlPrimaryHost, "primary-host", "", "Primary SQL host (required for add; omit for rebuild mode where -t[0] is primary)")
	cmd.Flags().StringVar(&mssqlPrimarySSHUser, "primary-ssh-user", "", "Primary SSH user (default: --ssh-user)")
	cmd.Flags().StringVar(&mssqlPrimarySSHPass, "primary-ssh-password", "", "Primary SSH password (default: --ssh-password)")
	cmd.Flags().StringVar(&mssqlPrimarySSHKey, "primary-ssh-key", "", "Primary SSH key path (default: --ssh-key-path)")
	cmd.Flags().StringVar(&mssqlMirrorDB, "mirror-db", "", "Comma-separated database names to mirror (default: all user databases on primary)")
	cmd.Flags().IntVar(&mssqlMirrorCertValidDays, "mirror-cert-valid-days", commonmssql.DefaultMirrorCertValidDays, "Mirror certificate validity in days")
	cmd.Flags().BoolVar(&mssqlMirrorDropExisting, "mirror-drop-existing", false, "Drop existing database on secondary only before restore (requires -f)")
	cmd.Flags().BoolVar(&mssqlMirrorSkipSeed, "mirror-skip-seed", false, "Skip M-012 backup-restore seed; assume secondary already NORECOVERY")
	cmd.Flags().StringVar(&mssqlMirrorWorkDir, "mirror-work-dir", "", "Mirror work directory (certs/backups; default: SQL Backup\\yinstall_mirror)")
	cmd.Flags().StringVar(&mssqlMirrorRestoreFrom, "mirror-restore-from", "", "Secondary restore source path/UNC (skip fetch from primary)")
	cmd.Flags().StringVar(&mssqlMirrorBackupDir, "mirror-backup-dir", "", "Backup directory; file name is {mirror-db}_{YYYYmmdd_HHMMSS}.bak")
	registerMssqlPathFlags(cmd)
	registerMssqlHAExtensionFlags(cmd)
}

func registerMssqlMirrorRemoveFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&mssqlPrimaryHost, "primary-host", "", "Primary SQL host (required; not in -t)")
	cmd.Flags().StringVar(&mssqlPrimarySSHUser, "primary-ssh-user", "", "Primary SSH user (default: --ssh-user)")
	cmd.Flags().StringVar(&mssqlPrimarySSHPass, "primary-ssh-password", "", "Primary SSH password (default: --ssh-password)")
	cmd.Flags().StringVar(&mssqlPrimarySSHKey, "primary-ssh-key", "", "Primary SSH key path (default: --ssh-key-path)")
	cmd.Flags().StringVar(&mssqlMirrorDB, "mirror-db", "", "Comma-separated database names to remove (default: all mirrored databases on primary)")
	cmd.Flags().BoolVar(&mssqlMirrorRecover, "mirror-recover-secondary", true, "After partner off, RESTORE WITH RECOVERY on secondary")
	cmd.Flags().BoolVar(&mssqlMirrorDropSecondaryDB, "mirror-drop-secondary-db", false, "Drop mirror database(s) on secondary only after partner off")
	cmd.Flags().StringVar(&mssqlSaPassword, "mssql-sa-password", "", "SA password for SQL install (default aaBB11@@ when installing; empty for HA/remove uses Windows auth)")
	cmd.Flags().StringVar(&mssqlPort, "mssql-port", commonmssql.PortAuto, "SQL TCP port (auto or 1-65535)")
	cmd.Flags().StringVar(&mssqlInstance, "mssql-instance", commonmssql.InstanceAuto, "SQL instance name (auto or name)")
	registerMssqlHAInstanceFlags(cmd)
}
func buildMssqlMirrorParams(flags GlobalFlags) map[string]interface{} {
	p := buildMssqlBaseParams(flags)
	mssqlTopology = string(commonmssql.TopologyMirror)
	p["mssql_topology"] = string(commonmssql.TopologyMirror)
	p["mssql_ha_mode"] = commonmssql.HAModeMirror
	p["mssql_mirror_db"] = mssqlMirrorDB
	p["mirror_recover_secondary"] = mssqlMirrorRecover
	if mssqlMirrorCertValidDays <= 0 {
		p["mirror_cert_valid_days"] = commonmssql.DefaultMirrorCertValidDays
	} else {
		p["mirror_cert_valid_days"] = mssqlMirrorCertValidDays
	}
	p["mirror_drop_existing"] = mssqlMirrorDropExisting
	p["mirror_skip_seed"] = mssqlMirrorSkipSeed
	if wd := strings.TrimSpace(mssqlMirrorWorkDir); wd != "" {
		p["mirror_work_dir"] = wd
	}
	if rf := strings.TrimSpace(mssqlMirrorRestoreFrom); rf != "" {
		p["mirror_restore_from"] = rf
	}
	if bd := strings.TrimSpace(mssqlMirrorBackupDir); bd != "" {
		p["mirror_backup_dir"] = bd
	}
	applyMssqlPathParams(p)
	applyMssqlRestorePathParams(p)
	buildMssqlSSHParams(p, flags)
	applyMssqlHAInstanceParams(p)
	commonmssql.MergeOSFirewallSQLPorts(p)
	return p
}

func runMssqlMirror(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintMssqlMirrorStepCatalog(mssqlSkipOS)
		return nil
	}
	if err := applyMssqlLocalDefaults(&flags); err != nil {
		return err
	}
	if err := validateMssqlMirrorParams(&flags); err != nil {
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
		rid = fmt.Sprintf("mssql-mirror-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logger.Close()

	params := buildMssqlMirrorParams(flags)
	setMssqlSAPasswordParam(params, commonmssql.HAIncludesSoftwareInstall(haStage))
	params["mssql_ha_stage"] = haStage

	primaryHost, replicaHosts, rebuildMode := resolveMirrorHosts(flags)
	if rebuildMode {
		mssqlPrimaryHost = primaryHost // set global so primarySSHConfig() works
		flags.Targets = replicaHosts   // primary is handled via primarySSHConfig separately
	}
	params["mssql_primary_host"] = primaryHost
	params["mssql_replica_hosts"] = append([]string(nil), replicaHosts...)
	params["mssql_rebuild_mode"] = rebuildMode

	logger.Info("MSSQL mirror: mode=%s stage=%s primary=%s replicas=%v rebuild=%v",
		commonmssql.HAModeMirror, haStage, primaryHost, replicaHosts, rebuildMode)

	listHosts := append([]string{primaryHost}, replicaHosts...)
	if done, err := runMssqlListInstancesIfRequested(flags, logger, listHosts, params); err != nil {
		return err
	} else if done {
		return nil
	}

	return RunMssqlMirrorOnHosts(flags, logger, params, haStage, rebuildMode)
}

func runMssqlMirrorRemove(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintMssqlMirrorRemoveStepCatalog()
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
		rid = fmt.Sprintf("mssql-mirror-remove-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logger.Close()

	params := buildMssqlMirrorParams(flags)
	params["mssql_primary_host"] = strings.TrimSpace(mssqlPrimaryHost)
	params["mssql_replica_hosts"] = append([]string(nil), flags.Targets...)
	params["mssql_ha_mode"] = commonmssql.HAModeMirror
	params["mirror_recover_secondary"] = mssqlMirrorRecover
	params["mirror_drop_secondary_db"] = mssqlMirrorDropSecondaryDB
	setMssqlSAPasswordParam(params, false)

	logger.Info("MSSQL mirror remove: primary=%s replicas=%v db=%s drop_secondary_db=%v",
		mssqlPrimaryHost, flags.Targets, mssqlMirrorDB, mssqlMirrorDropSecondaryDB)

	allSteps := buildMssqlMirrorRemoveSteps()
	return RunMssqlHARemoveStepsOnHosts(flags, logger, params, allSteps, "MSSQL mirror remove (M)")
}

func validateMssqlMirrorParams(flags *GlobalFlags) error {
	primary := strings.TrimSpace(mssqlPrimaryHost)
	if primary == "" {
		// Rebuild mode: -t[0] is primary, -t[1:] are replicas.
		if len(flags.Targets) < 2 {
			return fmt.Errorf("mirror rebuild mode requires -t PRIMARY,REPLICA[,..] (at least 2 hosts) when --primary-host is omitted")
		}
		for _, t := range flags.Targets {
			if strings.TrimSpace(t) == "" {
				return fmt.Errorf("-t contains an empty target")
			}
		}
		return nil
	}
	// Add mode: --primary-host + -t replicas; primary must not appear in -t.
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
	return nil
}

// resolveMirrorHosts returns (primaryHost, replicaHosts, rebuildMode) based on
// whether --primary-host was provided.
func resolveMirrorHosts(flags GlobalFlags) (string, []string, bool) {
	if strings.TrimSpace(mssqlPrimaryHost) != "" {
		return strings.TrimSpace(mssqlPrimaryHost), append([]string(nil), flags.Targets...), false
	}
	// Rebuild mode: targets[0] = primary, targets[1:] = replicas.
	primary := strings.TrimSpace(flags.Targets[0])
	replicas := make([]string, 0, len(flags.Targets)-1)
	for _, t := range flags.Targets[1:] {
		if t = strings.TrimSpace(t); t != "" {
			replicas = append(replicas, t)
		}
	}
	return primary, replicas, true
}

func buildMssqlMirrorAllSteps(skipOS bool, profile commonwin.Profile) []*runner.Step {
	var all []*runner.Step
	all = append(all, ossteps.StepB001CheckConnectivity())
	if !skipOS {
		all = append(all, winsteps.GetPreInstanceSteps(profile)...)
	}
	all = append(all, mirrsteps.GetMirrorAddSteps()...)
	if !skipOS {
		all = append(all, winsteps.GetPostInstanceSteps(profile)...)
	}
	return all
}

type mssqlMirrorStepGroups struct {
	b001   *runner.Step
	preOS  []*runner.Step
	mirror []*runner.Step
	postOS []*runner.Step
}

func splitMssqlMirrorSteps(steps []*runner.Step) mssqlMirrorStepGroups {
	var g mssqlMirrorStepGroups
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
		case strings.HasPrefix(s.ID, "M-"):
			g.mirror = append(g.mirror, s)
		}
	}
	return g
}

// RunMssqlMirrorOnHosts runs B-001 → M-002 → pre W-* → M-003/004 → M-005..M-014 → post W-*.
func RunMssqlMirrorOnHosts(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}, haStage string, rebuildMode bool) error {
	profile := commonmssql.WinOSProfileForMssql(commonmssql.TopologyMirror, params)
	allSteps := buildMssqlMirrorAllSteps(mssqlSkipOS, profile)
	steps := ensureConnectivityStep(allSteps, filterSteps(allSteps, flags))
	if len(steps) == 0 && !commonmssql.HAIncludesSoftwareInstall(haStage) {
		logger.Info("No steps to execute after filtering")
		return nil
	}
	groups := splitMssqlMirrorSteps(steps)
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

	logger.Info("======== Phase: Resolve instance (M-001) ========")
	for _, hi := range hostInfos {
		if err := runMssqlHASingleStep(mirrsteps.StepM001ResolveInstance(), hi, params, flags, logger, sharedResults, progress); err != nil {
			return fmt.Errorf("M-001 on %s: %w", hi.Host, err)
		}
	}

	logger.Info("======== Phase: Primary SQL check (M-002) ========")
	if err := runMssqlHASingleStep(mirrsteps.StepM002CheckPrimary(), primaryInfo, params, flags, logger, sharedResults, progress); err != nil {
		return err
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
		logger.Info("======== Phase: Replica SQL install (M-003/004) ========")
		installSteps := []*runner.Step{mirrsteps.StepM003PlanReplicaInstall(), mirrsteps.StepM004InstallReplica()}
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
		logger.Info("MSSQL mirror stage %q completed (replica install only)", haStage)
		return nil
	}

	if len(groups.mirror) > 0 {
		logger.Info("======== Phase: MSSQL mirror (M) ========")
		res := RunRoundRobinPerHostStepsEx(groups.mirror, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx += len(groups.mirror)
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

func buildMssqlMirrorRemoveSteps() []*runner.Step {
	out := make([]*runner.Step, 0, 5)
	out = append(out, ossteps.StepB001CheckConnectivity())
	out = append(out, mirrsteps.GetMirrorRemoveSteps()...)
	return out
}

// PrintMssqlMirrorStepCatalog lists mssql mirror steps.
func PrintMssqlMirrorStepCatalog(skipOS bool) {
	profile := commonmssql.WinOSProfileForMssql(commonmssql.TopologyMirror, nil)
	steps := buildMssqlMirrorAllSteps(skipOS, profile)
	printStepSection("Primary SQL check", []*runner.Step{mirrsteps.StepM002CheckPrimary()})
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
		mirrsteps.StepM003PlanReplicaInstall(),
		mirrsteps.StepM004InstallReplica(),
	})
	var m []*runner.Step
	for _, s := range steps {
		if strings.HasPrefix(s.ID, "M-") {
			m = append(m, s)
		}
	}
	printStepSection("MSSQL mirror (M)", m)
	if !skipOS {
		post := winsteps.GetPostInstanceSteps(profile)
		printStepSection("Windows OS post-instance", post)
	}
	fmt.Fprintln(os.Stdout, "Note: --stage all (default) runs install+mirror; software/s install only; ha/h or --skip-install skips install")
	fmt.Fprintln(os.Stdout, "")
}

// PrintMssqlMirrorRemoveStepCatalog lists mssql mirror remove steps.
func PrintMssqlMirrorRemoveStepCatalog() {
	steps := buildMssqlMirrorRemoveSteps()
	printStepSection("Connectivity", []*runner.Step{steps[0]})
	printStepSection("MSSQL mirror remove (M)", mirrsteps.GetMirrorRemoveSteps())
	fmt.Fprintln(os.Stdout, "")
}
