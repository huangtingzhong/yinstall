package os

import (
	"fmt"
	"strings"

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

			config := `# YashanDB kernel parameters
vm.swappiness = 0
net.ipv4.ip_local_port_range = 32768 60999
vm.max_map_count = 2000000
net.core.somaxconn = 32768
kernel.shmall = 3774873
kernel.shmmni = 4096
kernel.shmmax = 246960619520
fs.aio-max-nr = 6194304
vm.dirty_ratio = 20
vm.dirty_background_ratio = 3
vm.dirty_writeback_centisecs = 100
vm.dirty_expire_centisecs = 500
vm.min_free_kbytes = 524288
net.core.netdev_max_backlog = 30000
net.core.netdev_budget = 600
vm.overcommit_memory = 2
vm.overcommit_ratio = 90
`
			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", configFile, config)
			_, err := ctx.ExecuteWithCheck(cmd, true)
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
