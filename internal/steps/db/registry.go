package db

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 DB 安装 steps（顺序即默认执行顺序；C-001～C-034 连续编号）
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// 前置：连通性与 YAC 条件（C-001 在 db.go 里作为全局 precheck 执行）
		StepC001Check(),
		StepC002PortCheck(),
		StepC003HomeCheck(),

		StepC004CreateInstallDir(),
		StepC005CreateDataDirs(),
		StepC006SetDirOwnership(),

		StepC007ExtractPackage(),
		StepC008CleanStaleBashrc(),

		StepC009VIPCheck(),
		StepC010WriteHosts(),
		StepC011ScanDNS(),
		StepC012DiskCheck(),
		StepC013ScanNameCheck(),

		StepC014GenConfig(),
		StepC015SetCharacterSet(),
		StepC016DisableArchivelog(),
		StepC017ConfigureRedo(),
		StepC018SetNativeType(),
		StepC019TuneYFSParams(),

		StepC020InstallSoftware(),
		StepC021DeployDatabase(),
		StepC022CreateArchDG(),

		StepC023SetEnvVars(),
		StepC024CreatePluggableDatabases(),
		StepC025ConfigureDefaultProfile(),
		StepC026ApplySpfileParams(),
		StepC027ConfigureTPCC(),
		StepC028ConfigureUnifiedAudit(),
		StepC029ExecuteCustomSQL(),
		StepC030RestartDatabase(),
		StepC031VerifyInstall(),

		StepC032ConfigAutostartScript(),
		StepC033ConfigAutostartService(),

		StepC034ShowClusterStatus(),
	}
}
