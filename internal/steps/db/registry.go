package db

import "github.com/yinstall/internal/runner"

// GetAllSteps returns all DB installation steps
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// First: connectivity and YAC prerequisites (C-001 runs as global precheck in db.go)
		StepC001Check(),
		// Port check: verify db begin port is not in use (runs per host)
		StepC002PortCheck(),
		// Home check: verify no yasdb/yasagent processes under YASDB_HOME (runs per host)
		StepC003HomeCheck(),

		// Directory creation
		StepC004CreateInstallDir(),
		StepC005CreateDataDirs(),
		StepC006SetDirOwnership(),

		// Package extraction
		StepC007ExtractPackage(),

		// Clean stale .bashrc entries before su - yashan
		StepC008CleanStaleBashrc(),

		// VIP 占位；实际校验/自动生成在 db.go 中 C-009-VIP 预检查阶段执行
		StepC009VIPCheck(),
		// Write VIP/SCAN entries to /etc/hosts on all YAC nodes
		StepC010WriteHosts(),
		// SCAN DNS 校验（C-011）
		StepC011ScanDNS(),
		// Shared disk validation（C-012）
		StepC012DiskCheck(),
		// SCAN 名解析；实际逻辑在 db.go 中 C-013-SCAN 预检查阶段执行
		StepC013ScanNameCheck(),

		// Configuration
		StepC014GenConfig(),
		StepC015SetCharacterSet(),
		StepC016DisableArchivelog(),
		StepC017ConfigureRedo(), // Configure REDO file parameters
		StepC018SetNativeType(),
		// YFS tuning（C-019）
		StepC019TuneYFSParams(),

		// Installation
		StepC020InstallSoftware(),
		StepC021DeployDatabase(),
		StepC022ConfigureTPCC(),
		StepC023CreateArchDG(),

		// Post installation
		StepC024SetEnvVars(),
		StepC025VerifyInstall(),

		// Autostart configuration (optional)
		StepC026ConfigAutostartScript(),
		StepC027ConfigAutostartService(),

		// Final step: display cluster status
		StepC028ShowClusterStatus(),

		// Custom SQL script execution (optional)
		StepC029ExecuteCustomSQL(),
	}
}
