// registry.go - stressos 步骤注册表
// 按执行顺序注册全部 S-01～S-11 步骤。
package stressos

import (
	"github.com/yinstall/internal/runner"
)

// GetAllSteps 返回按执行顺序排列的全部 stressos 步骤。
// 步骤顺序：连通 → 初始化 → 依赖 → 快照前 → CPU/MEM/IO/NET 压测 → 运行时采集 → 快照后 → 收尾。
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepS01CheckConnectivity(), // S-01 连通性检查（复用 B-001）
		StepS02InitArchiveDir(),    // S-02 初始化归档目录
		StepS03InstallDeps(),       // S-03 Optional：安装依赖（sysbench/fio/sysstat/numactl）
		StepS04StartPerfCollect(),  // S-04 后台启动 OS 性能并行采集
		StepS05CPUBench(),          // S-05 Optional：CPU 压测（sysbench cpu）
		StepS06MEMBench(),          // S-06 Optional：内存压测（sysbench memory）
		StepS07IOBench(),           // S-07 Optional：IO 压测（fio 3 场景）
		StepS08NETBench(),          // S-08 Optional：网络压测（ping 延迟）
		StepS09RuntimeMetrics(),    // S-09 运行时指标采集（一次性采集快照）
		StepS10StopPerfCollect(),   // S-10 终止后台性能采集并下载数据
		StepS11Finalize(),          // S-11 生成 manifest.json + summary.md（后置步骤）
	}
}
