package db

import (
	"fmt"
	"path"         // remote (Linux) path
	"path/filepath" // local (OS-native) path — for filepath.IsAbs on Windows
	"regexp"
	"strings"

	"github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC022ExecuteCustomSQL Execute custom SQL script
func StepC022ExecuteCustomSQL() *runner.Step {
	return &runner.Step{
		ID:          "C-022",
		Name:        "Execute Custom SQL Script",
		Description: "Execute custom SQL script using yasql",
		Tags:        []string{"db", "sql", "custom"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			sqlScript := ctx.GetParamString("db_custom_sql_script", "")
			if sqlScript == "" {
				return fmt.Errorf("no custom SQL script specified, skipping")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			sqlScript := ctx.GetParamString("db_custom_sql_script", "")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			sysPassword := ctx.GetParamString("db_admin_password", "")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			if sysPassword == "" {
				return fmt.Errorf("db_admin_password is required for SQL execution")
			}

			// 解析脚本路径（支持 remote:, local:, r:, l: 前缀）
			remotePath, err := resolveScriptPath(ctx, sqlScript)
			if err != nil {
				return fmt.Errorf("failed to resolve SQL script path: %w", err)
			}

			ctx.Logger.Info("Executing custom SQL script: %s", remotePath)

			// 构建 yasql 命令（installPath 是远端 Linux 路径，使用 path.Join）
			yasqlPath := path.Join(installPath, "bin/yasql")

			// yasql 连接命令：yasql sys/password@localhost:port/yasdb -f script.sql
			connectStr := fmt.Sprintf("sys/%s@localhost:%d/%s", commonos.YasqlQuotePassword(sysPassword), beginPort, clusterName)
			cmd := fmt.Sprintf("%s %s -f %s", yasqlPath, connectStr, remotePath)

			ctx.Logger.Info("Running yasql command...")
			result, err := ctx.Execute(cmd, false)
			if err != nil {
				return fmt.Errorf("failed to execute yasql: %w", err)
			}

			// 检查执行结果
			if result.GetExitCode() != 0 {
				ctx.Logger.Error("SQL script execution failed with exit code: %d", result.GetExitCode())
				ctx.Logger.Error("STDOUT: %s", result.GetStdout())
				ctx.Logger.Error("STDERR: %s", result.GetStderr())
				return fmt.Errorf("SQL script execution failed")
			}

			// 检查输出中是否有 YAS-NNNNN 错误代码
			output := result.GetStdout() + result.GetStderr()
			if hasYasError(output) {
				ctx.Logger.Error("SQL script execution completed with YAS errors:")
				ctx.Logger.Error("Output: %s", output)
				return fmt.Errorf("SQL script execution failed with YAS errors")
			}

			ctx.Logger.Info("Custom SQL script executed successfully")
			ctx.Logger.Info("Output: %s", result.GetStdout())
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// resolveScriptPath 解析脚本路径，支持多种格式
//
// 工具支持在 Windows/Linux/macOS 控制端运行，目标端始终为 Linux。
// 路径语义：
//   - remote:/path 或 r:/path   — 明确指定远端 Linux 路径，直接使用
//   - local:/path 或 l:/path    — 明确指定本地文件，上传后使用
//   - /absolute/path            — 以 '/' 开头，视为远端 Linux 绝对路径，先检查远端，
//                                  不存在则尝试从本地上传
//   - C:\...（Windows 本地绝对）  — filepath.IsAbs 为 true 但不以 '/' 开头，
//                                  直接从本地上传
//   - relative/path             — 相对路径，从本地软件目录查找并上传
func resolveScriptPath(ctx *runner.StepContext, scriptPath string) (string, error) {
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return "", fmt.Errorf("empty script path")
	}

	// 明确指定远程路径
	if strings.HasPrefix(scriptPath, "remote:") || strings.HasPrefix(scriptPath, "r:") {
		remotePath := strings.TrimPrefix(scriptPath, "remote:")
		remotePath = strings.TrimPrefix(remotePath, "r:")
		remotePath = strings.TrimSpace(remotePath)

		if !file.FileExists(ctx, remotePath) {
			return "", fmt.Errorf("remote file not found: %s", remotePath)
		}
		ctx.Logger.Info("Using remote SQL script: %s", remotePath)
		return remotePath, nil
	}

	// 明确指定本地路径
	if strings.HasPrefix(scriptPath, "local:") || strings.HasPrefix(scriptPath, "l:") {
		localPath := strings.TrimPrefix(scriptPath, "local:")
		localPath = strings.TrimPrefix(localPath, "l:")
		localPath = strings.TrimSpace(localPath)

		remotePath, err := file.FindAndDistribute(
			ctx,
			localPath,
			ctx.LocalSoftwareDirs,
			ctx.RemoteSoftwareDir,
		)
		if err != nil {
			return "", fmt.Errorf("failed to upload local file: %w", err)
		}
		ctx.Logger.Info("Uploaded local SQL script to: %s", remotePath)
		return remotePath, nil
	}

	// 以 '/' 开头 → 远端 Linux 绝对路径，先检查远端，不存在则尝试本地上传
	// 注意：Windows 控制端下 filepath.IsAbs("/foo") == false，因此这里不用 filepath.IsAbs
	if strings.HasPrefix(scriptPath, "/") {
		if file.FileExists(ctx, scriptPath) {
			ctx.Logger.Info("Using existing remote SQL script: %s", scriptPath)
			return scriptPath, nil
		}
		ctx.Logger.Info("Remote file not found at %s, trying to upload from local...", scriptPath)
		remotePath, err := file.FindAndDistribute(
			ctx,
			scriptPath,
			ctx.LocalSoftwareDirs,
			ctx.RemoteSoftwareDir,
		)
		if err != nil {
			return "", fmt.Errorf("file not found on remote or local: %s", scriptPath)
		}
		ctx.Logger.Info("Uploaded SQL script to: %s", remotePath)
		return remotePath, nil
	}

	// 本地平台绝对路径（如 Windows C:\...）→ 直接上传
	if filepath.IsAbs(scriptPath) {
		ctx.Logger.Info("Local absolute path detected, uploading: %s", scriptPath)
		remotePath, err := file.FindAndDistribute(
			ctx,
			scriptPath,
			ctx.LocalSoftwareDirs,
			ctx.RemoteSoftwareDir,
		)
		if err != nil {
			return "", fmt.Errorf("failed to upload local file: %w", err)
		}
		ctx.Logger.Info("Uploaded SQL script to: %s", remotePath)
		return remotePath, nil
	}

	// 相对路径 → 从本地软件目录查找并上传
	ctx.Logger.Info("Relative path detected, searching in local software directories...")
	remotePath, err := file.FindAndDistribute(
		ctx,
		scriptPath,
		ctx.LocalSoftwareDirs,
		ctx.RemoteSoftwareDir,
	)
	if err != nil {
		return "", fmt.Errorf("failed to find and upload SQL script: %w", err)
	}
	ctx.Logger.Info("Uploaded SQL script to: %s", remotePath)
	return remotePath, nil
}

// hasYasError 检查输出中是否包含 YAS-NNNNN 格式的错误代码
func hasYasError(output string) bool {
	// 匹配 YAS-NNNNN 格式（N 为数字）
	re := regexp.MustCompile(`YAS-\d{5}`)
	return re.MatchString(output)
}
