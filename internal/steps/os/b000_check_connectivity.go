package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB000CheckConnectivity Check target host connectivity
func StepB000CheckConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "B-000",
		Name:        "Check Connectivity",
		Description: "Verify target host IP validity and SSH connection",
		Tags:        []string{"os", "connectivity", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			host := ctx.Executor.Host()

			// 1. 检查是否能执行基本命令（证明 SSH 连接正常）
			result, err := ctx.ExecuteWithCheck("echo 'connection_ok'", false)
			if err != nil {
				return fmt.Errorf("SSH connection failed to %s: %w", host, err)
			}
			if !strings.Contains(result.GetStdout(), "connection_ok") {
				return fmt.Errorf("unexpected response from %s", host)
			}

			// 2. 获取主机名
			result, _ = ctx.Execute("hostname", false)
			hostname := ""
			if result != nil {
				hostname = strings.TrimSpace(result.GetStdout())
			}
			ctx.SetResult("hostname", hostname)

			// 3. 检测操作系统信息（使用公共函数）
			osInfo := commonos.DetectOSInfo(ctx)
			ctx.OSInfo = osInfo

			// 4. 检查是否有 root 权限或 sudo 权限
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
					ctx.Logger.Warn("User is not root and sudo requires password")
				}
			}

			// 5. 获取内存信息
			totalMem := ""
			result, _ = ctx.Execute("free -h 2>/dev/null | grep Mem | awk '{print $2}'", false)
			if result != nil {
				totalMem = strings.TrimSpace(result.GetStdout())
			}
			ctx.SetResult("total_memory", totalMem)

			// 6. 获取 CPU 核心数
			cpuCores := ""
			result, _ = ctx.Execute("nproc 2>/dev/null || grep -c processor /proc/cpuinfo", false)
			if result != nil {
				cpuCores = strings.TrimSpace(result.GetStdout())
			}
			ctx.SetResult("cpu_cores", cpuCores)

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
			// 验证基本命令可用
			commands := []string{"cat", "grep", "awk", "sed"}
			for _, cmd := range commands {
				result, err := ctx.Execute(fmt.Sprintf("which %s", cmd), false)
				if err != nil || result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("required command '%s' not found", cmd)
				}
			}
			return nil
		},
	}
}
