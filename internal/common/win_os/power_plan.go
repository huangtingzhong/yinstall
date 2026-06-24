package win_os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const highPerformanceGUID = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"

// PowerPlanMode reads os_power_plan param.
func PowerPlanMode(ctx *runner.StepContext) string {
	return strings.ToLower(strings.TrimSpace(ctx.GetParamString("os_power_plan", "high-performance")))
}

// ShouldApplyPowerPlan returns false when skip.
func ShouldApplyPowerPlan(ctx *runner.StepContext, profile Profile) bool {
	if !profile.EnablePowerPlan {
		return false
	}
	return PowerPlanMode(ctx) != "skip"
}

// ApplyPowerPlan sets Windows power plan.
func ApplyPowerPlan(ctx *runner.StepContext) (guid string, changed bool, err error) {
	mode := PowerPlanMode(ctx)
	targetGUID := highPerformanceGUID
	switch mode {
	case "balanced":
		targetGUID = "381b4222-f694-41f0-9685-ff5bb260df2e"
	case "high-performance", "":
		targetGUID = highPerformanceGUID
	default:
		return "", false, fmt.Errorf("unsupported os_power_plan: %s", mode)
	}

	list, _ := ctx.Execute(`powercfg /getactivescheme`, false)
	active := ""
	if list != nil {
		active = list.GetStdout()
	}
	if strings.Contains(strings.ToLower(active), strings.ToLower(targetGUID)) {
		return targetGUID, false, nil
	}

	cmd := fmt.Sprintf(`powercfg /setactive %s`, targetGUID)
	ctx.LogScriptPreview("powershell", "W-013 power plan", cmd)
	if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
		return "", false, err
	}
	return targetGUID, true, nil
}
