package os

import (
	"fmt"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB008WriteSysctlConfig Write sysctl config
func StepB008WriteSysctlConfig() *runner.Step {
	return &runner.Step{
		ID:          "B-008",
		Name:        "Write Sysctl Config",
		Description: "Write kernel parameters configuration file",
		Tags:        []string{"os", "kernel"},
		Optional:    false,

		Action: func(ctx *runner.StepContext) error {
			configFile := ctx.GetParamString("os_sysctl_file", "/etc/sysctl.d/yashandb.conf")

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
			shmmax, shmall, shmmni := commonos.ComputeKernelShmSizing(memKB, pageSize, useMaxRAM, dbPct)

			ctx.Logger.Info("Sysctl shared memory: MemTotal=%d kB, page_size=%d, use_max_ram_only=%v, db_memory_percent=%d -> shmmax=%d shmmall=%d shmmni=%d",
				memKB, pageSize, useMaxRAM, dbPct, shmmax, shmall, shmmni)

			config := fmt.Sprintf(`# YashanDB kernel parameters
vm.swappiness = 0
net.ipv4.ip_local_port_range = 32768 60999
vm.max_map_count = 2000000
net.core.somaxconn = 32768
kernel.shmall = %d
kernel.shmmni = %d
kernel.shmmax = %d
fs.aio-max-nr = 6194304
vm.dirty_ratio = 20
vm.dirty_background_ratio = 3
vm.dirty_writeback_centisecs = 100
vm.dirty_expire_centisecs = 500
vm.min_free_kbytes = 524288
net.core.netdev_max_backlog = 30000
net.core.netdev_budget = 600
`, shmall, shmmni, shmmax)

			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", configFile, config)
			_, err = ctx.ExecuteWithCheck(cmd, true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			configFile := ctx.GetParamString("os_sysctl_file", "/etc/sysctl.d/yashandb.conf")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s && echo exists", configFile), false)
			if !strings.Contains(result.GetStdout(), "exists") {
				return fmt.Errorf("sysctl config file not created")
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
