// primary_env.go - 主库环境变量文件路径辅助函数
// 提供获取主库环境变量文件路径的通用逻辑

package standby

import (
	"fmt"
	"path" // remote (Linux) path
	"strings"

	"github.com/yinstall/internal/runner"
)

// GetPrimaryEnvFile 获取主库环境变量文件路径
// 优先级：
// 1. 如果指定了 primary_env_file 参数，使用指定的路径（绝对路径或相对用户家目录）
// 2. 自动检测：优先使用 ~/.yasboot/<cluster>_yasdb_home/conf/<cluster>.bashrc
// 3. 如果不存在，使用 ~/.bashrc（单实例）或 ~/.<port>（多实例）
func GetPrimaryEnvFile(ctx *runner.StepContext) (string, error) {
	specifiedEnvFile := ctx.GetParamString("primary_env_file", "")
	if specifiedEnvFile != "" {
		if strings.HasPrefix(specifiedEnvFile, "/") {
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", specifiedEnvFile), false)
			if result != nil && result.GetExitCode() == 0 {
				return specifiedEnvFile, nil
			}
			return "", fmt.Errorf("specified primary environment file %s not found", specifiedEnvFile)
		}
		primaryUser := ctx.GetParamString("primary_os_user", "yashan")
		homeDir, err := getUserHomeDir(ctx, primaryUser)
		if err != nil {
			return "", fmt.Errorf("failed to get home directory for primary user %s: %w", primaryUser, err)
		}
		envFile := path.Join(homeDir, specifiedEnvFile)
		result, _ := ctx.Execute(fmt.Sprintf("test -f %s", envFile), false)
		if result != nil && result.GetExitCode() == 0 {
			return envFile, nil
		}
		return "", fmt.Errorf("specified primary environment file %s not found", envFile)
	}

	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	primaryUser := ctx.GetParamString("primary_os_user", "yashan")
	homeDir, err := getUserHomeDir(ctx, primaryUser)
	if err != nil {
		return "", fmt.Errorf("failed to get home directory for primary user %s: %w", primaryUser, err)
	}

	yasbootEnvFile := fmt.Sprintf("%s/.yasboot/%s_yasdb_home/conf/%s.bashrc", homeDir, clusterName, clusterName)
	result, _ := ctx.Execute(fmt.Sprintf("test -f %s", yasbootEnvFile), false)
	if result != nil && result.GetExitCode() == 0 {
		return yasbootEnvFile, nil
	}

	beginPort := ctx.GetParamInt("db_begin_port", 1688)

	portEnvFile := fmt.Sprintf("%s/.port%d", homeDir, beginPort)
	result, _ = ctx.Execute(fmt.Sprintf("test -f %s", portEnvFile), false)
	if result != nil && result.GetExitCode() == 0 {
		return portEnvFile, nil
	}

	var envFile string
	if beginPort != 1688 {
		envFile = fmt.Sprintf("%s/.port%d", homeDir, beginPort)
	} else {
		envFile = fmt.Sprintf("%s/.bashrc", homeDir)
	}

	result, _ = ctx.Execute(fmt.Sprintf("test -f %s", envFile), false)
	if result != nil && result.GetExitCode() == 0 {
		return envFile, nil
	}

	return "", fmt.Errorf("primary environment file not found (tried: %s, %s, %s)", yasbootEnvFile, portEnvFile, envFile)
}

// GetPrimaryOSUser 获取主库数据库用户
func GetPrimaryOSUser(ctx *runner.StepContext) string {
	return ctx.GetParamString("primary_os_user", "yashan")
}

// getUserHomeDir 获取用户主目录（内部辅助函数）
func getUserHomeDir(ctx *runner.StepContext, user string) (string, error) {
	result, _ := ctx.Execute(fmt.Sprintf("getent passwd %s | cut -d: -f6", user), false)
	if result == nil || result.GetStdout() == "" {
		return "", fmt.Errorf("cannot determine home directory for user %s", user)
	}
	homeDir := strings.TrimSpace(result.GetStdout())
	if homeDir == "" {
		homeDir = fmt.Sprintf("/home/%s", user)
	}
	return homeDir, nil
}

// ExtractClusterNameFromEnvFile 从环境文件中提取集群名
// 环境文件格式示例：
//
//	source /home/yashan/.yasboot/huang_yasdb_home/conf/huang.bashrc
//
// 从路径中提取集群名（如 huang）
func ExtractClusterNameFromEnvFile(ctx *runner.StepContext, envFile string) (string, error) {
	result, err := ctx.Execute(fmt.Sprintf("cat %s", envFile), false)
	if err != nil {
		return "", fmt.Errorf("failed to read environment file %s: %w", envFile, err)
	}
	if result == nil || result.GetStdout() == "" {
		return "", fmt.Errorf("environment file %s is empty", envFile)
	}

	content := result.GetStdout()
	lines := strings.Split(content, "\n")

	// 查找包含 source 和 .yasboot 的行
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "source") && strings.Contains(line, ".yasboot") {
			// 提取路径，格式：source /path/to/.yasboot/<cluster>_yasdb_home/conf/<cluster>.bashrc
			// 或者：source ~/.yasboot/<cluster>_yasdb_home/conf/<cluster>.bashrc
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			path := parts[1]

			// 处理 ~ 符号（如果需要，可以在这里展开 ~ 路径）
			// 目前直接使用路径，因为 executor.Execute 会处理 ~ 符号

			// 从路径中提取集群名
			// 路径格式：.../.yasboot/<cluster>_yasdb_home/conf/<cluster>.bashrc
			// 或者：.../<cluster>_yasdb_home/conf/<cluster>.bashrc
			if strings.Contains(path, "_yasdb_home/conf/") {
				// 提取 <cluster>_yasdb_home 部分
				startIdx := strings.Index(path, "_yasdb_home/conf/")
				if startIdx > 0 {
					// 向前查找，找到集群名的开始位置
					// 查找最后一个 / 或 ~/ 或 .yasboot/ 之后的位置
					prefix := path[:startIdx]
					// 查找最后一个 / 的位置
					lastSlash := strings.LastIndex(prefix, "/")
					if lastSlash >= 0 {
						clusterName := prefix[lastSlash+1:]
						if clusterName != "" {
							return clusterName, nil
						}
					}
				}
			}

			// 备用方法：从文件名提取（如 huang.bashrc）
			if strings.Contains(path, ".bashrc") {
				// 提取文件名（不含路径）
				lastSlash := strings.LastIndex(path, "/")
				if lastSlash >= 0 {
					filename := path[lastSlash+1:]
					// 移除 .bashrc 后缀
					if strings.HasSuffix(filename, ".bashrc") {
						clusterName := strings.TrimSuffix(filename, ".bashrc")
						if clusterName != "" {
							return clusterName, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("cannot extract cluster name from environment file %s (no source line with .yasboot found)", envFile)
}
