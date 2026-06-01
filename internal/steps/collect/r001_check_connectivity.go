// r001_check_connectivity.go - 连通性检查步骤
// 复用 ossteps.StepB001CheckConnectivity，仅将 ID 改为 R-001，零逻辑复制。
package collect

import (
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
)

// StepR001CheckConnectivity 返回连通性检查步骤（R-001）。
// 直接复用 B-001 实现，仅覆盖 ID，确保与 collect 步骤序列一致。
func StepR001CheckConnectivity() *runner.Step {
	s := ossteps.StepB001CheckConnectivity()
	s.ID = "R-001"
	return s
}
