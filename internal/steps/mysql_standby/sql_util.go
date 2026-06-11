package mysql_standby

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// queryMysqlSQL runs mysql client via Execute (no LogErrorExit on failure).
func queryMysqlSQL(ctx *runner.StepContext, layout commonmysql.Layout, password, sql string) (string, error) {
	cmd := buildMysqlClientCmd(ctx, layout, password, sql)
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return "", err
	}
	if res != nil && res.GetExitCode() != 0 {
		errMsg := strings.TrimSpace(res.GetStderr())
		if errMsg == "" {
			errMsg = strings.TrimSpace(res.GetStdout())
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("exit code %d", res.GetExitCode())
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	if res == nil {
		return "", nil
	}
	return strings.TrimSpace(res.GetStdout()), nil
}

func buildMysqlClientCmd(ctx *runner.StepContext, layout commonmysql.Layout, password, sql string) string {
	port := layout.Port
	socket := layout.Other + "/mysql.sock"
	platform := ctx.GetTargetPlatform()
	mysqlBin, err := commonmysql.ResolveMysqlToolBin(ctx, layout, "mysql")
	if err != nil {
		return fmt.Sprintf("echo %s", commonos.ShellSingleQuote(err.Error()))
	}
	mysqlBinQ := commonos.ShellSingleQuote(mysqlBin)
	socketQ := commonos.ShellSingleQuote(socket)

	if platform == "windows" {
		hostPort := fmt.Sprintf("--host=127.0.0.1 --port=%d", port)
		sqlQ := commonos.PowerShellSingleQuote(strings.ReplaceAll(sql, "\n", " "))
		if password != "" {
			return fmt.Sprintf(`powershell -NoProfile -Command "$env:MYSQL_PWD='%s'; & %s %s -uroot -e %s"`,
				strings.ReplaceAll(password, `'`, `''`), mysqlBinQ, hostPort, sqlQ)
		}
		return fmt.Sprintf(`powershell -NoProfile -Command "& %s %s -uroot -e %s"`, mysqlBinQ, hostPort, sqlQ)
	}
	sqlQ := commonos.ShellSingleQuote(sql)
	if password != "" {
		return fmt.Sprintf("MYSQL_PWD=%s %s --no-defaults -S %s -uroot -e %s",
			commonos.ShellSingleQuote(password), mysqlBinQ, socketQ, sqlQ)
	}
	return fmt.Sprintf("%s --no-defaults -S %s -uroot -e %s", mysqlBinQ, socketQ, sqlQ)
}

func queryPrimarySQL(ctx *runner.StepContext, sql string) (string, error) {
	return queryMysqlSQL(ctx, primaryLayout(ctx), primaryRootPassword(ctx), sql)
}

func executePrimarySQL(ctx *runner.StepContext, sql string) error {
	return commonsql.ExecuteMysqlSQL(ctx, primaryLayout(ctx), primaryRootPassword(ctx), sql)
}

func mysqlPluginActive(ctx *runner.StepContext, layout commonmysql.Layout, password, pluginName string) (bool, error) {
	sql := fmt.Sprintf("SELECT PLUGIN_STATUS FROM INFORMATION_SCHEMA.PLUGINS WHERE PLUGIN_NAME='%s'",
		commonsql.EscapeSQLString(pluginName))
	out, err := queryMysqlSQL(ctx, layout, password, sql)
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToUpper(out), "ACTIVE"), nil
}

func ensureClonePlugin(ctx *runner.StepContext, layout commonmysql.Layout, password string) error {
	active, err := mysqlPluginActive(ctx, layout, password, "clone")
	if err != nil {
		return fmt.Errorf("check clone plugin: %w", err)
	}
	if active {
		ctx.Logger.Info("clone plugin already active")
		return nil
	}
	sql := commonmysql.ClonePluginSQL(ctx.GetTargetPlatform())
	return commonsql.ExecuteMysqlSQL(ctx, layout, password, sql)
}

func mysqldumpReplicationFlags(version string) string {
	if commonmysql.UsesReplicationSourceSyntax(version) {
		return "--source-data=2"
	}
	return "--master-data=2"
}

func buildMysqldumpCmd(ctx *runner.StepContext, layout commonmysql.Layout, remoteHost string, remotePort int, user, password, dumpPath, version string) string {
	// --all-databases: all DBs + tables + views (needs SHOW VIEW)
	// --routines: stored procedures & functions (off by default in 8.0+)
	// --events: scheduled events (off by default)
	// --triggers: table triggers (on by default; kept explicit)
	flags := "--single-transaction --set-gtid-purged=ON --all-databases --routines --events --triggers " + mysqldumpReplicationFlags(version)
	if remotePort <= 0 {
		remotePort = layout.Port
	}
	host := strings.TrimSpace(remoteHost)
	if host == "" {
		host = "127.0.0.1"
	}
	user = strings.TrimSpace(user)
	if user == "" {
		user = "root"
	}
	platform := ctx.GetTargetPlatform()
	dumpBin, err := commonmysql.ResolveMysqlToolBin(ctx, layout, "mysqldump")
	if err != nil {
		return fmt.Sprintf("echo %s", commonos.ShellSingleQuote(err.Error()))
	}
	dumpBinQ := commonos.ShellSingleQuote(dumpBin)
	userQ := commonos.ShellSingleQuote(user)

	if platform == "windows" {
		dumpWin := strings.ReplaceAll(dumpPath, `\`, `/`)
		hostPort := fmt.Sprintf("-h%s -P%d", host, remotePort)
		dumpQ := strings.ReplaceAll(dumpWin, `'`, `''`)
		userWin := strings.ReplaceAll(user, `'`, `''`)
		if password != "" {
			return fmt.Sprintf(`powershell -NoProfile -Command "$env:MYSQL_PWD='%s'; & %s %s -u%s %s > '%s'"`,
				strings.ReplaceAll(password, `'`, `''`), dumpBinQ, hostPort, userWin, flags, dumpQ)
		}
		return fmt.Sprintf(`powershell -NoProfile -Command "& %s %s -u%s %s > '%s'"`,
			dumpBinQ, hostPort, userWin, flags, dumpQ)
	}

	if password != "" {
		return fmt.Sprintf("MYSQL_PWD=%s %s --no-defaults -h %s -P%d -u%s %s > %s",
			commonos.ShellSingleQuote(password), dumpBinQ, commonos.ShellSingleQuote(host), remotePort, userQ, flags, shellQuote(dumpPath))
	}
	return fmt.Sprintf("%s --no-defaults -h %s -P%d -u%s %s > %s",
		dumpBinQ, commonos.ShellSingleQuote(host), remotePort, userQ, flags, shellQuote(dumpPath))
}

func runMysqldump(ctx *runner.StepContext, layout commonmysql.Layout, remoteHost string, remotePort int, user, password, dumpPath string) error {
	version := ctx.GetParamString("primary_mysql_version", "")
	if version == "" {
		if v, ok := ctx.Results["primary_mysql_version"].(string); ok {
			version = v
		}
	}
	if version == "" {
		version = ctx.GetParamString("mysql_version", "8.0.46")
	}
	cmd := buildMysqldumpCmd(ctx, layout, remoteHost, remotePort, user, password, dumpPath, version)
	ctx.LogScriptPreview("shell", "mysqldump", cmd)
	res, err := ctx.ExecuteWithCheck(cmd, false)
	if err != nil {
		return err
	}
	if res != nil && res.GetExitCode() != 0 {
		return fmt.Errorf("mysqldump failed with exit code %d", res.GetExitCode())
	}
	return nil
}

func dumpFileFromContext(ctx *runner.StepContext) string {
	if p := strings.TrimSpace(ctx.GetParamString("dump_file", "")); p != "" {
		return p
	}
	if v, ok := ctx.Results["dump_file"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return ""
}

func remoteFileSize(ctx *runner.StepContext, path string) (int64, error) {
	if ctx.GetTargetPlatform() == commonmysql.PlatformWindows {
		winPath := strings.ReplaceAll(path, `\`, `/`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "(Get-Item -LiteralPath '%s').Length"`,
			strings.ReplaceAll(winPath, `'`, `''`))
		res, err := ctx.Execute(cmd, false)
		if err != nil {
			return 0, err
		}
		if res != nil && res.GetExitCode() != 0 {
			return 0, fmt.Errorf("stat %s failed", path)
		}
		return strconv.ParseInt(strings.TrimSpace(res.GetStdout()), 10, 64)
	}
	cmd := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s 2>/dev/null", shellQuote(path), shellQuote(path))
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return 0, err
	}
	if res != nil && res.GetExitCode() != 0 {
		return 0, fmt.Errorf("stat %s failed", path)
	}
	return strconv.ParseInt(strings.TrimSpace(res.GetStdout()), 10, 64)
}

func formatByteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

const (
	defaultCloneTimeoutSec      = 0    // unlimited (see cloneDeadline)
	defaultCloneReadyTimeoutSec = 3600 // 1h for InnoDB recovery after restart
	clonePollInterval           = 15 * time.Second
)

type CloneStatusRow struct {
	State        string
	ErrorNo      string
	ErrorMessage string
}

type CloneProgressRow struct {
	Stage string
	State string
	Data  string
}

func cloneOpTimeout(ctx *runner.StepContext) time.Duration {
	sec := ctx.GetParamInt("clone_timeout", defaultCloneTimeoutSec)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func cloneReadyTimeout(ctx *runner.StepContext) time.Duration {
	sec := ctx.GetParamInt("clone_ready_timeout", defaultCloneReadyTimeoutSec)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func cloneDeadline(from time.Time, timeout time.Duration) time.Time {
	if timeout <= 0 {
		return from.Add(7 * 24 * time.Hour)
	}
	return from.Add(timeout)
}

func parseCloneStatus(out string) (CloneStatusRow, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := 1; i < len(lines); i++ {
		fields := strings.Split(lines[i], "\t")
		if len(fields) < 1 {
			continue
		}
		row := CloneStatusRow{State: strings.TrimSpace(fields[0])}
		if len(fields) > 1 {
			row.ErrorNo = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			row.ErrorMessage = strings.TrimSpace(fields[2])
		}
		if row.State != "" {
			return row, true
		}
	}
	return CloneStatusRow{}, false
}

func parseCloneProgress(out string) (CloneProgressRow, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := 1; i < len(lines); i++ {
		fields := strings.Split(lines[i], "\t")
		if len(fields) < 2 {
			continue
		}
		row := CloneProgressRow{
			Stage: strings.TrimSpace(fields[0]),
			State: strings.TrimSpace(fields[1]),
		}
		if len(fields) > 2 {
			row.Data = strings.TrimSpace(fields[2])
		}
		if row.Stage != "" {
			return row, true
		}
	}
	return CloneProgressRow{}, false
}

func queryCloneStatus(ctx *runner.StepContext, layout commonmysql.Layout, password string) (CloneStatusRow, error) {
	sql := "SELECT STATE, IFNULL(ERROR_NO, 0), IFNULL(ERROR_MESSAGE, '') FROM performance_schema.clone_status LIMIT 1"
	out, err := queryMysqlSQL(ctx, layout, password, sql)
	if err != nil {
		return CloneStatusRow{}, err
	}
	if row, ok := parseCloneStatus(out); ok {
		return row, nil
	}
	return CloneStatusRow{}, nil
}

func queryCloneProgress(ctx *runner.StepContext, layout commonmysql.Layout, password string) (CloneProgressRow, error) {
	sql := `SELECT STAGE, STATE, IFNULL(DATA, 0)
FROM performance_schema.clone_progress
WHERE END_TIME IS NULL
ORDER BY BEGIN_TIME DESC
LIMIT 1`
	out, err := queryMysqlSQL(ctx, layout, password, sql)
	if err != nil {
		return CloneProgressRow{}, err
	}
	if row, ok := parseCloneProgress(out); ok {
		return row, nil
	}
	return CloneProgressRow{}, nil
}

func formatCloneProgressLog(stage, state, data string) string {
	if data == "" || data == "0" {
		return fmt.Sprintf("clone progress: stage=%s state=%s", stage, state)
	}
	if n, err := strconv.ParseInt(data, 10, 64); err == nil && n > 0 {
		gb := float64(n) / (1024 * 1024 * 1024)
		return fmt.Sprintf("clone progress: stage=%s state=%s data=%.2f GiB", stage, state, gb)
	}
	return fmt.Sprintf("clone progress: stage=%s state=%s data=%s bytes", stage, state, data)
}

func isBenignCloneClientError(lastState, stdout, stderr string, exitCode int) bool {
	if exitCode == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(lastState)) {
	case "completed", "restart":
		return true
	}
	combined := stdout + "\n" + stderr
	return strings.Contains(combined, "Lost connection") ||
		strings.Contains(combined, "Can't connect") ||
		strings.Contains(combined, "2013") ||
		strings.Contains(combined, "2006")
}

func cloneClientError(lastState, stdout, stderr string, exitCode int) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = strings.TrimSpace(stdout)
	}
	if msg == "" {
		msg = fmt.Sprintf("mysql client exit code %d", exitCode)
	}
	if lastState != "" {
		return fmt.Errorf("clone client failed (clone_status=%s): %s", lastState, msg)
	}
	return fmt.Errorf("clone client failed: %s", msg)
}

type cloneClientResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

// runCloneWithMonitor starts CLONE in a concurrent session and polls performance_schema
// so large transfers can run for hours while failures surface early.
func runCloneWithMonitor(ctx *runner.StepContext, layout commonmysql.Layout, password, cloneSQL string) error {
	opTimeout := cloneOpTimeout(ctx)
	readyTimeout := cloneReadyTimeout(ctx)
	if readyTimeout <= 0 {
		readyTimeout = time.Duration(defaultCloneReadyTimeoutSec) * time.Second
	}
	standbyLogPhase(ctx, "clone-monitor-start",
		fmt.Sprintf("clone_timeout=%s clone_ready_timeout=%s poll=%s",
			formatDurationParam(opTimeout), formatDurationParam(readyTimeout), clonePollInterval))

	done := make(chan cloneClientResult, 1)
	go func() {
		cmd := buildMysqlClientCmd(ctx, layout, password, cloneSQL)
		res, err := ctx.Execute(cmd, false)
		cr := cloneClientResult{err: err}
		if res != nil {
			cr.stdout = res.GetStdout()
			cr.stderr = res.GetStderr()
			cr.exitCode = res.GetExitCode()
		}
		done <- cr
	}()

	start := time.Now()
	deadline := cloneDeadline(start, opTimeout)
	ticker := time.NewTicker(clonePollInterval)
	defer ticker.Stop()

	var (
		lastState     string
		lastProgress  string
		clientDone    bool
		clientResult  cloneClientResult
		statusMissing int
	)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("clone timed out after %s (last clone_status=%q last_progress=%q)",
				time.Since(start).Round(time.Second), lastState, lastProgress)
		}

		select {
		case clientResult = <-done:
			clientDone = true
		default:
		}

		st, stErr := queryCloneStatus(ctx, layout, password)
		if stErr == nil && st.State != "" {
			statusMissing = 0
			lastState = st.State
			if strings.EqualFold(st.State, "Failed") {
				if st.ErrorMessage != "" {
					return fmt.Errorf("clone failed: %s (errno=%s)", st.ErrorMessage, st.ErrorNo)
				}
				return fmt.Errorf("clone failed (errno=%s)", st.ErrorNo)
			}
		} else if stErr != nil {
			statusMissing++
			standbyLogPhase(ctx, "clone-poll", fmt.Sprintf("clone_status unavailable: %v", stErr))
		}

		if prog, progErr := queryCloneProgress(ctx, layout, password); progErr == nil && prog.Stage != "" {
			line := formatCloneProgressLog(prog.Stage, prog.State, prog.Data)
			if line != lastProgress {
				lastProgress = line
				standbyLogPhase(ctx, "clone-progress", line)
				ctx.Logger.Info("%s", line)
			}
		}

		if strings.EqualFold(lastState, "Completed") {
			break
		}
		if clientDone && (stErr != nil || statusMissing > 0) {
			break
		}
		if clientDone && isBenignCloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode) {
			break
		}
		if clientDone && !isBenignCloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode) {
			return cloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode)
		}

		select {
		case clientResult = <-done:
			clientDone = true
			if !isBenignCloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode) {
				return cloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode)
			}
		case <-ticker.C:
		}
	}

	if !clientDone {
		select {
		case clientResult = <-done:
			clientDone = true
			if !isBenignCloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode) {
				return cloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode)
			}
		case <-time.After(2 * time.Minute):
		}
	}

	if st, err := queryCloneStatus(ctx, layout, password); err == nil && st.State != "" {
		lastState = st.State
		if strings.EqualFold(st.State, "Failed") {
			if st.ErrorMessage != "" {
				return fmt.Errorf("clone failed: %s (errno=%s)", st.ErrorMessage, st.ErrorNo)
			}
			return fmt.Errorf("clone failed (errno=%s)", st.ErrorNo)
		}
	} else if clientDone && !isBenignCloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode) {
		return cloneClientError(lastState, clientResult.stdout, clientResult.stderr, clientResult.exitCode)
	}

	standbyLogPhase(ctx, "clone-done", fmt.Sprintf("clone finished (status=%s), waiting for mysqld recovery", lastState))
	return mysqlsteps.WaitForMysqlReady(ctx, layout, readyTimeout, password)
}

func formatDurationParam(d time.Duration) string {
	if d <= 0 {
		return "unlimited"
	}
	return d.String()
}
