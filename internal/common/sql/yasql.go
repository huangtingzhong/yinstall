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

func errIfYasqlOutputHasError(r *YasqlResult) error {
	if r == nil {
		return nil
	}
	combined := r.Stdout + r.Stderr
	if !OutputContainsYasError(combined) {
		return nil
	}
	code := yasErrorCodePattern.FindString(combined)
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
	if r == nil {
		return fmt.Errorf("yasql result is nil")
	}
	if err := errIfYasqlOutputHasError(r); err != nil {
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

func yasqlResultFromExec(result runner.ExecResult, execErr error) (*YasqlResult, error) {
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
	if err := ValidateYasqlResultSuccess(yasqlResult); err != nil {
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

	connStr, err := buildYasqlConnArg(cfg)
	if err != nil {
		return nil, err
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
	return yasqlResultFromExec(result, execErr)
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
	User        string // 数据库用户，如 sys
	Password    string // 数据库密码
	ClusterName string // 集群名称
	AsSysdba    bool   // 是否使用 as sysdba 连接
	OSUser      string // 操作系统用户（执行 yasql 命令的用户）
	EnvFile     string // 环境变量文件路径
	Silent      bool   // 是否静默模式 (-s)
	Quiet       bool   // 是否安静模式 (-q，不显示 banner）
	ShowOutput  bool   // 是否显示命令输出
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
	return ExecuteSQLAsSysdba(ctx, osUser, envFile, clusterName, sql, showOutput)
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

// ParseYasqlOutput 解析 yasql 输出为键值对
// 适用于查询结果为两列（name, value）的场景
//
// 参数：
//   - output: yasql 输出
//
// 返回：
//   - map[string]string: 键值对映射
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
			strings.Contains(strings.ToLower(line), "name") && strings.Contains(strings.ToLower(line), "value") {
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
		}
	}

	return result
}
