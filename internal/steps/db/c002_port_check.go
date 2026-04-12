package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC002PortCheck 检查 yinstall --db-port（yasboot --begin-port）对应端口是否已被占用；占用则报错退出
func StepC002PortCheck() *runner.Step {
	return &runner.Step{
		ID:          "C-002",
		Name:        "Check Begin Port Available",
		Description: "Verify db begin port is not in use on the host",
		Tags:        []string{"db", "port", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)

				// 1. 检查端口是否被占用（这是最关键的检查）
				// 使用精确匹配避免误匹配（如 1688 不会匹配到 16888）
				hctx.Logger.Info("Checking if port %d is in use on %s...", beginPort, th.Host)
				portCmd := fmt.Sprintf("ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", beginPort, beginPort)
				result, err := hctx.Execute(portCmd, false)
				if err != nil {
					return fmt.Errorf("failed to check port %d on %s: %w", beginPort, th.Host, err)
				}
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					portInfo := strings.TrimSpace(result.GetStdout())
					// 端口被占用，检查是否是yasdb进程
					hctx.Logger.Warn("Port %d is already in use on %s: %s", beginPort, th.Host, portInfo)
					// 尝试检查是否是yasdb进程占用的
					yasdbCheckCmd := fmt.Sprintf("netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' | grep -i yasdb", beginPort)
					yasdbResult, _ := hctx.Execute(yasdbCheckCmd, false)
					if yasdbResult != nil && yasdbResult.GetExitCode() == 0 {
						// 是yasdb进程占用了端口
						return fmt.Errorf("port %d is already in use by YashanDB process on %s; port info: %s; please stop the database first or use clean command to remove it", beginPort, th.Host, portInfo)
					}
					// 不是yasdb进程，但仍被占用
					return fmt.Errorf("port %d is already in use on %s (--db-port); port info: %s; please choose another port or stop the process using it", beginPort, th.Host, portInfo)
				}
				hctx.Logger.Info("✓ Port %d is available on %s", beginPort, th.Host)

				// 2. 检查安装目录是否存在（如果存在，说明数据库可能已安装）
				hctx.Logger.Info("Checking if installation directory exists on %s...", th.Host)
				dirCmd := fmt.Sprintf("test -d %s", installPath)
				dirResult, _ := hctx.Execute(dirCmd, false)
				if dirResult != nil && dirResult.GetExitCode() == 0 {
					// 检查目录是否为空（可能只是创建了目录但未安装）
					checkEmptyCmd := fmt.Sprintf("test -f %s/bin/yasboot || test -f %s/om/bin/monit", installPath, installPath)
					emptyResult, _ := hctx.Execute(checkEmptyCmd, false)
					if emptyResult != nil && emptyResult.GetExitCode() == 0 {
						// 目录存在且包含数据库文件，给出警告但继续（因为端口检查已经通过）
						hctx.Logger.Warn("Installation directory %s already exists on %s with database files, but target port %d is available; proceeding with installation", installPath, th.Host, beginPort)
					} else {
						// 目录存在但为空，给出警告但不阻止（会在后续步骤处理）
						hctx.Logger.Warn("Installation directory %s exists but appears to be empty on %s", installPath, th.Host)
					}
				} else {
					hctx.Logger.Info("✓ Installation directory %s does not exist on %s", installPath, th.Host)
				}

				// 3. 检查安装路径下是否有 yasdb 进程在运行（仅作为警告，不阻止安装）
				// 注意：端口检查已经通过，所以即使有进程运行，只要端口不冲突就允许安装
				// 这允许在同一安装路径下，用不同端口安装多个实例
				hctx.Logger.Info("Checking for running yasdb processes under %s on %s...", installPath, th.Host)
				// 在路径后添加 / 以避免误匹配（如 /data/1233 不会匹配到 /data/12334）
				installPathPattern := installPath
				if !strings.HasSuffix(installPathPattern, "/") {
					installPathPattern = installPathPattern + "/"
				}
				// 路径固定串 + 集群名边界（避免 -c yashandb 匹配到 -c yashandb_2788）
				clusterGrep := commonos.ClusterArgBoundaryGrepE(clusterName)
				processCmd := fmt.Sprintf(
					"ps -ef | grep -E '(yasdb|yasagent|yasom)' | grep -v grep | grep -F %s | grep -E %s",
					commonos.ShellSingleQuote(installPathPattern),
					commonos.ShellSingleQuote(clusterGrep),
				)
				processResult, _ := hctx.Execute(processCmd, false)
				if processResult != nil && processResult.GetExitCode() == 0 && strings.TrimSpace(processResult.GetStdout()) != "" {
					processInfo := strings.TrimSpace(processResult.GetStdout())
					// 找到了进程，但端口检查已通过，所以只给出警告
					hctx.Logger.Warn("Found existing YashanDB processes under %s on %s (cluster: %s), but they are not using port %d; proceeding with installation. Process info: %s", installPath, th.Host, clusterName, beginPort, processInfo)
				} else {
					hctx.Logger.Info("✓ No yasdb processes found under %s on %s", installPath, th.Host)
				}
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
