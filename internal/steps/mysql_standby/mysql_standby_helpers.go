package mysql_standby

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

func primaryPort(ctx *runner.StepContext) int {
	if v, ok := ctx.Params["primary_port"].(int); ok && v > 0 {
		return v
	}
	return ctx.GetParamInt("primary_port", 3306)
}

func replicaPort(ctx *runner.StepContext) int {
	if v, ok := ctx.Params["replica_port"].(int); ok && v > 0 {
		return v
	}
	return ctx.GetParamInt("replica_port", 0)
}

func primaryRootPassword(ctx *runner.StepContext) string {
	return ctx.GetParamString("primary_root_password", "")
}

func readRemoteFile(ctx *runner.StepContext, path string) (string, error) {
	if ctx.GetTargetPlatform() == commonmysql.PlatformWindows {
		winPath := strings.ReplaceAll(path, `\`, `/`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Get-Content -LiteralPath '%s' -Raw"`,
			strings.ReplaceAll(winPath, `'`, `''`))
		res, err := ctx.Execute(cmd, false)
		if err != nil {
			return "", err
		}
		if res.GetExitCode() != 0 {
			return "", fmt.Errorf("failed to read %s", path)
		}
		return res.GetStdout(), nil
	}
	cmd := fmt.Sprintf("cat %s 2>/dev/null", shellQuote(path))
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return "", err
	}
	if res.GetExitCode() != 0 {
		return "", fmt.Errorf("failed to read %s", path)
	}
	return res.GetStdout(), nil
}

func writeRemoteFile(ctx *runner.StepContext, path, content string) error {
	ctx.LogScriptPreview("file", path, content)
	if ctx.GetTargetPlatform() == commonmysql.PlatformWindows {
		if err := commonfile.RemoteEnsureDir(ctx, filepath.Dir(strings.ReplaceAll(path, `\`, `/`)), false); err != nil {
			return err
		}
		return commonfile.RemoteWriteTextFile(ctx, path, content, false)
	}
	dir := filepath.Dir(path)
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s << 'EOF'\n%sEOF", shellQuote(dir), shellQuote(path), content)
	useSudo := ctx.GetParamBool("sudo", false) && !ctx.GetParamBool("local_mode", false)
	_, err := ctx.ExecuteWithCheck(cmd, useSudo)
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func parseSQLScalar(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) >= 2 {
		return strings.TrimSpace(lines[1])
	}
	return strings.TrimSpace(out)
}

func serverIDFromSQL(ctx *runner.StepContext) (int, error) {
	out, err := queryPrimarySQL(ctx, "SELECT @@server_id")
	if err != nil {
		return 0, err
	}
	out = strings.TrimSpace(out)
	lines := strings.Split(out, "\n")
	if len(lines) >= 2 {
		out = strings.TrimSpace(lines[1])
	}
	return strconv.Atoi(out)
}

func syncMethod(ctx *runner.StepContext) string {
	return strings.ToLower(ctx.GetParamString("sync_method", "clone"))
}

func standbyStage(ctx *runner.StepContext) string {
	if s := ctx.GetParamString("standby_stage", ""); s != "" {
		return s
	}
	if s := ctx.GetParamString("mysql_stage", ""); s != "" {
		return s
	}
	return commonmysql.DefaultStandbyStage()
}

func skipUnlessStandbyReplicationStage(ctx *runner.StepContext) error {
	if !commonmysql.StandbyIncludesReplicationSetup(standbyStage(ctx)) {
		return runner.NewStepSkippedError(fmt.Sprintf("standby stage %q does not configure replication", standbyStage(ctx)))
	}
	return nil
}

func hostsEquivalent(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if a == b {
		return true
	}
	return isLoopbackHost(a) && isLoopbackHost(b)
}

func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

func repUser(ctx *runner.StepContext) string {
	user := strings.TrimSpace(ctx.GetParamString("rep_user", commonmysql.DefaultReplicationUser))
	if user == "" {
		return commonmysql.DefaultReplicationUser
	}
	return user
}

func repPassword(ctx *runner.StepContext) string {
	return ctx.GetParamString("rep_password", "")
}

func dumpUser(ctx *runner.StepContext) string {
	if u := strings.TrimSpace(ctx.GetParamString("dump_user", "")); u != "" {
		return u
	}
	return repUser(ctx)
}

func dumpUserIsRep(ctx *runner.StepContext) bool {
	return dumpUser(ctx) == repUser(ctx)
}

func dumpPassword(ctx *runner.StepContext) string {
	if dumpUserIsRep(ctx) {
		return repPassword(ctx)
	}
	if p := strings.TrimSpace(ctx.GetParamString("dump_password", "")); p != "" {
		return p
	}
	return repPassword(ctx)
}

func replicaHosts(ctx *runner.StepContext) []string {
	if v, ok := ctx.Params["replica_hosts"].([]string); ok && len(v) > 0 {
		return v
	}
	if v, ok := ctx.Params["replica_hosts"].([]interface{}); ok && len(v) > 0 {
		var hosts []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				hosts = append(hosts, strings.TrimSpace(s))
			}
		}
		return hosts
	}
	return nil
}

func channelName(ctx *runner.StepContext) string {
	host := ctx.GetParamString("primary_host", "")
	return commonmysql.ChannelName(host, primaryPort(ctx), ctx.GetParamString("channel_name", ""))
}

func primaryHost(ctx *runner.StepContext) string {
	return ctx.GetParamString("primary_host", "")
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
