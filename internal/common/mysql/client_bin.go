package mysql

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yinstall/internal/runner"
)

var mysqlClientVersionRE = regexp.MustCompile(`(?i)Ver\s+(\d+\.\d+\.\d+)`)

// MysqlHomeFromToolBin derives MYSQL_HOME from a mysql/mysqldump binary path (.../bin/mysql).
func MysqlHomeFromToolBin(toolBin, platform string) string {
	toolBin = filepath.ToSlash(strings.TrimSpace(toolBin))
	if toolBin == "" {
		return ""
	}
	lower := strings.ToLower(toolBin)
	if platform == PlatformWindows {
		if strings.HasSuffix(lower, "/bin/mysql.exe") {
			return strings.TrimSuffix(toolBin, "/bin/mysql.exe")
		}
		if strings.HasSuffix(lower, "/bin/mysqldump.exe") {
			return strings.TrimSuffix(toolBin, "/bin/mysqldump.exe")
		}
	} else {
		if strings.HasSuffix(toolBin, "/bin/mysql") {
			return strings.TrimSuffix(toolBin, "/bin/mysql")
		}
		if strings.HasSuffix(toolBin, "/bin/mysqldump") {
			return strings.TrimSuffix(toolBin, "/bin/mysqldump")
		}
	}
	dir := filepath.Dir(toolBin)
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir)
	}
	return dir
}

// ParseMysqlClientVersionOutput extracts semver from `mysql --version` stdout.
func ParseMysqlClientVersionOutput(out string) (string, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("empty mysql --version output")
	}
	if m := mysqlClientVersionRE.FindStringSubmatch(out); len(m) > 1 {
		return m[1], nil
	}
	return "", fmt.Errorf("cannot parse mysql client version from %q", out)
}

// QueryMysqlClientVersion runs `mysql --version` and returns parsed semver.
func QueryMysqlClientVersion(ctx *runner.StepContext, mysqlBin, platform string) (string, error) {
	if strings.TrimSpace(mysqlBin) == "" {
		return "", fmt.Errorf("empty mysql binary path")
	}
	var cmd string
	if platform == PlatformWindows {
		winBin := strings.ReplaceAll(mysqlBin, `\`, `/`)
		cmd = fmt.Sprintf(`powershell -NoProfile -Command "& '%s' --version"`, strings.ReplaceAll(winBin, `'`, `''`))
	} else {
		cmd = fmt.Sprintf("%s --version 2>&1", shellSingleQuote(mysqlBin))
	}
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return "", fmt.Errorf("mysql --version: %w", err)
	}
	if res == nil || res.GetExitCode() != 0 {
		msg := strings.TrimSpace(res.GetStderr())
		if msg == "" && res != nil {
			msg = strings.TrimSpace(res.GetStdout())
		}
		if msg == "" {
			msg = "mysql --version failed"
		}
		return "", fmt.Errorf("%s", msg)
	}
	out := res.GetStdout()
	if strings.TrimSpace(out) == "" {
		out = res.GetStderr()
	}
	return ParseMysqlClientVersionOutput(out)
}

// DetectInstalledSoftwareViaClient locates mysql on PATH/--mysql-home, reads client version,
// and confirms mysqld exists under the same home directory.
func DetectInstalledSoftwareViaClient(ctx *runner.StepContext, layout Layout) (version, home string, ok bool, err error) {
	mysqlBin, err := ResolveMysqlToolBin(ctx, layout, "mysql")
	if err != nil {
		return "", "", false, nil
	}
	platform := ctx.GetTargetPlatform()
	home = MysqlHomeFromToolBin(mysqlBin, platform)
	if home == "" {
		return "", "", false, nil
	}
	if !MysqldExistsAtHome(ctx, home, platform) {
		return "", "", false, nil
	}
	version, err = QueryMysqlClientVersion(ctx, mysqlBin, platform)
	if err != nil {
		return "", "", false, err
	}
	return version, home, true, nil
}

// MysqldExistsAtHome reports whether mysqld exists under home/bin.
func MysqldExistsAtHome(ctx *runner.StepContext, home, platform string) bool {
	if strings.TrimSpace(home) == "" {
		return false
	}
	tool := joinMysqlToolPath(home, "mysqld", platform)
	return mysqlToolExists(ctx, tool, platform)
}

// ResolveMysqlToolBin locates mysql/mysqldump/mysqld on the target host.
// Priority: --mysql-home → layout.Home → PATH (command -v).
func ResolveMysqlToolBin(ctx *runner.StepContext, layout Layout, tool string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil step context")
	}
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "", fmt.Errorf("empty mysql tool name")
	}
	cacheKey := "mysql_tool_bin_" + tool
	if ctx.Results != nil {
		if v, ok := ctx.Results[cacheKey].(string); ok && strings.TrimSpace(v) != "" {
			return v, nil
		}
	}

	platform := ctx.GetTargetPlatform()
	var candidates []string
	if home := strings.TrimSpace(ctx.GetParamString("mysql_home", "")); home != "" {
		candidates = append(candidates, joinMysqlToolPath(home, tool, platform))
	}
	if layout.Home != "" {
		p := joinMysqlToolPath(layout.Home, tool, platform)
		if !containsPath(candidates, p) {
			candidates = append(candidates, p)
		}
	}
	for _, c := range candidates {
		if mysqlToolExists(ctx, c, platform) {
			cacheToolBin(ctx, cacheKey, c)
			return c, nil
		}
	}

	pathBin, err := lookupMysqlToolInPath(ctx, tool, platform)
	if err != nil {
		return "", err
	}
	cacheToolBin(ctx, cacheKey, pathBin)
	return pathBin, nil
}

func joinMysqlToolPath(home, tool, platform string) string {
	home = strings.TrimRight(strings.ReplaceAll(home, `\`, `/`), "/")
	if platform == PlatformWindows {
		return home + "/bin/" + tool + ".exe"
	}
	return home + "/bin/" + tool
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func cacheToolBin(ctx *runner.StepContext, key, path string) {
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}
	ctx.Results[key] = path
}

func mysqlToolExists(ctx *runner.StepContext, path, platform string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if platform == PlatformWindows {
		winPath := strings.ReplaceAll(path, `\`, `/`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Test-Path -LiteralPath '%s'"`,
			strings.ReplaceAll(winPath, `'`, `''`))
		res, _ := ctx.Execute(cmd, false)
		return res != nil && res.GetExitCode() == 0 && strings.EqualFold(strings.TrimSpace(res.GetStdout()), "True")
	}
	cmd := fmt.Sprintf("test -x %s", shellSingleQuote(path))
	res, _ := ctx.Execute(cmd, false)
	return res != nil && res.GetExitCode() == 0
}

func lookupMysqlToolInPath(ctx *runner.StepContext, tool, platform string) (string, error) {
	var cmd string
	if platform == PlatformWindows {
		cmd = fmt.Sprintf(`powershell -NoProfile -Command "(Get-Command %s -ErrorAction SilentlyContinue).Source"`, tool)
	} else {
		cmd = fmt.Sprintf("command -v %s 2>/dev/null || which %s 2>/dev/null", tool, tool)
	}
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return "", fmt.Errorf("lookup %s in PATH: %w", tool, err)
	}
	if res == nil || res.GetExitCode() != 0 {
		return "", fmt.Errorf("%s not found in PATH; set --mysql-home or ensure mysql client is installed", tool)
	}
	bin := strings.TrimSpace(res.GetStdout())
	if bin == "" {
		return "", fmt.Errorf("%s not found in PATH; set --mysql-home or ensure mysql client is installed", tool)
	}
	return bin, nil
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
