// appliance.go - 一体机平台探测（可扩展多厂商）
package os

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// ApplianceNone 表示未识别为一体机平台。
	ApplianceNone = ""
	// ApplianceEnmotech 恩墨一体机。
	ApplianceEnmotech = "enmotech"

	enmotechZdataDir     = "/opt/zdata"
	enmotechRceAgentPath = "/opt/zdata/compute/bin/rce_agent"
)

// MatchEnmotechRceAgentArgs 判断 ps 参数行是否为恩墨 rce_agent（argv0 路径完全一致）。
func MatchEnmotechRceAgentArgs(argsLine string) bool {
	fields := strings.Fields(strings.TrimSpace(argsLine))
	if len(fields) == 0 {
		return false
	}
	return fields[0] == enmotechRceAgentPath
}

// DetectAppliancePlatform 在 ctx.Executor 对应主机上探测一体机平台。
// 当前支持恩墨；后续在此增加其它 detect* 分支即可。
func DetectAppliancePlatform(ctx *runner.StepContext) string {
	if ctx == nil {
		return ApplianceNone
	}
	if detectEnmotechAppliance(ctx) {
		return ApplianceEnmotech
	}
	return ApplianceNone
}

func detectEnmotechAppliance(ctx *runner.StepContext) bool {
	dirRes, _ := ctx.Execute("test -d "+enmotechZdataDir, false)
	if dirRes == nil || dirRes.GetExitCode() != 0 {
		return false
	}
	psRes, _ := ctx.Execute("ps -eo args=", false)
	if psRes == nil || psRes.GetExitCode() != 0 {
		return false
	}
	for _, line := range strings.Split(psRes.GetStdout(), "\n") {
		if MatchEnmotechRceAgentArgs(line) {
			return true
		}
	}
	return false
}
