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

// ClusterNameFromEnvFileContent 从端口包装文件（如 ~/.port3988）或直连 bashrc 的文本内容中解析 yasboot 集群名。
// 典型行：source /home/yashan/.yasboot/yashandb_3988_yasdb_home/conf/yashandb_3988.bashrc
// 集群名为路径中的 <cluster>（与 <cluster>_yasdb_home、<cluster>.bashrc 一致）。
func ClusterNameFromEnvFileContent(content string) (string, error) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "source") {
			continue
		}
		if !strings.Contains(line, ".yasboot") && !strings.Contains(line, "_yasdb_home") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		path := parts[1]

		if strings.Contains(path, "_yasdb_home/conf/") {
			startIdx := strings.Index(path, "_yasdb_home/conf/")
			if startIdx > 0 {
				prefix := path[:startIdx]
				lastSlash := strings.LastIndex(prefix, "/")
				if lastSlash >= 0 {
					clusterName := prefix[lastSlash+1:]
					if clusterName != "" {
						return clusterName, nil
					}
				}
			}
		}

		if strings.Contains(path, ".bashrc") {
			lastSlash := strings.LastIndex(path, "/")
			if lastSlash >= 0 {
				filename := path[lastSlash+1:]
				if strings.HasSuffix(filename, ".bashrc") {
					clusterName := strings.TrimSuffix(filename, ".bashrc")
					if clusterName != "" {
						return clusterName, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("cannot extract cluster name from env file content (expected a line like: source .../.yasboot/<cluster>_yasdb_home/conf/<cluster>.bashrc)")
}

// ExtractClusterNameFromEnvFile 在远端 cat envFile 后解析集群名（供独立步骤或 Sync 使用）。
func ExtractClusterNameFromEnvFile(ctx *runner.StepContext, envFile string) (string, error) {
	result, err := ctx.Execute(fmt.Sprintf("cat %s", envFile), false)
	if err != nil {
		return "", fmt.Errorf("failed to read environment file %s: %w", envFile, err)
	}
	if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
		return "", fmt.Errorf("environment file %s is empty", envFile)
	}
	return ClusterNameFromEnvFileContent(result.GetStdout())
}

// SyncPrimaryClusterNameFromEnvFile 根据已解析到的主库 env 文件（如 ~/.port3988、.yasboot 下 bashrc）尝试解析 yasboot 集群名并写回 Params["db_cluster_name"]。
// 解析成功则覆盖为与 yasboot -c 一致的名字（如 yashandb_3988）；适用于「显式 primary_env_file」与「GetPrimaryEnvFile 自动探测」两种路径。
// 解析失败时：若用户显式设置了 primary_env_file 则返回错误；否则静默跳过（保留 CLI 默认或手动传入的 db_cluster_name，例如仅含 PATH 的 .bashrc）。
// 任意在主库上执行且依赖集群名的步骤，在 GetPrimaryEnvFile 成功后应调用本函数（支持单独 -s 重跑某步）。
func SyncPrimaryClusterNameFromEnvFile(ctx *runner.StepContext, envFile string) error {
	explicitEnv := strings.TrimSpace(ctx.GetParamString("primary_env_file", "")) != ""
	name, err := ExtractClusterNameFromEnvFile(ctx, envFile)
	if err != nil {
		if explicitEnv {
			return fmt.Errorf("primary_env_file set but cluster name could not be derived: %w", err)
		}
		return nil
	}
	if name == "" {
		if explicitEnv {
			return fmt.Errorf("primary_env_file set but extracted cluster name is empty")
		}
		return nil
	}
	if ctx.Params == nil {
		ctx.Params = make(map[string]interface{})
	}
	ctx.Params["db_cluster_name"] = name
	return nil
}
