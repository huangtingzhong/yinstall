package clean

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// buildFindYashanDBProcessPSCmd 构造 ps|grep，仅匹配当前清理目标相关进程：
// - PathMatchLiteralsForPS：尾 / 前缀 + 父目录/实例叶子，覆盖真实二进制路径与 -D/-L 无尾 /；
// - ~/.yasboot/<cluster>_yasdb_home/ 与 -c <cluster> 固定串，避免误伤其它实例。
func buildFindYashanDBProcessPSCmd(ctx *runner.StepContext, yasdbHome, yasdbData, yasdbLog, osUser, clusterName string, awkPrintPid bool) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	userHome, err := commonos.GetUserHomeDir(ctx, u)
	if err != nil {
		userHome = path.Join("/home", u)
	}

	var grepFe []string
	addPats := func(p string) {
		for _, pat := range PathMatchLiteralsForPS(p) {
			grepFe = append(grepFe, "-e "+commonos.ShellSingleQuote(pat))
		}
	}
	addPats(yasdbHome)
	addPats(yasdbData)
	addPats(yasdbLog)
	addPats(path.Join(userHome, ".yasboot", clusterName+"_yasdb_home"))
	if alt, ok := ctx.Results[resultKeyCleanAltYasdbHome].(string); ok && strings.TrimSpace(alt) != "" {
		addPats(alt)
	}
	if link, ok := ctx.Results[resultKeyCleanYasbootHomeLink].(string); ok && strings.TrimSpace(link) != "" {
		addPats(link)
	}
	// CLI 数据根（sourced 可能是 .../db-1-1）
	if paramData := strings.TrimSpace(ctx.GetParamString("yasdb_data", "")); paramData != "" {
		addPats(paramData)
	}
	grepFe = appendYACPathPatterns(ctx, grepFe, yasdbData)

	cn := strings.TrimSpace(clusterName)
	if cn != "" {
		// 空格定界，避免 yashandb 误匹配 yashandb_3788
		grepFe = append(grepFe, "-e "+commonos.ShellSingleQuote(" -c "+cn+" "))
		grepFe = append(grepFe, "-e "+commonos.ShellSingleQuote(" -c "+cn))
	}

	if len(grepFe) == 0 {
		return `false`
	}

	cmd := fmt.Sprintf(
		`ps -ef | grep -E '%s' | grep -F %s | grep -v grep | grep -v yinstall`,
		cleanDBProcessNamePattern(ctx),
		strings.Join(grepFe, " "),
	)
	if awkPrintPid {
		return cmd + ` | awk '{print $2}'`
	}
	return cmd
}

// yasbootOMListenPorts 由 begin-port 推导 yasom/yasagent 监听端口（begin-13 / begin-12）。
func yasbootOMListenPorts(beginPort int) (omPort, agentPort int) {
	if beginPort <= 13 {
		return 0, 0
	}
	return beginPort - 13, beginPort - 12
}

// collectCleanPIDs 合并多条 ps 查找命令的 PID（去重、去空）。
func collectCleanPIDs(ctx *runner.StepContext, cmds ...string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, cmd := range cmds {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" || cmd == "false" {
			continue
		}
		result, _ := ctx.Execute(cmd, false)
		if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
			continue
		}
		for _, pid := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
			pid = strings.TrimSpace(pid)
			if pid == "" {
				continue
			}
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			out = append(out, pid)
		}
	}
	return out
}

// buildFindYashanDBProcessByBeginPortPSCmd 按 begin-port 对应的 OM/DB 监听串匹配残留 yasom/yasagent/yasdb。
// 覆盖 env 已删、仅剩 --init OM 的场景（path/-c 可能匹配不到）。
// 默认端口 1688 禁用端口匹配，避免误杀现网 HA。
func buildFindYashanDBProcessByBeginPortPSCmd(ctx *runner.StepContext, beginPort int, awkPrintPid bool) string {
	if beginPort <= 0 || beginPort == 1688 {
		return `false`
	}
	omPort, agentPort := yasbootOMListenPorts(beginPort)
	if omPort == 0 {
		return `false`
	}
	cmd := fmt.Sprintf(
		`ps -ef | grep -E '%s' | grep -E %s | grep -v grep | grep -v yinstall`,
		cleanDBProcessNamePattern(ctx),
		commonos.ShellSingleQuote(fmt.Sprintf(":%d|:%d|:%d", omPort, agentPort, beginPort)),
	)
	if awkPrintPid {
		return cmd + ` | awk '{print $2}'`
	}
	return cmd
}

// stopCleanDBSystemdUnit 停用并禁用 yashan_monit_<port>（仅非默认端口，避免误动 1688 HA）。
func stopCleanDBSystemdUnit(ctx *runner.StepContext, beginPort int) {
	if beginPort <= 0 || beginPort == 1688 {
		return
	}
	unit := fmt.Sprintf("yashan_monit_%d", beginPort)
	ctx.Logger.Info("Stopping systemd unit %s (if present)...", unit)
	ctx.Execute(fmt.Sprintf("systemctl disable --now %s 2>/dev/null || true", commonos.ShellSingleQuote(unit)), true)
	ctx.Execute(fmt.Sprintf("rm -f /etc/systemd/system/%s.service 2>/dev/null; systemctl daemon-reload 2>/dev/null || true", unit), true)
}

// buildFindMonitPSCmd 仅匹配当前集群 monit（其 -c 指向 ~/.yasboot/<cluster>_yasdb_home/.../monitrc），
// 避免误杀其他实例的 monit。
func buildFindMonitPSCmd(ctx *runner.StepContext, osUser, clusterName string, awkPrintPid bool) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	userHome, err := commonos.GetUserHomeDir(ctx, u)
	if err != nil {
		userHome = path.Join("/home", u)
	}
	monitrc := strings.ReplaceAll(path.Join(userHome, ".yasboot", clusterName+"_yasdb_home", "om/monit/monitrc"), `\`, `/`)
	cmd := fmt.Sprintf(
		`ps -ef | grep monit | grep -F %s | grep -v grep | grep -v yinstall`,
		commonos.ShellSingleQuote(monitrc),
	)
	if awkPrintPid {
		return cmd + ` | awk '{print $2}'`
	}
	return cmd
}

// removeDir removes a directory with rm -rf after path validation and existence check.
func removeDir(ctx *runner.StepContext, path, label string) {
	if err := commonos.ValidateDeletePath(path); err != nil {
		ctx.Logger.Warn("Skipping removal of %s: invalid delete path %q: %v", label, path, err)
		return
	}
	pathQ := commonos.ShellSingleQuote(path)

	// Check if directory exists
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", pathQ), false)
	if result == nil || result.GetExitCode() != 0 {
		ctx.Logger.Info("Skipping removal of %s: directory does not exist (%s)", label, path)
		return
	}

	ctx.Logger.Info("Removing %s: %s", label, path)
	result, err := ctx.Execute(fmt.Sprintf("rm -rf %s", pathQ), true)
	if err != nil || (result != nil && result.GetExitCode() != 0) {
		ctx.Logger.Warn("Failed to remove %s: %v", label, err)
	} else {
		ctx.Logger.Info("%s removed successfully", label)
	}
}

// removeCleanDBDirectoryTree 删除 sourced 的 HOME/DATA/LOG、stage 目录；YAC 时另删 CLI 级 data 根与 /data/.../yasdb_home 软件树。
func removeCleanDBDirectoryTree(ctx *runner.StepContext) {
	yasdbHome, yasdbData, yasdbLog, _, _, err := effectiveCleanDBPaths(ctx)
	if err != nil {
		ctx.Logger.Warn("removeCleanDBDirectoryTree: %v", err)
		return
	}
	removeDir(ctx, yasdbHome, "YASDB_HOME")
	if alt, ok := ctx.Results[resultKeyCleanAltYasdbHome].(string); ok && strings.TrimSpace(alt) != "" {
		removeDir(ctx, alt, "YASDB_HOME (install tree)")
	}
	if link, ok := ctx.Results[resultKeyCleanYasbootHomeLink].(string); ok && strings.TrimSpace(link) != "" {
		removeDir(ctx, link, "YASDB_HOME (yasboot symlink)")
	}
	removeDir(ctx, yasdbData, "YASDB_DATA")
	paramData := path.Clean(strings.ReplaceAll(ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data"), `\`, `/`))
	dataClean := path.Clean(strings.ReplaceAll(yasdbData, `\`, `/`))
	if paramData != "" && paramData != dataClean {
		if commonos.DeletePathUnder(dataClean, paramData) || commonos.DeletePathUnder(paramData, dataClean) {
			removeDir(ctx, paramData, "YAC YASDB_DATA root")
		}
	}
	removeDir(ctx, yasdbLog, "YASDB_LOG")
	removeCleanYACExtraDirs(ctx)
	if stageDir, err := resolveCleanDBStageDir(ctx); err != nil {
		ctx.Logger.Warn("Skipping DB stage directory removal: %v", err)
	} else {
		removeDir(ctx, stageDir, "DB stage directory")
	}
}

// verifyDirRemoved checks that a directory no longer exists
func verifyDirRemoved(ctx *runner.StepContext, path, label string) {
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(path)), false)
	if result != nil && result.GetExitCode() == 0 {
		ctx.Logger.Warn("WARNING: %s still exists: %s", label, path)
	} else {
		ctx.Logger.Info("[OK] %s removed successfully", label)
	}
}

// verifyFileRemoved checks that a file no longer exists
func verifyFileRemoved(ctx *runner.StepContext, path, label string) {
	result, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(path)), false)
	if result != nil && result.GetExitCode() == 0 {
		ctx.Logger.Warn("WARNING: %s still exists: %s", label, path)
	} else {
		ctx.Logger.Info("[OK] %s removed successfully", label)
	}
}
