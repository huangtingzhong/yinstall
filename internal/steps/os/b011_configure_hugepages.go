package os

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB011ConfigureHugepages 按库内存需求配置 huge pages
func StepB011ConfigureHugepages() *runner.Step {
	return &runner.Step{
		ID:          "B-011",
		Name:        "Configure Huge Pages",
		Description: "Configure huge pages based on database memory requirements",
		Tags:        []string{"os", "hugepages", "memory"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// 是否启用 hugepages 配置
			enableHugepages := ctx.GetParamBool("os_hugepages_enable", false)
			if !enableHugepages {
				return fmt.Errorf("hugepages configuration is disabled, skipping")
			}

			// 能否读取内存信息
			result, err := ctx.Execute("cat /proc/meminfo | grep MemTotal", false)
			if err != nil || result.GetExitCode() != 0 {
				return fmt.Errorf("failed to read memory info")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// 数据库内存占用百分比（来自参数 / yasboot 规划）
			dbMemoryPercent := ctx.GetParamInt("db_memory_percent", 50)

			// 读取物理内存总量（KB）
			result, err := ctx.Execute("cat /proc/meminfo | grep MemTotal | awk '{print $2}'", false)
			if err != nil || result.GetExitCode() != 0 {
				return fmt.Errorf("failed to get total memory: %w", err)
			}

			totalMemKB, err := strconv.ParseInt(strings.TrimSpace(result.GetStdout()), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse total memory: %w", err)
			}

			totalMemGB := totalMemKB / 1024 / 1024

			ctx.Logger.Info("Total physical memory: %d GB", totalMemGB)
			ctx.Logger.Info("Database memory percent (from yasboot): %d%%", dbMemoryPercent)

			// 按物理内存档位决定 hugepages 占用物理内存的比例
			var hugepagesMemPercent int
			if totalMemGB < 32 {
				hugepagesMemPercent = 50
				ctx.Logger.Info("Physical memory < 32GB, using 50%% for hugepages")
			} else {
				hugepagesMemPercent = 70
				ctx.Logger.Info("Physical memory >= 32GB, using 70%% for hugepages")
			}

			// hugepages 目标内存（KB）
			hugepagesMemKB := totalMemKB * int64(hugepagesMemPercent) / 100

			// hugepage 页大小（通常为 2048 KB）
			result, err = ctx.Execute("cat /proc/meminfo | grep Hugepagesize | awk '{print $2}'", false)
			if err != nil || result.GetExitCode() != 0 {
				return fmt.Errorf("failed to get hugepage size: %w", err)
			}

			hugepageSizeKB, err := strconv.ParseInt(strings.TrimSpace(result.GetStdout()), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse hugepage size: %w", err)
			}

			// 计算 hugepages 个数
			nrHugepages := hugepagesMemKB / hugepageSizeKB

			ctx.Logger.Info("Hugepage size: %d KB", hugepageSizeKB)
			ctx.Logger.Info("Hugepages memory: %d GB (%d%%)", hugepagesMemKB/1024/1024, hugepagesMemPercent)
			ctx.Logger.Info("Number of hugepages: %d", nrHugepages)

			if ctx.DryRun {
				ctx.Logger.Info("[DRY-RUN] Would configure %d hugepages", nrHugepages)
				return nil
			}

			// 写入 sysctl 配置 hugepages
			sysctlFile := "/etc/sysctl.d/yashandb-hugepages.conf"
			sysctlContent := fmt.Sprintf("# YashanDB Huge Pages Configuration\nvm.nr_hugepages = %d\n", nrHugepages)

			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", sysctlFile, sysctlContent)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to write hugepages sysctl config: %w", err)
			}

			ctx.Logger.Info("Hugepages sysctl config written to %s", sysctlFile)

			// 应用 sysctl
			if _, err := ctx.ExecuteWithCheck("sysctl -p "+sysctlFile, true); err != nil {
				return fmt.Errorf("failed to apply hugepages sysctl config: %w", err)
			}

			ctx.Logger.Info("Hugepages sysctl config applied successfully")

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// 校验 HugePages_Total
			result, err := ctx.Execute("cat /proc/meminfo | grep HugePages_Total | awk '{print $2}'", false)
			if err != nil || result.GetExitCode() != 0 {
				return fmt.Errorf("failed to verify hugepages configuration")
			}

			totalHugepages := strings.TrimSpace(result.GetStdout())
			ctx.Logger.Info("HugePages_Total: %s", totalHugepages)

			// 是否已实际分配 hugepages（0 可能需重启或内存可用性）
			if totalHugepages == "0" {
				ctx.Logger.Warn("Hugepages configured but not allocated yet (may require reboot or memory availability)")
			}

			return nil
		},
	}
}
