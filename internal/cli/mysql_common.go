package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	commonmysql "github.com/yinstall/internal/common/mysql"
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
	ossteps "github.com/yinstall/internal/steps/os"
	winsteps "github.com/yinstall/internal/steps/win_os"
)

var (
	mysqlSkipOS           bool
	mysqlPort             int
	mysqlBase             string
	mysqlPackage          string
	mysqlRootPassword     string
	mysqlInitInsecure     bool
	mysqlCnfTemplate      string
	mysqlServerID         int
	mysqlInnodbBufferPool string
	mysqlRemoteRoot       bool
	mysqlEnvFile          string
	mysqlCustomSQLScript  string
	mysqlSkipSystemd      bool
	mysqlVCRedistPackage  string
	mysqlGtidMode         string
	mysqlEnforceGtid      string
	mysqlStage            string
	mysqlVersion          string
	mysqlHome             string
	mysqlSELinuxMode      string
)

func registerMysqlInstallFlags(cmd *cobra.Command) {
	registerAllOSFlags(cmd, registerOSFlagsConfig{forMySQL: true})
	registerWinOSExtensionFlags(cmd, registerWinOSFlagsConfig{whenSkipOSFalse: " (only when --skip-os=false on Windows targets)"})

	cmd.Flags().IntVar(&mysqlPort, "mysql-port", 3306, "MySQL port")
	cmd.Flags().StringVar(&mysqlBase, "mysql-base", "/mysql/app/mysql", "MySQL base directory")
	cmd.Flags().StringVar(&mysqlPackage, "mysql-package", "", "MySQL installation package path (auto-discover if empty)")
	cmd.Flags().StringVar(&mysqlRootPassword, "mysql-root-password", "", "MySQL root password")
	cmd.Flags().BoolVar(&mysqlInitInsecure, "mysql-init-insecure", true, "Use mysqld --initialize-insecure")
	cmd.Flags().StringVar(&mysqlCnfTemplate, "mysql-cnf-template", "", "my.cnf template: mysql80 (default 8.0+) or mysql57")
	cmd.Flags().IntVar(&mysqlServerID, "mysql-server-id", 0, "server-id (0=auto)")
	cmd.Flags().StringVar(&mysqlInnodbBufferPool, "mysql-innodb-buffer-pool", "4G", "innodb_buffer_pool_size")
	cmd.Flags().BoolVar(&mysqlRemoteRoot, "mysql-remote-root", false, "Allow root remote login")
	cmd.Flags().StringVar(&mysqlEnvFile, "mysql-env-file", "", "Env file path under SSH login user (remote) or local executor (~/.3306 default)")
	cmd.Flags().StringVar(&mysqlCustomSQLScript, "mysql-custom-sql-script", "", "Custom SQL script after install")
	cmd.Flags().BoolVar(&mysqlSkipSystemd, "mysql-skip-systemd", false, "Skip systemd service setup on Linux")
	cmd.Flags().StringVar(&mysqlVCRedistPackage, "mysql-vc-redist-package", "", "VC_redist.x64.exe path (auto if empty)")
	cmd.Flags().StringVar(&mysqlGtidMode, "mysql-gtid-mode", "on", "gtid_mode in my.cnf")
	cmd.Flags().StringVar(&mysqlEnforceGtid, "mysql-enforce-gtid-consistency", "on", "enforce_gtid_consistency in my.cnf")
	cmd.Flags().StringVar(&mysqlStage, "stage", commonmysql.DefaultInstallStage(), "Install stage: all/a (software+instance), software/s (binary only), instance/i (new port instance)")
	cmd.Flags().StringVar(&mysqlVersion, "mysql-version", "", "MySQL software version for replica layout (auto-detect on standby when empty)")
	cmd.Flags().StringVar(&mysqlHome, "mysql-home", "", "MySQL installation home for mysql/mysqldump client (default: PATH lookup)")
	cmd.Flags().StringVar(&mysqlSELinuxMode, "mysql-selinux-mode", "auto", "SELinux on Linux: auto (label when Enforcing), label (force), skip")
}

func buildMysqlParams(targetCount int, flags GlobalFlags, stage string) map[string]interface{} {
	p := buildOSParams(false, targetCount)
	for k, v := range buildWinOSParams(mysqlSkipOS, commonwin.ProfileMySQL()) {
		p[k] = v
	}
	p["os_user"] = "mysql"
	p["os_group"] = "mysql"
	p["os_user_shell"] = "/sbin/nologin"
	p["os_user_system"] = true
	p["os_user_nologin"] = true
	p["os_sudoers_enable"] = false
	p["os_dba_group"] = ""
	p["os_user_password"] = ""
	p["os_user_uid"] = 701
	p["os_group_gid"] = 701
	p["os_sysctl_file"] = "/etc/sysctl.d/mysql.conf"
	p["os_sysctl_profile"] = "mysql"
	p["os_kernel_args"] = "elevator=deadline transparent_hugepage=never numa=off"
	p["os_firewall_mode"] = "open-ports"
	p["os_firewall_ports"] = fmt.Sprintf("%d,%d0", mysqlPort, mysqlPort)
	if strings.TrimSpace(osLocalMount) == "" {
		p["os_local_mount"] = mysqlBase
	} else {
		p["os_local_mount"] = osLocalMount
	}
	p["windows_transport"] = "auto"
	p["winrm_port"] = defaultWinRMPort
	p["os_local_mount_opts"] = "nobarrier,largeio,noatime,nodiratime"
	p["os_deps_db_packages"] = "make gcc-c++ cmake bison-devel ncurses-devel libaio libnuma"
	p["os_deps_tools_packages"] = "unzip bind-utils sysstat setuptool telnet iotop openssh-clients net-tools libvncserver tigervnc-server device-mapper-multipath dstat lsof ntp psmisc redhat-lsb-core parted xhost strace showmount expect tcl sysfsutils gdisk rsync screen"

	p["mysql_port"] = mysqlPort
	p["mysql_base"] = mysqlBase
	p["target_platform"] = inferTargetPlatformFromFlags(flags)
	p["mysql_package"] = mysqlPackage
	p["mysql_root_password"] = mysqlRootPassword
	p["mysql_init_insecure"] = mysqlInitInsecure
	p["mysql_cnf_template"] = mysqlCnfTemplate
	p["mysql_server_id"] = mysqlServerID
	p["mysql_innodb_buffer_pool_size"] = mysqlInnodbBufferPool
	p["mysql_remote_root"] = mysqlRemoteRoot
	p["mysql_skip_os"] = mysqlSkipOS
	p["os_hostname_default_prefix"] = "mysql"
	p["mysql_env_file"] = mysqlEnvFile
	p["mysql_custom_sql_script"] = mysqlCustomSQLScript
	p["mysql_skip_systemd"] = mysqlSkipSystemd
	p["mysql_vc_redist_package"] = mysqlVCRedistPackage
	p["mysql_gtid_mode"] = mysqlGtidMode
	p["mysql_enforce_gtid_consistency"] = mysqlEnforceGtid
	p["mysql_stage"] = stage
	p["mysql_version"] = mysqlVersion
	p["mysql_selinux_mode"] = mysqlSELinuxMode
	if strings.TrimSpace(mysqlHome) != "" {
		p["mysql_home"] = strings.TrimSpace(mysqlHome)
	}
	return p
}

func buildMysqlAllSteps(skipOS bool) []*runner.Step {
	return mysqlStepCatalog(skipOS)
}

// mysqlStepCatalog returns B-001 + (Linux B-* or W-* catalog) + M-* for filtering and -l.
func mysqlStepCatalog(skipOS bool) []*runner.Step {
	var all []*runner.Step
	all = append(all, ossteps.StepB001CheckConnectivity())
	if !skipOS {
		all = append(all, winsteps.GetPreInstanceSteps(commonwin.ProfileMySQL())...)
		all = append(all, filterOSStepsForMySQL(ossteps.GetAllSteps())...)
		all = append(all, winsteps.GetPostInstanceSteps(commonwin.ProfileMySQL())...)
	}
	all = append(all, mysqlsteps.GetAllSteps()...)
	return all
}

func filterOSStepsForMySQL(steps []*runner.Step) []*runner.Step {
	var out []*runner.Step
	for _, s := range steps {
		if isMySQLExcludedOSStep(s.ID) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func isMySQLExcludedOSStep(id string) bool {
	switch id {
	case "B-006", "B-011", "B-013", "B-014", "B-015", "B-016":
		return true
	case "B-023":
		return false
	}
	if len(id) == 5 && strings.HasPrefix(id, "B-") {
		n, err := strconv.Atoi(id[3:5])
		if err == nil && n >= 21 && n <= 31 {
			return true
		}
	}
	return false
}

func splitMysqlSteps(steps []*runner.Step) (b001, m001 *runner.Step, winOSSteps, winOSPostSteps, osSteps, mysqlSteps []*runner.Step) {
	for _, s := range steps {
		switch s.ID {
		case "B-001":
			b001 = s
		case "M-001":
			m001 = s
		default:
			switch {
			case strings.HasPrefix(s.ID, "W-"):
				if s.ID == "W-012" || s.ID == "W-014" {
					winOSPostSteps = append(winOSPostSteps, s)
				} else {
					winOSSteps = append(winOSSteps, s)
				}
			case strings.HasPrefix(s.ID, "B-"):
				osSteps = append(osSteps, s)
			case strings.HasPrefix(s.ID, "M-"):
				mysqlSteps = append(mysqlSteps, s)
			}
		}
	}
	return b001, m001, winOSSteps, winOSPostSteps, osSteps, mysqlSteps
}

func detectSharedPlatform(shared map[string]interface{}) string {
	if v, ok := shared["target_platform"].(string); ok && v != "" {
		return v
	}
	return mysqlsteps.PlatformLinux
}

func mysqlRefreshHostPlatforms(cmd *cobra.Command, hostInfos []*HostInfo, flags GlobalFlags, shared map[string]interface{}, logger *logging.Logger) ([]*HostInfo, GlobalFlags, error) {
	updatedFlags := flags
	for _, info := range hostInfos {
		platform := detectSharedPlatform(shared)
		if platform == "" {
			platform = targetPlatformForHost(info, shared, nil)
		}
		if platform == "" {
			platform = inferTargetPlatformFromFlags(flags)
		}
		info.TargetPlatform = platform
		if platform != mysqlsteps.PlatformWindows {
			continue
		}
		if !cmd.Flags().Changed("ssh-user") {
			updatedFlags.SSHUser = "Administrator"
		}
		info.Executor.Close()
		exec, err := createWindowsExecutor(info.Host, updatedFlags, logger, "M-001")
		if err != nil {
			return hostInfos, updatedFlags, fmt.Errorf("failed to reconnect Windows host %s: %w", info.Host, err)
		}
		info.Executor = exec
		logger.Info("Reconnected Windows host %s as %s", info.Host, updatedFlags.SSHUser)
	}
	return hostInfos, updatedFlags, nil
}

func applyMysqlPlatformDefaults(cmd *cobra.Command, flags *GlobalFlags, base *string) {
	platform := inferTargetPlatformFromFlags(*flags)
	if platform == "" {
		platform = commonmysql.PlatformLinux
	}
	if base != nil && platform == commonmysql.PlatformWindows && !cmd.Flags().Changed("mysql-base") {
		*base = commonmysql.DefaultBase(platform)
	}
	if !cmd.Flags().Changed("remote-software-dir") {
		dir := commonmysql.DefaultRemoteSoftwareDir(platform)
		remoteSoftwareDir = dir
		flags.RemoteSoftwareDir = dir
	}
}

func filterMysqlInstallStepsByStage(steps []*runner.Step, stage string) []*runner.Step {
	if stage == commonmysql.StageAll {
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
		if commonmysql.StepMatchesInstallStage(step, stage) {
			out = append(out, step)
		}
	}
	return out
}

func validateMysqlInstallStage(stage, rootPassword, pkg, version string, dryRun, precheck bool) error {
	normalized, err := commonmysql.ParseStage(stage)
	if err != nil {
		return err
	}
	if dryRun || precheck {
		return nil
	}
	switch normalized {
	case commonmysql.StageSoftware:
		if strings.TrimSpace(pkg) == "" {
			return fmt.Errorf("--mysql-package is required when --stage is software")
		}
	case commonmysql.StageInstance:
		if strings.TrimSpace(rootPassword) == "" {
			return fmt.Errorf("--mysql-root-password is required when --stage is instance")
		}
		if strings.TrimSpace(pkg) == "" && strings.TrimSpace(version) == "" {
			return fmt.Errorf("--mysql-version or --mysql-package is required when --stage is instance")
		}
	default:
		if strings.TrimSpace(rootPassword) == "" {
			return fmt.Errorf("--mysql-root-password is required")
		}
	}
	return nil
}

func validateMysqlCleanStage(cleanType, stage string, cmdChangedStage bool) error {
	if _, err := commonmysql.ParseStage(stage); err != nil {
		return err
	}
	if cleanType != "mysql" && cmdChangedStage {
		return fmt.Errorf("--stage is only valid with --type mysql")
	}
	return nil
}

// RunMysqlInstallOnHosts executes the MySQL install workflow on target hosts.
func RunMysqlInstallOnHosts(cmd *cobra.Command, flags GlobalFlags, logger *logging.Logger, stage string, params map[string]interface{}) error {
	allSteps := buildMysqlAllSteps(mysqlSkipOS)
	steps := ensureConnectivityStep(allSteps, filterSteps(allSteps, flags))
	steps = filterMysqlInstallStepsByStage(steps, stage)
	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}

	b001, m001, winOSSteps, winOSPostSteps, osSteps, mysqlRest := splitMysqlSteps(steps)
	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	plannedProgress := runner.CountNonOptionalSteps(steps)
	progress := runner.NewStepProgress(plannedProgress)
	totalSteps := progress.Total()
	sharedResults := make(map[string]interface{})

	connResult, err := RunConnectivityPhase(b001, flags.Targets, flags, params, logger, 0, totalSteps, progress)
	if err != nil {
		return err
	}
	hostInfos := connResult.HostInfos
	defer func() {
		for _, h := range hostInfos {
			h.Executor.Close()
		}
	}()

	stepIdx := connResult.NextStepIndex

	if m001 != nil {
		logger.Info("======== Phase: M-001 platform detect ========")
		res := RunPerHostStepsEx([]*runner.Step{m001}, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx++
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
		var err error
		hostInfos, flags, err = mysqlRefreshHostPlatforms(cmd, hostInfos, flags, sharedResults, logger)
		if err != nil {
			return err
		}
	}

	platform := detectSharedPlatform(sharedResults)
	logger.Info("Detected target platform: %s", platform)

	if !mysqlSkipOS {
		switch platform {
		case mysqlsteps.PlatformWindows:
			if len(winOSSteps) > 0 {
				logger.Info("======== Phase: Windows OS pre-instance ========")
				res := RunPerHostStepsEx(winOSSteps, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
				stepIdx += len(winOSSteps)
				if res.LastError != nil {
					return res.LastError
				}
				if flags.Precheck && res.PrecheckFailed {
					return fmt.Errorf("precheck failed")
				}
			}
		default:
			if len(osSteps) > 0 {
				logger.Info("======== Phase: OS baseline ========")
				res := RunPerHostStepsEx(osSteps, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
				stepIdx += len(osSteps)
				if res.LastError != nil {
					return res.LastError
				}
				if flags.Precheck && res.PrecheckFailed {
					return fmt.Errorf("precheck failed")
				}
			}
		}
	}

	if len(mysqlRest) > 0 {
		logger.Info("======== Phase: MySQL steps ========")
		res := RunPerHostStepsEx(mysqlRest, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx += len(mysqlRest)
		if res.LastError != nil {
			logger.Error("MySQL installation completed with errors")
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}

	if !mysqlSkipOS && platform == mysqlsteps.PlatformWindows && len(winOSPostSteps) > 0 {
		logger.Info("======== Phase: Windows OS post-instance ========")
		res := RunPerHostStepsEx(winOSPostSteps, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}
	return nil
}

func inferPlatformFromSSHUser(user string) string {
	if strings.EqualFold(strings.TrimSpace(user), "Administrator") {
		return "windows"
	}
	return "linux"
}

func inferPrimaryTargetPlatform(flags GlobalFlags, params map[string]interface{}) string {
	if v, ok := params["primary_platform"].(string); ok && v != "" {
		return v
	}
	user := mysqlStandbyPrimarySSHUser
	if user == "" {
		user = flags.SSHUser
	}
	if p := inferPlatformFromSSHUser(user); p == "windows" {
		return "windows"
	}
	if tp := inferTargetPlatformFromFlags(flags); tp != "" {
		return tp
	}
	return "linux"
}

func inferReplicaTargetPlatform(flags GlobalFlags, params map[string]interface{}) string {
	if v, ok := params["target_platform"].(string); ok && v != "" {
		return v
	}
	if tp := inferTargetPlatformFromFlags(flags); tp != "" {
		return tp
	}
	return "linux"
}

func newMysqlStandbyStepContext(ex runner.Executor, logger *logging.Logger, params map[string]interface{}, flags GlobalFlags) *runner.StepContext {
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
