// yasql.go - YashanDB SQL 执行公共函数
// 提供 yasql 命令执行的通用逻辑，支持多种连接方式

package sql

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// yasErrorCodePattern 匹配 YashanDB yasql 输出中的错误码 YAS-NNNNN（N 为数字）。
var yasErrorCodePattern = regexp.MustCompile(`YAS-\d{5}`)

// OutputContainsYasError 判断合并输出（stdout+stderr）中是否出现 Yashan 报错关键字 YAS-NNNNN。
func OutputContainsYasError(output string) bool {
	return yasErrorCodePattern.MatchString(output)
}

// YasqlErrPDBAlreadyOpen is returned when ALTER OPEN runs against an already-open PDB.
const YasqlErrPDBAlreadyOpen = "YAS-02882"

func ignoreYasCodesSet(codes []string) map[string]bool {
	if len(codes) == 0 {
		return nil
	}
	out := make(map[string]bool, len(codes))
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c != "" {
			out[c] = true
		}
	}
	return out
}

func errIfYasqlOutputHasError(r *YasqlResult, ignoreCodes map[string]bool) error {
	if r == nil {
		return nil
	}
	combined := r.Stdout + r.Stderr
	if !OutputContainsYasError(combined) {
		return nil
	}
	code := yasErrorCodePattern.FindString(combined)
	if code != "" && ignoreCodes != nil && ignoreCodes[code] {
		return nil
	}
	if code == "" {
		code = "YAS-NNNNN"
	}
	return fmt.Errorf("yasql output contains Yashan error %s", code)
}

// ReportSQLFailure 将 yasql 失败输出为终端 LogErrorExit 块（供调用方在确定非可恢复失败后调用）。
func ReportSQLFailure(ctx *runner.StepContext, cmd string, r *YasqlResult) {
	if ctx == nil || r == nil {
		return
	}
	errMsg := strings.TrimSpace(r.Stderr)
	if errMsg == "" {
		errMsg = strings.TrimSpace(r.Stdout)
	}
	if errMsg == "" {
		errMsg = fmt.Sprintf("exit code %d", r.ExitCode)
	}
	ctx.Logger.LogErrorExit(
		ctx.Executor.Host(),
		ctx.CurrentStepID,
		"",
		cmd,
		r.Stdout,
		r.Stderr,
		r.ExitCode,
		errMsg,
	)
}

// ValidateYasqlResultSuccess 校验 yasql 结果：先检查 YAS-NNNNN，再检查退出码（供直连 ctx.Execute yasql 的路径统一使用）。
func ValidateYasqlResultSuccess(r *YasqlResult) error {
	return ValidateYasqlResultSuccessIgnore(r)
}

// ValidateYasqlResultSuccessIgnore 校验 yasql 结果，ignoreYasCodes 中的 YAS 错误码视为可忽略。
func ValidateYasqlResultSuccessIgnore(r *YasqlResult, ignoreYasCodes ...string) error {
	if r == nil {
		return fmt.Errorf("yasql result is nil")
	}
	if err := errIfYasqlOutputHasError(r, ignoreYasCodesSet(ignoreYasCodes)); err != nil {
		return err
	}
	if !r.Success || r.ExitCode != 0 {
		return fmt.Errorf("yasql command failed with exit code %d: %s", r.ExitCode, r.Stderr)
	}
	return nil
}

// buildInstallLayoutEnvPrefix 在安装介质目录尚未写入用户 env 时推导 YASDB_HOME 等（与 C-023 历史布局一致）。
func buildInstallLayoutEnvPrefix(installPath, dataPath string) string {
	return fmt.Sprintf(
		`export YASDB_HOME=%s/$(ls %s/ 2>/dev/null | head -1) && `+
			`export YASCS_HOME=%s/ycs/ce-1-1 && `+
			`export PATH=$YASDB_HOME/bin:$PATH && `+
			`export LD_LIBRARY_PATH=$YASDB_HOME/lib:$LD_LIBRARY_PATH`,
		installPath, installPath, dataPath)
}

func ensureSQLFileContent(sql string) string {
	s := strings.TrimSpace(sql)
	if s == "" {
		return ";\n"
	}
	if !strings.HasSuffix(s, ";") {
		return s + ";\n"
	}
	return s + "\n"
}

func buildYasqlConnArg(cfg *YasqlConfig) (string, error) {
	if cfg.AsSysdba {
		return "/ as sysdba", nil
	}
	if cfg.User != "" && cfg.Password != "" {
		return fmt.Sprintf("%s/%s@%s", cfg.User, commonos.YasqlQuotePassword(cfg.Password), cfg.ClusterName), nil
	}
	return "", fmt.Errorf("either AsSysdba=true or User/Password must be provided")
}

func yasqlResultFromExec(result runner.ExecResult, execErr error, ignoreYasCodes ...string) (*YasqlResult, error) {
	yasqlResult := &YasqlResult{Success: false}
	if result != nil {
		yasqlResult.Stdout = result.GetStdout()
		yasqlResult.Stderr = result.GetStderr()
		yasqlResult.ExitCode = result.GetExitCode()
		yasqlResult.Success = result.GetExitCode() == 0
	}
	if execErr != nil {
		return yasqlResult, fmt.Errorf("failed to execute yasql: %w", execErr)
	}
	if err := ValidateYasqlResultSuccessIgnore(yasqlResult, ignoreYasCodes...); err != nil {
		yasqlResult.Success = false
		return yasqlResult, err
	}
	return yasqlResult, nil
}

// executeYasqlViaRemoteFile 将 SQL 上传到目标机后用 yasql -f 执行（与 collectRunSQL 对齐）。
func executeYasqlViaRemoteFile(ctx *runner.StepContext, cfg *YasqlConfig, sql string) (*YasqlResult, error) {
	if ctx == nil || ctx.Executor == nil {
		return nil, fmt.Errorf("step context and executor are required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("yasql config is required")
	}

	localTmp, err := os.CreateTemp("", "yinstall_sql_*.sql")
	if err != nil {
		return nil, fmt.Errorf("create local tmp sql file: %w", err)
	}
	localName := localTmp.Name()
	defer os.Remove(localName)

	if _, err := localTmp.WriteString(ensureSQLFileContent(sql)); err != nil {
		localTmp.Close()
		return nil, fmt.Errorf("write local tmp sql: %w", err)
	}
	localTmp.Close()

	remotePath := fmt.Sprintf("/tmp/.yinstall_sql_%d.sql", time.Now().UnixNano())
	ctx.LogScriptPreview("sql", "remote="+remotePath, sql)
	if err := ctx.Executor.Upload(localName, remotePath, ctx.UploadContext()); err != nil {
		return nil, fmt.Errorf("upload sql file: %w", err)
	}
	remoteQ := commonos.ShellSingleQuote(remotePath)

	var connStr string
	if s := strings.TrimSpace(cfg.ConnectString); s != "" {
		connStr = s
	} else {
		var err error
		connStr, err = buildYasqlConnArg(cfg)
		if err != nil {
			return nil, err
		}
	}
	yasqlBin := "yasql"
	if cfg.Silent {
		yasqlBin += " -S"
	}
	yasqlCmd := fmt.Sprintf("%s %s -f %s", yasqlBin, connStr, remoteQ)

	var result runner.ExecResult
	var execErr error
	if cfg.EnvFile != "" {
		result, execErr = commonos.ExecuteAsUserWithEnv(ctx, cfg.OSUser, cfg.EnvFile, yasqlCmd, cfg.ShowOutput)
	} else {
		result, execErr = commonos.ExecuteAsUser(ctx, cfg.OSUser, yasqlCmd, cfg.ShowOutput)
	}
	_, _ = ctx.Execute(fmt.Sprintf("rm -f %s", remoteQ), false)
	return yasqlResultFromExec(result, execErr, cfg.IgnoreYasErrorCodes...)
}

// ExecuteSQLAsSysdbaInstallLayoutCtx 使用安装目录布局执行 sysdba SQL（远端 -f），并进行退出码与 YAS-NNNNN 校验。
func ExecuteSQLAsSysdbaInstallLayoutCtx(ctx *runner.StepContext, osUser, installPath, dataPath, sql string, showOutput bool) (*YasqlResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("step context is required")
	}
	if sql == "" {
		return nil, fmt.Errorf("sql statement is required")
	}

	localTmp, err := os.CreateTemp("", "yinstall_sql_*.sql")
	if err != nil {
		return nil, fmt.Errorf("create local tmp sql file: %w", err)
	}
	localName := localTmp.Name()
	defer os.Remove(localName)

	if _, err := localTmp.WriteString(ensureSQLFileContent(sql)); err != nil {
		localTmp.Close()
		return nil, fmt.Errorf("write local tmp sql: %w", err)
	}
	localTmp.Close()

	remotePath := fmt.Sprintf("/tmp/.yinstall_sql_%d.sql", time.Now().UnixNano())
	ctx.LogScriptPreview("sql", "remote="+remotePath, sql)
	if err := ctx.Executor.Upload(localName, remotePath, ctx.UploadContext()); err != nil {
		return nil, fmt.Errorf("upload sql file: %w", err)
	}
	remoteQ := commonos.ShellSingleQuote(remotePath)

	yasqlCmd := fmt.Sprintf("%s && yasql -S / as sysdba -f %s", buildInstallLayoutEnvPrefix(installPath, dataPath), remoteQ)
	result, execErr := commonos.ExecuteAsUser(ctx, osUser, yasqlCmd, showOutput)
	_, _ = ctx.Execute(fmt.Sprintf("rm -f %s", remoteQ), false)
	return yasqlResultFromExec(result, execErr)
}

// YasqlConfig yasql 执行配置
type YasqlConfig struct {
	User                string   // 数据库用户，如 sys
	Password            string   // 数据库密码
	ClusterName         string   // 集群名称
	ConnectString       string   // 若非空，直接作为 yasql 连接串（如 sys/pass@host:1888/PDB1）
	IgnoreYasErrorCodes []string // 校验输出时忽略的 YAS-NNNNN 错误码（如 PDB 已 OPEN 时的 YAS-02882）
	AsSysdba            bool     // 是否使用 as sysdba 连接
	OSUser              string   // 操作系统用户（执行 yasql 命令的用户）
	EnvFile             string   // 环境变量文件路径
	Silent              bool     // 是否静默模式 (-s)
	Quiet               bool     // 是否安静模式 (-q，不显示 banner）
	ShowOutput          bool     // 是否显示命令输出
}

// YasqlResult yasql 执行结果
type YasqlResult struct {
	Stdout   string // 标准输出
	Stderr   string // 标准错误
	ExitCode int    // 退出码
	Success  bool   // 是否成功
}

// ExecuteSQL 执行 SQL 语句
// 使用 yasql 连接数据库并执行 SQL
//
// 参数：
//   - executor: 命令执行器
//   - cfg: yasql 配置
//   - sql: 要执行的 SQL 语句
//
// 返回：
//   - YasqlResult: 执行结果
//   - error: 错误信息
func ExecuteSQL(ctx *runner.StepContext, cfg *YasqlConfig, sql string) (*YasqlResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("yasql config is required")
	}
	if sql == "" {
		return nil, fmt.Errorf("sql statement is required")
	}
	return executeYasqlViaRemoteFile(ctx, cfg, sql)
}

// ExecuteSQLAsSysdba 以 sysdba 身份执行 SQL（便捷函数）
//
// 参数：
//   - executor: 命令执行器
//   - osUser: 操作系统用户
//   - envFile: 环境变量文件路径
//   - clusterName: 集群名称
//   - sql: 要执行的 SQL 语句
//   - showOutput: 是否显示输出
//
// 返回：
//   - YasqlResult: 执行结果
//   - error: 错误信息
func ExecuteSQLAsSysdba(ctx *runner.StepContext, osUser, envFile, clusterName, sql string, showOutput bool) (*YasqlResult, error) {
	cfg := &YasqlConfig{
		ClusterName: clusterName,
		AsSysdba:    true,
		OSUser:      osUser,
		EnvFile:     envFile,
		Quiet:       true,
		Silent:      true,
		ShowOutput:  showOutput,
	}
	return ExecuteSQL(ctx, cfg, sql)
}

// ExecuteSQLAsSysdbaCtx 以 sysdba 身份执行 SQL（带 StepContext，支持日志记录）。
//
// 参数：
//   - ctx: 步骤上下文
//   - osUser: 操作系统用户
//   - envFile: 环境变量文件路径
//   - clusterName: 集群名称
//   - sql: 要执行的 SQL 语句
//   - showOutput: 是否显示输出
//
// 返回：
//   - YasqlResult: 执行结果
//   - error: 错误信息
func ExecuteSQLAsSysdbaCtx(ctx *runner.StepContext, osUser, envFile, clusterName, sql string, showOutput bool) (*YasqlResult, error) {
	return ExecuteSQLAsSysdbaCtxIgnore(ctx, osUser, envFile, clusterName, sql, showOutput)
}

// ExecuteSQLAsSysdbaCtxIgnore 以 sysdba 执行 SQL，ignoreYasCodes 中的 YAS 错误码视为可忽略。
func ExecuteSQLAsSysdbaCtxIgnore(ctx *runner.StepContext, osUser, envFile, clusterName, sql string, showOutput bool, ignoreYasCodes ...string) (*YasqlResult, error) {
	cfg := &YasqlConfig{
		ClusterName:         clusterName,
		AsSysdba:            true,
		OSUser:              osUser,
		EnvFile:             envFile,
		Quiet:               true,
		Silent:              true,
		ShowOutput:          showOutput,
		IgnoreYasErrorCodes: ignoreYasCodes,
	}
	return ExecuteSQL(ctx, cfg, sql)
}

// YasqlConnectHost 返回 yasql TCP 连接用的 host（YAC VIP/SCAN 优先，否则当前执行节点 IP）。
func YasqlConnectHost(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	isYAC := ctx.GetParamBool("yac_mode", false) || len(ctx.TargetHosts) > 1
	accessMode := strings.ToLower(strings.TrimSpace(ctx.GetParamString("yac_access_mode", "vip")))

	if isYAC && accessMode != "direct" {
		if accessMode == "scan" {
			for _, ip := range ctx.GetParamStringSlice("yac_scan_ips_list") {
				if h := strings.TrimSpace(ip); h != "" {
					return h
				}
			}
		}
		for _, ip := range ctx.GetParamStringSlice("yac_vips") {
			if h := strings.TrimSpace(ip); h != "" {
				return h
			}
		}
	}

	if ctx.Executor != nil {
		if h := strings.TrimSpace(ctx.Executor.Host()); h != "" && !strings.EqualFold(h, "local") {
			return h
		}
	}
	for _, ip := range ctx.GetParamStringSlice("target_ips") {
		if h := strings.TrimSpace(ip); h != "" {
			return h
		}
	}
	return ""
}

// WrapSQLForPDBContainer prefixes SQL with ALTER SESSION SET CONTAINER for execution in a PDB.
//
// Deprecated: YashanDB 不支持 ALTER SESSION SET CONTAINER；请用 ExecuteSQLAsSysdbaInPDBCtx（TCP 连 PDB）。
func WrapSQLForPDBContainer(pdbName, sql string) string {
	id := strings.TrimSpace(pdbName)
	body := strings.TrimSpace(sql)
	if body != "" && !strings.HasSuffix(body, ";") {
		body += ";"
	}
	if body == "" {
		return fmt.Sprintf("ALTER SESSION SET CONTAINER = %s;", sqlPDBIdentifier(id))
	}
	return fmt.Sprintf("ALTER SESSION SET CONTAINER = %s;\n%s", sqlPDBIdentifier(id), body)
}

func sqlPDBIdentifier(name string) string {
	name = strings.TrimSpace(name)
	if matched, _ := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_$#]*$`, name); matched {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ExecuteSQLAsSysdbaInPDBCtx runs SQL in the given PDB via sys@host:port/pdbName TCP 连接。
func ExecuteSQLAsSysdbaInPDBCtx(ctx *runner.StepContext, osUser, envFile, clusterName, pdbName, sql string, showOutput bool) (*YasqlResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("step context is required")
	}
	password := ctx.GetParamString("db_admin_password", "")
	if password == "" {
		return nil, fmt.Errorf("db_admin_password is required for PDB SQL execution")
	}
	port := ctx.GetParamInt("db_begin_port", 1688)
	connectStr := BuildYasqlTCPConnect(YasqlConnectHost(ctx), "sys", password, port, pdbName)
	cfg := &YasqlConfig{
		ConnectString: connectStr,
		OSUser:        osUser,
		EnvFile:       envFile,
		Quiet:         true,
		Silent:        true,
		ShowOutput:    showOutput,
	}
	return ExecuteSQL(ctx, cfg, sql)
}

// BuildYasqlTCPConnect builds user/pass@host:port/service for yasql -f script execution.
func BuildYasqlTCPConnect(host, user, password string, port int, service string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s/%s@%s:%d/%s", user, commonos.YasqlQuotePassword(password), host, port, service)
}

// BuildYasqlLocalTCPConnect builds user/pass@localhost:port/service (prefer BuildYasqlTCPConnect with target host).
func BuildYasqlLocalTCPConnect(user, password string, port int, service string) string {
	return BuildYasqlTCPConnect("localhost", user, password, port, service)
}

// QueryParameter 查询数据库参数（便捷函数）
//
// 参数：
//   - executor: 命令执行器
//   - osUser: 操作系统用户
//   - envFile: 环境变量文件路径
//   - clusterName: 集群名称
//   - paramName: 参数名称
//   - showOutput: 是否显示输出
//
// 返回：
//   - 参数值（如果找到）
//   - error: 错误信息
func QueryParameter(ctx *runner.StepContext, osUser, envFile, clusterName, paramName string, showOutput bool) (string, error) {
	sql := fmt.Sprintf("SELECT value FROM v$parameter WHERE name = '%s';", paramName)

	result, err := ExecuteSQLAsSysdba(ctx, osUser, envFile, clusterName, sql, showOutput)
	if err != nil {
		return "", err
	}

	// 解析输出，提取参数值
	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 跳过表头和分隔线
		if strings.Contains(strings.ToLower(line), "value") || strings.Contains(line, "---") {
			continue
		}
		// 返回第一个非空行作为参数值
		if line != "" && !strings.EqualFold(line, "null") {
			return line, nil
		}
	}

	return "", fmt.Errorf("parameter %s not found or has no value", paramName)
}

// yasqlTwoColumnRowPattern matches yasql fixed-width two-column result rows (e.g. V$PDBS NAME/STATUS).
var yasqlTwoColumnRowPattern = regexp.MustCompile(`^(\S+(?:\$\S+)?)\s+(\S+)\s*$`)

// ParseYasqlOutput 解析 yasql 输出为键值对
// 适用于查询结果为两列（name, value 或 name, status）的场景；支持 | 分隔与空格对齐两种格式。
func ParseYasqlOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 跳过表头和分隔线
		if strings.Contains(line, "---") ||
			(strings.Contains(strings.ToLower(line), "name") && (strings.Contains(strings.ToLower(line), "value") || strings.Contains(strings.ToLower(line), "status"))) {
			continue
		}
		if strings.Contains(strings.ToLower(line), "rows fetched") {
			continue
		}

		// 解析 name | value 格式
		parts := strings.Split(line, "|")
		if len(parts) >= 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" && value != "" && !strings.EqualFold(value, "null") {
				result[key] = value
			}
			continue
		}

		// 解析空格对齐两列（V$PDBS 等）
		if m := yasqlTwoColumnRowPattern.FindStringSubmatch(line); len(m) == 3 {
			result[m[1]] = m[2]
		}
	}

	return result
}
