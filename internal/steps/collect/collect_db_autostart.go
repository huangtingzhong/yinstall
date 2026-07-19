// r025_db_autostart.go - 数据库自启动配置采集（可选）
// YashanDB 自启动通过 yashan_monit.sh + systemd 实现（C-033 步骤创建）：
//   - 默认端口 1688 单实例：service=yashan_monit，arg=bashrc
//   - 非默认端口：service=yashan_monit_<port>，arg=<port>（与 .portN 对齐）
//   - 默认端口多实例：service=yashan_monit_1688，arg=1688
//   - 脚本位置：/usr/local/bin/yashan_monit.sh（与 commonos.ScriptPath 一致）
//
// 本步骤采集服务状态、脚本内容及 unit 文件，写入 db/autostart.json 和 db/autostart/。
package collect

import (
	"fmt"
	"path/filepath"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const monitScriptPath = "/usr/local/bin/yashan_monit.sh"

// stepDbAutostart 返回 R-025 步骤：采集数据库自启动配置（Optional）。
func stepDbAutostart() *runner.Step {
	return &runner.Step{
		Name:     "Collect DB autostart config",
		Optional: true,
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "db", "autostart")

			// 确定服务名候选列表（与 C-033 / commonos.DetermineServiceName 一致）
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			yasdbCount := commonos.GetYasdbProcessCount(ctx)
			singleService, _ := commonos.DetermineServiceName(yasdbCount, beginPort)
			// 也枚举常见的多实例服务名
			serviceNames := []string{
				singleService,
				"yashan_monit",
				fmt.Sprintf("yashan_monit_%d", beginPort),
			}
			seen := make(map[string]bool)
			var uniqueServices []string
			for _, s := range serviceNames {
				if !seen[s] {
					seen[s] = true
					uniqueServices = append(uniqueServices, s)
				}
			}

			// 采集每个候选服务的状态与 unit 文件
			serviceStatus := make(map[string]interface{})
			cmdTimeout := collectCmdTimeout(ctx)
			for _, svc := range uniqueServices {
				isEnabled, _ := collectExecute(ctx,
					fmt.Sprintf("systemctl is-enabled %s 2>/dev/null || echo not-found", svc), false, cmdTimeout)
				isActive, _ := collectExecute(ctx,
					fmt.Sprintf("systemctl is-active %s 2>/dev/null || echo not-found", svc), false, cmdTimeout)
				unitContent, _ := collectExecute(ctx,
					fmt.Sprintf("cat /etc/systemd/system/%s.service 2>/dev/null || true", svc), false, cmdTimeout)

				status := map[string]string{}
				if isEnabled != nil {
					status["enabled"] = isEnabled.GetStdout()
				}
				if isActive != nil {
					status["active"] = isActive.GetStdout()
				}
				if unitContent != nil && unitContent.GetExitCode() == 0 {
					status["unit_file"] = unitContent.GetStdout()
					// 写入独立文件便于查阅
					_ = writeTextFile(filepath.Join(dir, svc+".service"), unitContent.GetStdout())
				}
				serviceStatus[svc] = status
			}

			// 采集 yashan_monit.sh 脚本内容
			scriptContent, _ := collectExecute(ctx,
				fmt.Sprintf("cat %s 2>/dev/null || true", monitScriptPath), false, cmdTimeout)
			if scriptContent != nil && scriptContent.GetExitCode() == 0 {
				_ = writeTextFile(filepath.Join(dir, "yashan_monit.sh"), scriptContent.GetStdout())
			}
			scriptExists, _ := collectExecute(ctx,
				fmt.Sprintf("test -x %s && echo yes || echo no", monitScriptPath), false, cmdTimeout)

			// 采集 rc.local 中的 yashan 相关行
			_ = runAndSave(ctx,
				"grep -iE 'yashan|yasdb|yashan_monit' /etc/rc.d/rc.local /etc/rc.local 2>/dev/null || true",
				filepath.Join(dir, "rc-local.txt"))

			// 列出所有 systemd 系统服务中包含 yashan/yasdb 的条目
			_ = runAndSave(ctx,
				"systemctl list-units --type=service 2>/dev/null | grep -iE 'yashan|yasdb' || true",
				filepath.Join(dir, "systemctl-all-yashan.txt"))
			_ = runAndSave(ctx,
				"ls /etc/systemd/system/ 2>/dev/null | grep -iE 'yashan|yasdb' || true",
				filepath.Join(dir, "systemd-unit-list.txt"))

			// 结构化摘要 JSON
			scriptExistsStr := "unknown"
			if scriptExists != nil {
				scriptExistsStr = scriptExists.GetStdout()
			}
			autostart := map[string]interface{}{
				"host":                ctx.Executor.Host(),
				"monit_script_path":   monitScriptPath,
				"monit_script_exists": scriptExistsStr,
				"determined_service":  singleService,
				"services":            serviceStatus,
			}
			if err := writeJSON(filepath.Join(collectHostDir(ctx), "db", "autostart.json"), autostart); err != nil {
				appendWarning(ctx, err.Error())
			}

			ctx.Logger.Info("[R-025] DB autostart config collected (service=%s)", singleService)
			return nil
		},
	}
}
