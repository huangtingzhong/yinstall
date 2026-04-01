package db

import "github.com/yinstall/internal/runner"

// GetAllSteps returns all DB installation steps
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// First: connectivity and YAC prerequisites (C-000 runs as global precheck in db.go)
		StepC000Check(),
		// Port check: verify db begin port is not in use (runs per host)
		StepC001PortCheck(),
		// Home check: verify no yasdb/yasagent processes under YASDB_HOME (runs per host)
		StepC002HomeCheck(),

		// Directory creation
		StepC003CreateInstallDir(),
		StepC004CreateDataDirs(),
		StepC005SetDirOwnership(),

		// Package extraction
		StepC006ExtractPackage(),

		// Clean stale .bashrc entries before su - yashan
		StepC006ACleanStaleBashrc(),

		// VIP validation or auto-generation (C-004-VIP runs as global precheck in db.go when YAC vip mode; must run before gen_config)
		StepC007VIPCheck(),
		// Write VIP/SCAN entries to /etc/hosts on all YAC nodes
		StepC007AWriteHosts(),
		// SCAN DNS validation (C-004-SCAN-DNS runs after VIP check; validates SCAN DNS resolution, subnet and IP availability)
		StepC008ScanDNS(),
		// Shared disk validation (C-004-DISK runs after VIP check; validates disk existence, ownership and permissions on all nodes)
		StepC009DiskCheck(),
		// SCAN name resolve and subnet check (C-005-SCAN runs as global precheck in db.go when YAC scan mode; must run before gen_config)
		StepC010ScanNameCheck(),

		// Configuration
		StepC011GenConfig(),
		StepC012SetCharacterSet(),
		StepC012AConfigureRedo(), // Configure REDO file parameters
		StepC013SetNativeType(),
		// YFS tuning (C-007-YFS runs after native type setting; configures YFS parameters for YAC)
		StepC014TuneYFSParams(),

		// Installation
		StepC015InstallSoftware(),
		StepC016DeployDatabase(),
		StepC016BConfigureTPCC(),
		StepC016ACreateArchDG(),

		// Post installation
		StepC017SetEnvVars(),
		StepC018VerifyInstall(),

		// Autostart configuration (optional)
		StepC019ConfigAutostartScript(),
		StepC020ConfigAutostartService(),

		// Final step: display cluster status
		StepC021ShowClusterStatus(),

		// Custom SQL script execution (optional)
		StepC022ExecuteCustomSQL(),
	}
}
