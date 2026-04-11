// yasql.go - YashanDB SQL 执行公共函数
// 提供 yasql 命令执行的通用逻辑，支持多种连接方式

package sql

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

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

	// 构建 yasql 连接字符串
	var connStr string
	if cfg.AsSysdba {
		// 使用 / as sysdba 连接（不需要密码，不需要 @cluster）
		connStr = "/ as sysdba"
	} else if cfg.User != "" && cfg.Password != "" {
		connStr = fmt.Sprintf("%s/%s@%s", cfg.User, commonos.YasqlQuotePassword(cfg.Password), cfg.ClusterName)
	} else {
		return nil, fmt.Errorf("either AsSysdba=true or User/Password must be provided")
	}

	// 使用 heredoc 方式执行 SQL，避免引号转义问题
	// 格式：yasql / as sysdba <<EOF
	//       SQL语句
	//       EOF
	//
	// 在 bash -c 中，使用单引号包裹 EOF 标记（<<'EOF'），避免 bash 解释 $ 符号
	// 由于使用了 <<'EOF'，heredoc 内的内容不会被 bash 解释，所以不需要转义 $
	escapedSQL := sql

	// 构建 yasql 命令，使用 heredoc
	// 使用单引号包裹 EOF 标记（'EOF'），这样 heredoc 内的 $ 不会被 bash 解释
	var options []string
	if cfg.Silent {
		options = append(options, "-S")
	}

	// 构建完整的 yasql 命令，使用 heredoc
	// 注意：在 bash -c "..." 中，使用 <<'EOF' 来避免 $ 被解释
	var yasqlCmd string
	if len(options) > 0 {
		yasqlCmd = fmt.Sprintf("yasql %s %s <<'EOF'\n%s\nEOF",
			strings.Join(options, " "),
			connStr,
			escapedSQL)
	} else {
		yasqlCmd = fmt.Sprintf("yasql %s <<'EOF'\n%s\nEOF",
			connStr,
			escapedSQL)
	}

	// 使用 commonos.ExecuteAsUserWithEnv 执行命令
	result, err := commonos.ExecuteAsUserWithEnv(
		ctx,
		cfg.OSUser,
		cfg.EnvFile,
		yasqlCmd,
		cfg.ShowOutput,
	)

	yasqlResult := &YasqlResult{
		Success: false,
	}

	if result != nil {
		yasqlResult.Stdout = result.GetStdout()
		yasqlResult.Stderr = result.GetStderr()
		yasqlResult.ExitCode = result.GetExitCode()
		yasqlResult.Success = result.GetExitCode() == 0
	}

	if err != nil {
		return yasqlResult, fmt.Errorf("failed to execute yasql: %w", err)
	}

	if !yasqlResult.Success {
		return yasqlResult, fmt.Errorf("yasql command failed with exit code %d: %s",
			yasqlResult.ExitCode, yasqlResult.Stderr)
	}

	return yasqlResult, nil
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

// ExecuteSQLAsSysdbaCtx 以 sysdba 身份执行 SQL（带 StepContext，支持日志记录）
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
	if ctx == nil {
		return nil, fmt.Errorf("step context is required")
	}
	if sql == "" {
		return nil, fmt.Errorf("sql statement is required")
	}

	// 构建 yasql 命令，使用 heredoc
	// 使用 <<EOF（不带引号），SQL 中的 $ 需要转义为 \$，避免被 bash 解释
	var options []string
	options = append(options, "-S") // 静默模式

	connStr := "/ as sysdba"
	// 转义 SQL 中的 $ 符号（用于 v$parameter 等视图）
	escapedSQL := strings.ReplaceAll(sql, "$", "\\$")
	yasqlCmd := fmt.Sprintf("yasql %s %s <<EOF\n%s\nEOF",
		strings.Join(options, " "),
		connStr,
		escapedSQL)

	// 使用 ExecuteAsUserWithEnvCheckCtx 执行命令，确保日志记录
	result, err := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, osUser, envFile, yasqlCmd, showOutput)

	yasqlResult := &YasqlResult{
		Success: false,
	}

	if result != nil {
		yasqlResult.Stdout = result.GetStdout()
		yasqlResult.Stderr = result.GetStderr()
		yasqlResult.ExitCode = result.GetExitCode()
		yasqlResult.Success = result.GetExitCode() == 0
	}

	if err != nil {
		return yasqlResult, fmt.Errorf("failed to execute yasql: %w", err)
	}

	if !yasqlResult.Success {
		return yasqlResult, fmt.Errorf("yasql command failed with exit code %d: %s",
			yasqlResult.ExitCode, yasqlResult.Stderr)
	}

	return yasqlResult, nil
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
