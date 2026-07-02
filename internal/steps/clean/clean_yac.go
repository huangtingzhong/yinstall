package clean

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const cleanYFSDiscoverRoot = "/dev/yfs"

var (
	cleanReAliasSys  = regexp.MustCompile(`^sys(\d+)$`)
	cleanReAliasData = regexp.MustCompile(`^data(\d+)$`)
	cleanReAliasArch = regexp.MustCompile(`^arch(\d+)$`)
)

// isCleanYACContext 判断是否按 YAC 清理（含单节点）。
func isCleanYACContext(ctx *runner.StepContext) bool {
	if ctx.GetParamBool("yac_mode", false) {
		return true
	}
	if strings.TrimSpace(ctx.GetParamString("clean_yac_disks", "")) != "" {
		return true
	}
	if yac, _ := probeYACEnvironment(ctx); yac {
		return true
	}
	if v, ok := ctx.Results[resultKeyCleanSourcedYascs].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	return false
}

func cleanDBProcessNamePattern(ctx *runner.StepContext) string {
	if isCleanYACContext(ctx) {
		return `(yasdb|yasagent|yasom|monit|yascs|yascsm)`
	}
	return `(yasdb|yasagent|yasom|monit)`
}

// fillSourcedEnvFromCLI 单节点 YAC 常见：env 仅含 YASCS_HOME，HOME/DATA 由 CLI 提供。
func fillSourcedEnvFromCLI(ctx *runner.StepContext, vars *sourcedDBEnv) {
	if strings.TrimSpace(vars.YASDBHome) == "" {
		vars.YASDBHome = strings.TrimSpace(ctx.GetParamString("yasdb_home", ""))
	}
	if strings.TrimSpace(vars.YASDBData) == "" {
		vars.YASDBData = strings.TrimSpace(ctx.GetParamString("yasdb_data", ""))
	}
	if strings.TrimSpace(vars.YASCSHome) == "" {
		vars.YASCSHome = inferYASCSHome(ctx, vars.YASDBData)
	}
}

func inferYASCSHome(ctx *runner.StepContext, dataRoot string) string {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" {
		dataRoot = strings.TrimSpace(ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data"))
	}
	ycsRoot := path.Join(dataRoot, "ycs")
	ycsQ := commonos.ShellSingleQuote(ycsRoot)
	cmd := fmt.Sprintf(`ls -1 %s 2>/dev/null | head -1`, ycsQ)
	result, _ := ctx.Execute(cmd, false)
	if result == nil || result.GetExitCode() != 0 {
		return ""
	}
	name := strings.TrimSpace(result.GetStdout())
	if name == "" {
		return ""
	}
	return path.Join(ycsRoot, name)
}

// discoverYACDiskPathsFromYFS 单节点 / ycsctl 不可用时，从 /dev/yfs 枚举共享盘（与 db C-001 一致）。
func discoverYACDiskPathsFromYFS(ctx *runner.StepContext, root string) ([]string, error) {
	root = strings.TrimRight(strings.TrimSpace(root), "/")
	if root == "" {
		root = cleanYFSDiscoverRoot
	}
	rootQ := commonos.ShellSingleQuote(root)
	result, err := ctx.Execute(fmt.Sprintf(`ls -1 %s 2>/dev/null || true`, rootQ), false)
	if err != nil {
		return nil, err
	}
	var names []string
	if result != nil && result.GetStdout() != "" {
		for _, line := range strings.Split(result.GetStdout(), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				names = append(names, line)
			}
		}
	}
	var paths []string
	for _, name := range names {
		if cleanReAliasSys.MatchString(name) || cleanReAliasData.MatchString(name) || cleanReAliasArch.MatchString(name) {
			paths = append(paths, path.Join(root, name))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no sys*/data*/arch* disks under %s", root)
	}
	return paths, nil
}

func appendYACPathPatterns(ctx *runner.StepContext, grepFe []string, yasdbData string) []string {
	if v, ok := ctx.Results[resultKeyCleanSourcedYascs].(string); ok && strings.TrimSpace(v) != "" {
		if pat := PathLiteralPrefixForPS(v); pat != "" {
			grepFe = append(grepFe, "-e "+commonos.ShellSingleQuote(pat))
		}
	}
	dataRoot := strings.TrimSpace(yasdbData)
	if dataRoot == "" {
		dataRoot = strings.TrimSpace(ctx.GetParamString("yasdb_data", ""))
	}
	if dataRoot != "" {
		ycsRoot := path.Join(dataRoot, "ycs")
		if pat := PathLiteralPrefixForPS(ycsRoot); pat != "" {
			grepFe = append(grepFe, "-e "+commonos.ShellSingleQuote(pat))
		}
	}
	return grepFe
}

func removeCleanYACExtraDirs(ctx *runner.StepContext) {
	if !isCleanYACContext(ctx) {
		return
	}
	if yascs, ok := ctx.Results[resultKeyCleanSourcedYascs].(string); ok && strings.TrimSpace(yascs) != "" {
		removeDir(ctx, strings.TrimSpace(yascs), "YASCS_HOME")
	}
	paramData := path.Clean(strings.ReplaceAll(ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data"), `\`, `/`))
	if paramData != "" {
		removeDir(ctx, path.Join(paramData, "ycs"), "YAC ycs root")
	}
}
