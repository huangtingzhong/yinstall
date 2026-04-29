package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB001CheckConnectivity 检查目标主机连通性与基础环境
func StepB001CheckConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "B-001",
		Name:        "Check Connectivity",
		Description: "Verify target host IP validity and SSH connection",
		Tags:        []string{"os", "connectivity", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			host := ctx.Executor.Host()

			// 1) 基础 SSH 命令
			result, err := ctx.ExecuteWithCheck("echo 'connection_ok'", false)
			if err != nil {
				return fmt.Errorf("SSH connection failed to %s: %w", host, err)
			}
			if !strings.Contains(result.GetStdout(), "connection_ok") {
				return fmt.Errorf("unexpected response from %s", host)
			}

			// 2) 主机名
			result, _ = ctx.Execute("hostname", false)
			hostname := ""
			if result != nil {
				hostname = strings.TrimSpace(result.GetStdout())
			}
			ctx.SetResult("hostname", hostname)

			// 3) OS 探测
			osInfo := commonos.DetectOSInfo(ctx)
			ctx.OSInfo = osInfo

			// 4) 权限 / sudo 能力（只读探测）
			result, _ = ctx.Execute("id -u", false)
			uid := ""
			if result != nil {
				uid = strings.TrimSpace(result.GetStdout())
			}
			isRoot := uid == "0"
			ctx.SetResult("is_root", isRoot)
			if !isRoot {
				result, _ = ctx.Execute("sudo -n true 2>/dev/null && echo 'sudo_ok'", false)
				hasSudo := result != nil && strings.Contains(result.GetStdout(), "sudo_ok")
				ctx.SetResult("has_sudo", hasSudo)
				if !hasSudo {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "B-001",
						StepName:    "Check Connectivity",
						Host:        host,
						Severity:    runner.PrecheckSeverityWarn,
						Code:        "PC.OS.SUDO",
						Message:     "Current user is not root and passwordless sudo is not available; subsequent sudo-required steps may fail",
						Remediation: "Run as root, or configure passwordless sudo (sudoers NOPASSWD) for the login user",
					})
				}
			}

			// 5) 内存信息（尽力而为）
			totalMem := ""
			result, _ = ctx.Execute("free -h 2>/dev/null | grep Mem | awk '{print $2}'", false)
			if result != nil {
				totalMem = strings.TrimSpace(result.GetStdout())
			}
			ctx.SetResult("total_memory", totalMem)

			// 6) CPU 核数（尽力而为）
			cpuCores := ""
			result, _ = ctx.Execute("nproc 2>/dev/null || grep -c processor /proc/cpuinfo", false)
			if result != nil {
				cpuCores = strings.TrimSpace(result.GetStdout())
			}
			ctx.SetResult("cpu_cores", cpuCores)

			// 7) 基础工具链是否存在（供后续步骤使用）
			commands := []string{"cat", "grep", "awk", "sed"}
			for _, cmd := range commands {
				res, _ := ctx.Execute(fmt.Sprintf("command -v %s >/dev/null 2>&1", cmd), false)
				if res == nil || res.GetExitCode() != 0 {
					return fmt.Errorf("required command '%s' not found", cmd)
				}
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			host := ctx.Executor.Host()
			hostname := ctx.GetParamString("hostname", "")
			if v, ok := ctx.Results["hostname"]; ok {
				if s, ok := v.(string); ok {
					hostname = s
				}
			}
			osInfo := ctx.OSInfo
			if osInfo == nil {
				// 兜底：正常情况下 PreCheck 已填充 OSInfo
				osInfo = commonos.DetectOSInfo(ctx)
				ctx.OSInfo = osInfo
			}
			totalMem := ""
			if v, ok := ctx.Results["total_memory"]; ok {
				if s, ok := v.(string); ok {
					totalMem = s
				}
			}
			cpuCores := ""
			if v, ok := ctx.Results["cpu_cores"]; ok {
				if s, ok := v.(string); ok {
					cpuCores = s
				}
			}
			isRoot := false
			if v, ok := ctx.Results["is_root"]; ok {
				if b, ok := v.(bool); ok {
					isRoot = b
				}
			}

			// 输出主机信息
			ctx.Logger.Info("Host: %s", host)
			ctx.Logger.Info("  Hostname:    %s", hostname)
			ctx.Logger.Info("  OS:          %s %s (%s)", osInfo.Name, osInfo.Version, osInfo.ID)
			ctx.Logger.Info("  Kernel:      %s", osInfo.Kernel)
			ctx.Logger.Info("  Arch:        %s", osInfo.Arch)
			ctx.Logger.Info("  CPU Cores:   %s", cpuCores)
			ctx.Logger.Info("  Memory:      %s", totalMem)
			ctx.Logger.Info("  Pkg Manager: %s", osInfo.PkgManager)
			if isRoot {
				ctx.Logger.Info("  Privilege:   root")
			} else {
				ctx.Logger.Info("  Privilege:   non-root (sudo required)")
			}

			// 输出 OS 类型标识
			var osTypes []string
			if osInfo.IsRHEL7 {
				osTypes = append(osTypes, "RHEL7")
			}
			if osInfo.IsRHEL8 {
				osTypes = append(osTypes, "RHEL8")
			}
			if osInfo.IsKylin {
				osTypes = append(osTypes, "Kylin")
			}
			if osInfo.IsUOS {
				osTypes = append(osTypes, "UOS")
			}
			if len(osTypes) > 0 {
				ctx.Logger.Info("  OS Type:     %s", strings.Join(osTypes, ", "))
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
