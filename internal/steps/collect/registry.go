// registry.go - collect 步骤注册表
// 按文档规定的执行顺序注册全部 R-001～R-034 步骤。
package collect

import (
	"github.com/yinstall/internal/runner"
)

// GetAllSteps 返回按执行顺序排列的全部 collect 步骤。
// 步骤顺序遵循计划文档：连通 → 初始化 → 基础环境 → 网络/存储 → DB 信息 → 日志 → YAC → 收尾。
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepR001CheckConnectivity(),  // 连通性检查（wrap B-001）
		StepR002InitArchiveDir(),     // 初始化归档目录
		StepR003SnapshotInstallRun(), // Optional：快照安装参数
		StepR004DiscoverEnv(),        // Optional：自动发现 DB 环境
		StepR010HostIdentity(),       // 主机基础信息
		StepR011CollectDMI(),         // Optional：DMI 硬件信息
		StepR012UserLimits(),         // 产品用户 limits
		StepR013KernelParams(),       // 内核参数
		StepR014TimeNTP(),            // Optional：时间/NTP
		StepR015NetworkInterfaces(),  // 网卡信息（RHEL7/8 分支）
		StepR016NetworkRoutesDNS(),   // 路由/hosts/端口
		StepR017FirewallStatus(),     // Optional：防火墙规则
		StepR018PackagesYUM(),        // Optional：软件包列表
		StepR019StorageLVM(),         // 存储/LVM
		StepR020DBPathsVersion(),     // Optional：DB 路径与版本
		StepR021DBConfigFiles(),      // Optional：DB 配置文件
		StepR022DBFilesystemLayout(), // Optional：DB 文件系统布局
		StepR023DBClusterStatus(),    // Optional：集群状态
		StepR024DBProcessesPorts(),   // 数据库进程与端口
		StepR025DBAutostartArchDG(),  // Optional：自启动配置
		StepR026DBSQLCatalog(),       // Optional：SQL 目录信息
		StepR027DBConfigDrift(),      // Optional：配置漂移检测
		StepR035CustomRules(),        // Optional：配置驱动的扩展规则（embed + --rules-file）
		StepR034CollectDBLogs(),      // Optional：数据库日志（时间窗守卫）
		StepR030YACClusterInfo(),     // Optional Global：YAC 集群汇总
		StepR028SessionLogs(),        // Optional：归档会话日志
		StepR029FinalizeManifest(),   // Global：生成 manifest.json + summary.md
	}
}
