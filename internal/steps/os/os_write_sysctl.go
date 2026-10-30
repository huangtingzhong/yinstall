package os

import (
	"fmt"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepWriteSysctl 写入 sysctl 内核参数到 /etc/sysctl.conf 托管块
func stepWriteSysctl() *runner.Step {
	return &runner.Step{
		Name:        "Write Sysctl Config",
		Description: "Write kernel parameters into /etc/sysctl.conf managed block",
		Tags:        []string{"os", "kernel"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			memKB, err := readMemTotalKB(ctx)
			if err != nil {
				return err
			}
			pageSize, err := readHostPageSize(ctx)
			if err != nil {
				return err
			}
			useMaxRAM := ctx.GetParamBool("os_sysctl_shm_use_max_ram_only", false)
			dbPct := ctx.GetParamInt("db_memory_percent", 50)
			if dbPct < 0 || dbPct > 100 {
				return fmt.Errorf("invalid db_memory_percent: %d (expected 0-100)", dbPct)
			}
			shmmax, shmall, shmmni := commonos.ComputeKernelShmSizing(memKB, pageSize, useMaxRAM, dbPct)
			ctx.SetResult("os_sysctl_mem_kb", memKB)
			ctx.SetResult("os_sysctl_page_size", pageSize)
			ctx.SetResult("os_sysctl_shmmax", shmmax)
			ctx.SetResult("os_sysctl_shmall", shmall)
			ctx.SetResult("os_sysctl_shmmni", shmmni)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "Write Sysctl Config")
			confFile := ctx.GetParamString("os_sysctl_file", commonos.DefaultSysctlConfFile)

			memKB, _ := ctx.Results["os_sysctl_mem_kb"].(int64)
			pageSize, _ := ctx.Results["os_sysctl_page_size"].(int64)
			shmmax, _ := ctx.Results["os_sysctl_shmmax"].(int64)
			shmall, _ := ctx.Results["os_sysctl_shmall"].(int64)
			shmmni, _ := ctx.Results["os_sysctl_shmmni"].(int64)
			if shmmax == 0 || shmall == 0 || shmmni == 0 {
				var err error
				memKB, err = readMemTotalKB(ctx)
				if err != nil {
					return err
				}
				pageSize, _ = readHostPageSize(ctx)
				useMaxRAM := ctx.GetParamBool("os_sysctl_shm_use_max_ram_only", false)
				dbPct := ctx.GetParamInt("db_memory_percent", 50)
				shmmax, shmall, shmmni = commonos.ComputeKernelShmSizing(memKB, pageSize, useMaxRAM, dbPct)
			}
			useMaxRAM := ctx.GetParamBool("os_sysctl_shm_use_max_ram_only", false)
			dbPct := ctx.GetParamInt("db_memory_percent", 50)

			ctx.Logger.Info("Sysctl shared memory: MemTotal=%d kB, page_size=%d, use_max_ram_only=%v, db_memory_percent=%d -> shmmax=%d shmmall=%d shmmni=%d",
				memKB, pageSize, useMaxRAM, dbPct, shmmax, shmall, shmmni)
			ctx.Logger.Info("Sysctl persist target: %s (managed block)", confFile)

			defaults := commonos.YashanSysctlDefaults(shmmax, shmall, shmmni)
			osLogPhase(ctx, "filter-keys-start", fmt.Sprintf("candidates=%d", len(defaults)))
			kept, skipped := commonos.FilterYashanSysctlKeysOnHost(ctx, defaults)
			if len(skipped) > 0 {
				osLogPhase(ctx, "filter-keys-skip", fmt.Sprintf("missing=%s", strings.Join(skipped, ",")))
				ctx.Logger.Info("Sysctl keys skipped (not present on host): %s", strings.Join(skipped, ", "))
			}
			osLogPhase(ctx, "filter-keys-done", fmt.Sprintf("kept=%d skipped=%d", len(kept), len(skipped)))
			if len(kept) == 0 {
				return fmt.Errorf("no sysctl keys available on this host after existence filter")
			}
			ctx.SetResult("os_sysctl_written_keys", kept)

			osLogPhase(ctx, "conf-block-start", confFile)
			confChanged, err := commonos.EnsureManagedSysctlConfBlock(ctx, confFile, kept)
			if err != nil {
				osLogPhase(ctx, "conf-block-fail", runner.TruncateForLog(err.Error(), 80))
				return err
			}
			if ctx.IsForceStep() {
				confChanged = true
			}
			if confChanged {
				osLogPhase(ctx, "conf-block-done", "rewritten")
			} else {
				osLogPhase(ctx, "conf-block-skip", "already_correct")
				ctx.Logger.Info("Sysctl managed block in %s already configured, skipping write", confFile)
			}

			// 删除历史 drop-in，避免与 conf 双轨
			osLogPhase(ctx, "dropin-cleanup-start", "remove legacy yashan sysctl drop-ins")
			commonos.RemoveYashanSysctlDropIns(ctx)
			osLogPhase(ctx, "dropin-cleanup-done", "ok")

			ctx.SetResult("os_sysctl_changed", confChanged)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			confFile := ctx.GetParamString("os_sysctl_file", commonos.DefaultSysctlConfFile)
			result, _ := ctx.Execute(fmt.Sprintf("grep -Fq %s %s && echo exists",
				commonos.ShellSingleQuote(commonos.SysctlManagedBeginMarker), commonos.ShellSingleQuote(confFile)), false)
			if result == nil || !strings.Contains(result.GetStdout(), "exists") {
				return fmt.Errorf("sysctl managed block not found in %s", confFile)
			}
			return nil
		},
	}
}

func readMemTotalKB(ctx *runner.StepContext) (int64, error) {
	result, err := ctx.Execute("awk '/^MemTotal:/{print $2}' /proc/meminfo", false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return 0, fmt.Errorf("failed to read MemTotal from /proc/meminfo")
	}
	v, err := strconv.ParseInt(strings.TrimSpace(result.GetStdout()), 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid MemTotal value: %w", err)
	}
	return v, nil
}

func readHostPageSize(ctx *runner.StepContext) (int64, error) {
	result, err := ctx.Execute("getconf PAGE_SIZE 2>/dev/null || echo 4096", false)
	if err != nil || result == nil {
		return 4096, nil
	}
	out := strings.TrimSpace(result.GetStdout())
	if out == "" {
		return 4096, nil
	}
	v, err := strconv.ParseInt(out, 10, 64)
	if err != nil || v <= 0 {
		return 4096, nil
	}
	return v, nil
}
