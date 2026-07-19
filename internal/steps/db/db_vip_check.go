package db

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

const maxVIPSearchAttempts = 10

// pingResultAdapter 将 ExecResultForC001 适配为 commonos.PingExecResult
type pingResultAdapter struct{ r ExecResultForC001 }

func (a *pingResultAdapter) GetExitCode() int {
	if a.r == nil {
		return -1
	}
	return a.r.GetExitCode()
}

// pingExecutorAdapter 将 ExecutorForC001 适配为 commonos.PingExecutor
type pingExecutorAdapter struct{ e ExecutorForC001 }

func (a *pingExecutorAdapter) Execute(cmd string, sudo bool) (commonos.PingExecResult, error) {
	r, err := a.e.Execute(cmd, sudo)
	if err != nil || r == nil {
		return nil, err
	}
	return &pingResultAdapter{r: r}, nil
}

// stepVipCheck VIP 校验或自动生成步骤（vip/scan 模式）
func stepVipCheck() *runner.Step {
	return &runner.Step{
		Name:        "Validate or Auto-Generate VIP",
		Description: "YAC vip mode: validate configured VIPs or auto-generate from next IPs",
		Tags:        []string{"db", "yac", "vip", "validation"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("yac_mode", false) {
				return nil
			}
			if !YACAccessModeRequiresVIP(ctx.GetParamString("yac_access_mode", "vip")) {
				return fmt.Errorf("direct access mode: VIP check not required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-009: Validate or Auto-Generate VIP")
			if !ctx.GetParamBool("yac_mode", false) {
				return nil
			}
			if !YACAccessModeRequiresVIP(ctx.GetParamString("yac_access_mode", "vip")) {
				return nil
			}
			return RunVIPValidationOrAutoGenerate(HostExecsFromStepContext(ctx), ctx.Params, ctx.Logger)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// ValidateYACVIPsConfigured 校验已配置 VIP：数量/IPv4 + 从 hosts[0] ping 确认未被占用（无自动生成）。
// 供装库 C-009 与 standby CE 备路径共用。
func ValidateYACVIPsConfigured(hosts []HostExec, vips []string, logger *logging.Logger) error {
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts for VIP check")
	}
	if err := ValidateYACVIPList(vips, len(hosts)); err != nil {
		return err
	}
	for _, vip := range vips {
		host := NormalizeYACVIPHost(vip)
		inUse, err := commonos.PingFromHost(&pingExecutorAdapter{e: hosts[0].Executor}, host)
		if err != nil {
			return fmt.Errorf("failed to check VIP %s: %w", host, err)
		}
		if inUse {
			return fmt.Errorf("VIP %s is already in use by another server (ping responded)", host)
		}
	}
	if logger != nil {
		logger.Info("VIP addresses validated: %v (not in use)", vips)
	}
	return nil
}

// RunVIPValidationOrAutoGenerate 校验用户配置的 VIP，或在策略允许时自动生成。
// vip/scan 模式由 db.go 调用；direct 模式不调用。
func RunVIPValidationOrAutoGenerate(hosts []HostExec, params map[string]interface{}, logger *logging.Logger) error {
	if !YACAccessModeRequiresVIP(getParamString(params, "yac_access_mode", "vip")) {
		return nil
	}
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts for VIP check")
	}

	firstHost := hosts[0].Host
	logger.ConsoleWithType(StepIDByName("Validate or Auto-Generate VIP"), "Validate or Auto-Generate VIP", firstHost, "start", "", "", 0)
	logger.Info("Running VIP validation or auto-generation...")

	vips := getParamStringSliceFromParams(params, "yac_vips")
	if len(vips) > 0 {
		if err := ValidateYACVIPsConfigured(hosts, vips, logger); err != nil {
			return err
		}
		logger.ConsoleWithType(StepIDByName("Validate or Auto-Generate VIP"), "Validate or Auto-Generate VIP", firstHost, "success", "", "", time.Duration(0))
		return nil
	}

	// 3. 自动生成 VIP：每节点一个 VIP，且各节点 VIP 互不相同
	// 候选 VIP 不得与任意节点永久 IP 或已分配 VIP 重复
	permanentIPs := make(map[string]bool)
	for _, h := range hosts {
		permanentIPs[strings.TrimSpace(h.Host)] = true
	}
	generated := make([]string, 0, len(hosts))
	for i, h := range hosts {
		hostIP := strings.TrimSpace(h.Host)
		if hostIP == "" || !commonos.IsValidIPv4(hostIP) {
			return fmt.Errorf("host %d has invalid permanent IP: %s", i+1, hostIP)
		}
		nextIP, ok := commonos.NextIPv4(hostIP)
		if !ok {
			return fmt.Errorf("cannot compute next IP for host %s", hostIP)
		}
		var vip string
		for attempt := 0; attempt < maxVIPSearchAttempts; attempt++ {
			candidate := nextIP
			if permanentIPs[candidate] || containsString(generated, candidate) {
				nextIP, ok = commonos.NextIPv4(candidate)
				if !ok {
					break
				}
				continue
			}
			inUse, err := commonos.PingFromHost(&pingExecutorAdapter{e: h.Executor}, candidate)
			if err != nil {
				return fmt.Errorf("failed to check candidate VIP %s on %s: %w", candidate, h.Host, err)
			}
			if !inUse {
				vip = candidate
				break
			}
			nextIP, ok = commonos.NextIPv4(candidate)
			if !ok {
				break
			}
		}
		if vip == "" {
			return fmt.Errorf("no available VIP found for node %s after %d attempts; please specify --yac-vips manually", h.Host, maxVIPSearchAttempts)
		}
		generated = append(generated, vip)
		logger.Info("Node %s: auto-generated VIP %s", h.Host, vip)
	}

	params["yac_vips"] = generated
	logger.Info("Auto-generated VIP addresses: %v", generated)
	logger.ConsoleWithType(StepIDByName("Validate or Auto-Generate VIP"), "Validate or Auto-Generate VIP", firstHost, "success", "", fmt.Sprintf("VIPs: %v", generated), time.Duration(0))
	return nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func getParamStringSliceFromParams(params map[string]interface{}, key string) []string {
	if params == nil {
		return nil
	}
	v, ok := params[key]
	if !ok || v == nil {
		return nil
	}
	if s, ok := v.([]string); ok {
		return s
	}
	if slice, ok := v.([]interface{}); ok {
		var out []string
		for _, e := range slice {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
