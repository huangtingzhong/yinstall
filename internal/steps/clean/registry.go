package clean

import "github.com/yinstall/internal/runner"

// GetAllSteps returns all clean steps
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		StepCleanDB(),
		StepCleanYCM(),
		StepCleanYMP(),
	}
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

// GetStepByID returns a step by its ID
func GetStepByID(id string) *runner.Step {
	// 先查找主步骤
	steps := GetAllSteps()
	for _, step := range steps {
		if step.ID == id {
			return step
		}
	}

	// 查找 DB 详细步骤
	dbSteps := GetDBCleanSteps()
	for _, step := range dbSteps {
		if step.ID == id {
			return step
		}
	}

	return nil
}
