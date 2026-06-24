package clean

import "github.com/yinstall/internal/runner"

// GetAllSteps returns all clean steps (DB 为分步列表；CLEAN-DB 单步仍可通过 GetStepByID 按 -s 选用)。
func GetAllSteps() []*runner.Step {
	steps := make([]*runner.Step, 0, len(GetDBCleanSteps())+2)
	steps = append(steps, GetDBCleanSteps()...)
	steps = append(steps, StepCleanYCM(), StepCleanYMP())
	return steps
}

// GetDBCleanSteps returns detailed DB cleanup steps
func GetDBCleanSteps() []*runner.Step {
	return []*runner.Step{
		StepCleanDB001QueryYACDisks(),     // 查询 YAC 磁盘信息（在删除前）
		StepCleanDB002StopProcesses(),     // 停止进程
		StepCleanDB003RemoveDirectories(), // 删除目录
		StepCleanDB004RemoveConfig(),      // 删除配置文件
		StepCleanDB005CleanYACDisks(),     // 清理 YAC 共享磁盘
		StepCleanDB006FinalCheck(),        // 最终检查
	}
}

// GetStepByID returns a step by its ID（含遗留聚合步 CLEAN-DB）。
func GetStepByID(id string) *runner.Step {
	if id == "CLEAN-DB" {
		return StepCleanDB()
	}
	if id == "CLEAN-MYSQL" {
		return StepCleanMySQL()
	}
	for _, step := range GetAllSteps() {
		if step.ID == id {
			return step
		}
	}
	for _, step := range GetMysqlCleanSteps() {
		if step.ID == id {
			return step
		}
	}
	for _, step := range GetMssqlCleanSteps() {
		if step.ID == id {
			return step
		}
	}
	return nil
}
