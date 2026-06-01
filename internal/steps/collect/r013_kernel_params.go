// r013_kernel_params.go - 内核参数采集
// 采集 sysctl 参数、内核启动参数、grub 配置等，
// 写入 os/kernel/ 目录。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepR013KernelParams 返回 R-013 步骤：采集内核参数。
func StepR013KernelParams() *runner.Step {
	return &runner.Step{
		ID:   "R-013",
		Name: "Collect kernel parameters",
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "kernel")

			// sysctl -a 需要 root 才能读取全部参数（部分受保护 key 需要特权），
			// 其余 cat 命令文件均世界可读，但统一走 sudo 避免残缺。
			cmds := []struct {
				cmd  string
				dest string
				sudo bool
			}{
				{"sysctl -a 2>/dev/null || true", filepath.Join(dir, "sysctl-all.txt"), true},
				{"cat /proc/cmdline 2>/dev/null || true", filepath.Join(dir, "cmdline.txt"), false},
				{"ls /etc/sysctl.d/ 2>/dev/null && cat /etc/sysctl.d/*.conf 2>/dev/null || true", filepath.Join(dir, "sysctl.d.txt"), false},
				{"cat /etc/sysctl.conf 2>/dev/null || true", filepath.Join(dir, "sysctl.conf.txt"), false},
			}

			// grub 配置因 RHEL 系列而异
			if ctx.OSInfo != nil && ctx.OSInfo.IsRHEL7 {
				cmds = append(cmds, struct {
					cmd  string
					dest string
					sudo bool
				}{"grubby --info=ALL 2>/dev/null || true", filepath.Join(dir, "grubby.txt"), false})
			} else {
				cmds = append(cmds, struct {
					cmd  string
					dest string
					sudo bool
				}{"cat /etc/default/grub 2>/dev/null || true", filepath.Join(dir, "grub-default.txt"), false})
			}

			collectLogPhase(ctx, "plan",
				fmt.Sprintf("cmds=%d sysctl_keys=10 dir=os/kernel", len(cmds)))

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest, c.sudo); err != nil {
					appendWarning(ctx, "R-013", err.Error())
				}
			}

			// 精选关键 sysctl 参数：需要 root 才能读全部 key
			keyParams := []string{
				"kernel.shmmax", "kernel.shmall", "kernel.shmmni",
				"vm.nr_hugepages", "vm.overcommit_memory", "vm.overcommit_ratio",
				"fs.file-max", "fs.nr_open",
				"net.core.somaxconn", "net.ipv4.tcp_fin_timeout",
			}
			paramMap := make(map[string]string, len(keyParams))
			for _, p := range keyParams {
				r, _ := collectExecute(ctx, "sysctl -n "+p+" 2>/dev/null || true", true, collectCmdTimeout(ctx))
				if r != nil && r.GetExitCode() == 0 {
					paramMap[p] = r.GetStdout()
				}
			}
			if err := writeJSON(filepath.Join(dir, "key-params.json"), paramMap); err != nil {
				appendWarning(ctx, "R-013", err.Error())
			}

			ctx.Logger.Info("[R-013] kernel params collected to %s", dir)
			return nil
		},
	}
}
