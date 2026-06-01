// s001_check_connectivity.go - 连通性检查
// 复用 ossteps.StepB001CheckConnectivity，仅覆盖 ID 为 S-01，零逻辑复制。
package stressos

import (
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
)

// StepS01CheckConnectivity 返回 S-01 连通性检查步骤。
// 直接复用 B-001 实现，仅覆盖 ID。
func StepS01CheckConnectivity() *runner.Step {
	s := ossteps.StepB001CheckConnectivity()
	s.ID = "S-01"
	return s
}
