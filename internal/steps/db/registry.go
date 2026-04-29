package db

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 DB 安装 steps（顺序即默认执行顺序）
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// 前置：连通性与 YAC 条件（C-001 在 db.go 里作为全局 precheck 执行）
		StepC001Check(),
		// 端口检查：确认 db begin port 未被占用（逐主机）
		StepC002PortCheck(),
		// Home 检查：确认 YASDB_HOME 下无 yasdb/yasagent 残留进程（逐主机）
		StepC003HomeCheck(),

		// 目录创建
		StepC004CreateInstallDir(),
		StepC005CreateDataDirs(),
		StepC006SetDirOwnership(),

		// 解压安装包
		StepC007ExtractPackage(),

		// 清理失效的 .bashrc 引用（避免 su - yashan 前环境干扰）
		StepC008CleanStaleBashrc(),

		// VIP 占位；实际校验/自动生成在 db.go 中 C-009-VIP 预检查阶段执行
		StepC009VIPCheck(),
		// 将 VIP/SCAN 写入各 YAC 节点 /etc/hosts
		StepC010WriteHosts(),
		// SCAN DNS 校验（C-011）
		StepC011ScanDNS(),
		// 共享盘校验（C-012）
		StepC012DiskCheck(),
		// SCAN 名解析；实际逻辑在 db.go 中 C-013-SCAN 预检查阶段执行
		StepC013ScanNameCheck(),

		// 配置生成与参数调整
		StepC014GenConfig(),
		StepC015SetCharacterSet(),
		StepC016DisableArchivelog(),
		StepC017ConfigureRedo(), // 配置 REDO 文件参数
		StepC018SetNativeType(),
		// YFS 调优（C-019）
		StepC019TuneYFSParams(),

		// 软件安装与库部署
		StepC020InstallSoftware(),
		StepC021DeployDatabase(),
		StepC022ConfigureTPCC(),
		StepC023CreateArchDG(),

		// 安装后：环境变量与验证
		StepC024SetEnvVars(),
		StepC025VerifyInstall(),

		// 自启动配置（可选）
		StepC026ConfigAutostartScript(),
		StepC027ConfigAutostartService(),

		// 最后一步：展示集群状态
		StepC028ShowClusterStatus(),

		// 自定义 SQL 脚本（可选）
		StepC029ExecuteCustomSQL(),
	}
}
