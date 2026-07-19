// standby_topology_remote.go - 在远端读取 env / cluster status 解析 OM 与 primary

package standby

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// ResolveOmHostFromRemoteEnv 读取远端 ~/.yasboot/<cluster>.env 的 om_addr.
func ResolveOmHostFromRemoteEnv(ctx *runner.StepContext, osUser, clusterName string) (string, error) {
	osUser = strings.TrimSpace(osUser)
	clusterName = strings.TrimSpace(clusterName)
	if osUser == "" {
		osUser = "yashan"
	}
	if clusterName == "" {
		clusterName = "yashandb"
	}
	home, err := commonos.GetUserHomeDir(ctx, osUser)
	if err != nil {
		return "", err
	}
	envPath := path.Join(home, ".yasboot", clusterName+".env")
	res, _ := ctx.Execute(fmt.Sprintf("test -f %s && cat %s", commonos.ShellSingleQuote(envPath), commonos.ShellSingleQuote(envPath)), false)
	if res == nil || res.GetExitCode() != 0 || strings.TrimSpace(res.GetStdout()) == "" {
		return "", fmt.Errorf("cluster env not found or empty: %s", envPath)
	}
	return OmHostFromEnvFileContent(res.GetStdout())
}

// FetchClusterStatusOutput 在远端 source env 后执行 yasboot cluster status -d.
func FetchClusterStatusOutput(ctx *runner.StepContext, osUser, envFile, clusterName string) (string, error) {
	osUser = strings.TrimSpace(osUser)
	if osUser == "" {
		osUser = "yashan"
	}
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		clusterName = "yashandb"
	}
	if strings.TrimSpace(envFile) == "" {
		var err error
		envFile, err = GetPrimaryEnvFile(ctx)
		if err != nil {
			return "", err
		}
	}
	cmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
	res, err := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, cmd, false)
	if err != nil {
		return "", err
	}
	if res == nil || res.GetExitCode() != 0 {
		errMsg := ""
		if res != nil {
			errMsg = strings.TrimSpace(res.GetStderr() + " " + res.GetStdout())
		}
		return "", fmt.Errorf("yasboot cluster status failed: %s", errMsg)
	}
	return res.GetStdout(), nil
}

// DiscoverPrimaryIPOnRemote 通过 cluster status 发现当前 primary listen IP.
func DiscoverPrimaryIPOnRemote(ctx *runner.StepContext, osUser, envFile, clusterName string) (string, error) {
	out, err := FetchClusterStatusOutput(ctx, osUser, envFile, clusterName)
	if err != nil {
		return "", err
	}
	ip := PrimaryIPFromClusterStatus(out)
	if ip == "" {
		return "", fmt.Errorf("no primary role found in cluster status")
	}
	return ip, nil
}
