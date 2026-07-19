// r010_host_identity.go - 主机基础身份信息采集
// 采集 hostname、OS 版本、内核、CPU、内存等基础主机标识信息，
// 写入 os/identity/ 目录。
package collect

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepHostIdentity 返回 R-010 步骤：采集主机基础身份信息。
func stepHostIdentity() *runner.Step {
	return &runner.Step{
		Name: "Collect host identity",
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "identity")

			collectLogPhase(ctx, "plan", "cmds=8 dir=os/identity (+summary.json from archived files)")

			// 按命令与目标文件配对执行
			cmds := []struct {
				cmd  string
				dest string
			}{
				{"hostname -f", filepath.Join(dir, "hostname.txt")},
				{"uname -a", filepath.Join(dir, "uname.txt")},
				{"cat /etc/os-release 2>/dev/null || cat /etc/redhat-release 2>/dev/null || true", filepath.Join(dir, "os-release.txt")},
				{"cat /proc/cpuinfo", filepath.Join(dir, "cpuinfo.txt")},
				{"lscpu", filepath.Join(dir, "lscpu.txt")},
				{"free -h", filepath.Join(dir, "memory.txt")},
				{"cat /proc/meminfo", filepath.Join(dir, "meminfo.txt")},
				{"uptime", filepath.Join(dir, "uptime.txt")},
			}

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
					appendWarning(ctx, err.Error())
				}
			}

			// 结构化摘要 JSON
			identity := collectIdentityMap(ctx, dir)
			if err := writeJSON(filepath.Join(dir, "summary.json"), identity); err != nil {
				appendWarning(ctx, err.Error())
			}

			ctx.Logger.Info("[R-010] host identity collected to %s", dir)
			return nil
		},
	}
}

// collectIdentityMap 构建主机身份摘要 map（供 JSON 输出）；优先读已归档文本，避免重复远端命令。
func collectIdentityMap(ctx *runner.StepContext, dir string) map[string]string {
	readArchived := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	m := map[string]string{
		"hostname": readArchived("hostname.txt"),
		"uname":    "",
		"arch":     "",
	}
	if u := readArchived("uname.txt"); u != "" {
		fields := strings.Fields(u)
		if len(fields) >= 3 {
			m["uname"] = fields[2]
		}
		if len(fields) >= 2 {
			m["arch"] = fields[1]
		}
	}
	if m["hostname"] == "" {
		r, _ := collectExecute(ctx, "hostname -f", false, collectCmdTimeout(ctx))
		if r != nil {
			m["hostname"] = strings.TrimSpace(r.GetStdout())
		}
	}
	if m["arch"] == "" {
		r, _ := collectExecute(ctx, "uname -m", false, collectCmdTimeout(ctx))
		if r != nil {
			m["arch"] = strings.TrimSpace(r.GetStdout())
		}
	}
	if ctx.OSInfo != nil {
		m["os_name"] = ctx.OSInfo.Name
		m["os_version"] = ctx.OSInfo.Version
		m["os_id"] = ctx.OSInfo.ID
		m["os_kernel"] = ctx.OSInfo.Kernel
		m["os_arch"] = ctx.OSInfo.Arch
		m["os_family"] = osFamilyString(ctx.OSInfo)
	}
	return m
}
