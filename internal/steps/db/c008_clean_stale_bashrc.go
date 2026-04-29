package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC008CleanStaleBashrc 清理 .bashrc 与 ~/.port* 中引用已不存在文件的 source 行，
// 避免后续步骤执行 `su - yashan` 时报错。
func StepC008CleanStaleBashrc() *runner.Step {
	return &runner.Step{
		ID:          "C-008",
		Name:        "Clean Stale Bashrc Entries",
		Description: "Remove stale environment entries from .bashrc and ~/.port* files before installation",
		Tags:        []string{"db", "env"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				_, err := commonos.GetUserHomeDir(hctx, user)
				if err != nil {
					return err
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				homeDir, err := commonos.GetUserHomeDir(hctx, user)
				if err != nil {
					continue
				}

				// 收集待扫描文件：
				// 1) ~/.bashrc（默认端口 1688 常用）
				// 2) ~/.port*（非默认端口场景）
				var filesToScan []string

				bashrc := fmt.Sprintf("%s/.bashrc", homeDir)
				r, _ := hctx.Execute(fmt.Sprintf("test -f %s", bashrc), false)
				if r != nil && r.GetExitCode() == 0 {
					filesToScan = append(filesToScan, bashrc)
				}

				// 枚举 ~/.port* 文件
				portResult, _ := hctx.Execute(fmt.Sprintf("ls %s/.port* 2>/dev/null", homeDir), false)
				if portResult != nil && portResult.GetStdout() != "" {
					for _, f := range strings.Split(strings.TrimSpace(portResult.GetStdout()), "\n") {
						f = strings.TrimSpace(f)
						if f != "" {
							filesToScan = append(filesToScan, f)
						}
					}
				}

				if len(filesToScan) == 0 {
					hctx.Logger.Info("No env files found on %s, skipping", th.Host)
					continue
				}

				for _, envFile := range filesToScan {
					cleaned := cleanStaleEntriesFromFile(hctx, th.Host, envFile)
					if cleaned > 0 {
						hctx.Logger.Info("Cleaned %d stale entries from %s on %s", cleaned, envFile, th.Host)
					} else {
						hctx.Logger.Info("No stale entries found in %s on %s", envFile, th.Host)
					}
				}
			}
			return nil
		},
	}
}

// cleanStaleEntriesFromFile 扫描单个 env 文件，删除指向不存在路径的 source/export 行。
// 返回删除的行数。
func cleanStaleEntriesFromFile(hctx *runner.StepContext, host, envFile string) int {
	cleaned := 0

	// 查找仍引用缺失文件的 `source .../.yasboot/...bashrc` 行
	cmd := fmt.Sprintf("grep -n 'source.*\\.yasboot/.*_yasdb_home/conf/.*\\.bashrc' %s 2>/dev/null", envFile)
	result, _ := hctx.Execute(cmd, false)
	if result != nil && result.GetStdout() != "" {
		lines := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
		for _, line := range lines {
			// 从 `source /path/to/file` 提取路径
			parts := strings.SplitN(line, "source ", 2)
			if len(parts) < 2 {
				continue
			}
			sourcePath := strings.TrimSpace(parts[1])

			// 被引用文件是否存在
			checkResult, _ := hctx.Execute(fmt.Sprintf("test -f %s", sourcePath), false)
			if checkResult != nil && checkResult.GetExitCode() == 0 {
				continue
			}

			hctx.Logger.Info("Removing stale entry on %s from %s: source %s (file not found)", host, envFile, sourcePath)
			escapedPath := strings.ReplaceAll(sourcePath, "/", "\\/")
			sedCmd := fmt.Sprintf("sed -i '/source.*%s/d' %s", escapedPath, envFile)
			hctx.Execute(sedCmd, false)
			cleaned++
		}
	}

	// 若 YASCS_HOME 指向目录不存在，则清理该行
	cmd = fmt.Sprintf("grep -oP 'export YASCS_HOME=\\K.*' %s 2>/dev/null", envFile)
	result, _ = hctx.Execute(cmd, false)
	if result != nil && result.GetStdout() != "" {
		yascsPath := strings.TrimSpace(result.GetStdout())
		checkResult, _ := hctx.Execute(fmt.Sprintf("test -d %s", yascsPath), false)
		if checkResult == nil || checkResult.GetExitCode() != 0 {
			hctx.Logger.Info("Removing stale YASCS_HOME on %s from %s: %s (dir not found)", host, envFile, yascsPath)
			commonos.BashrcRemoveLine(hctx, envFile, "export YASCS_HOME=")
			cleaned++
		}
	}

	return cleaned
}
