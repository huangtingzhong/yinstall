package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC002PortCheck 检查 yinstall --db-port（yasboot --begin-port）对应端口是否已被占用；占用则报错退出
func StepC002PortCheck() *runner.Step {
	checkOnHost := func(ctx *runner.StepContext, th runner.TargetHost, beginPort int, installPath, clusterName string) error {
		hctx := ctx.ForHost(th)

		// 1. 检查端口是否已被占用
		hctx.Logger.Info("Checking if port %d is in use on %s...", beginPort, th.Host)
		portCmd := fmt.Sprintf("ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)'", beginPort, beginPort)
		result, err := hctx.Execute(portCmd, false)
		if err != nil {
			return fmt.Errorf("failed to check port %d on %s: %w", beginPort, th.Host, err)
		}
		if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
			portInfo := strings.TrimSpace(result.GetStdout())
			hctx.Logger.Warn("Port %d is already in use on %s: %s", beginPort, th.Host, portInfo)
			yasdbCheckCmd := fmt.Sprintf("netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' | grep -i yasdb", beginPort)
			yasdbResult, _ := hctx.Execute(yasdbCheckCmd, false)
			if yasdbResult != nil && yasdbResult.GetExitCode() == 0 {
				return fmt.Errorf("port %d is already in use by YashanDB process on %s; port info: %s; please stop the database first or use clean command to remove it", beginPort, th.Host, portInfo)
			}
			return fmt.Errorf("port %d is already in use on %s (--db-port); port info: %s; please choose another port or stop the process using it", beginPort, th.Host, portInfo)
		}
		hctx.Logger.Info("OK: Port %d is available on %s", beginPort, th.Host)

		// 2. Installation dir exists (warn only)
		hctx.Logger.Info("Checking if installation directory exists on %s...", th.Host)
		dirCmd := fmt.Sprintf("test -d %s", installPath)
		dirResult, _ := hctx.Execute(dirCmd, false)
		if dirResult != nil && dirResult.GetExitCode() == 0 {
			checkEmptyCmd := fmt.Sprintf("test -f %s/bin/yasboot || test -f %s/om/bin/monit", installPath, installPath)
			emptyResult, _ := hctx.Execute(checkEmptyCmd, false)
			if emptyResult != nil && emptyResult.GetExitCode() == 0 {
				hctx.Logger.Warn("Installation directory %s already exists on %s with database files, but target port %d is available; proceeding with installation", installPath, th.Host, beginPort)
			} else {
				hctx.Logger.Warn("Installation directory %s exists but appears to be empty on %s", installPath, th.Host)
			}
		} else {
			hctx.Logger.Info("OK: Installation directory %s does not exist on %s", installPath, th.Host)
		}

		// 3. Existing processes under installPath (warn only)
		hctx.Logger.Info("Checking for running yasdb processes under %s on %s...", installPath, th.Host)
		installPathPattern := installPath
		if !strings.HasSuffix(installPathPattern, "/") {
			installPathPattern = installPathPattern + "/"
		}
		clusterGrep := commonos.ClusterArgBoundaryGrepE(clusterName)
		processCmd := fmt.Sprintf(
			"ps -ef | grep -E '(yasdb|yasagent|yasom)' | grep -v grep | grep -F %s | grep -E %s",
			commonos.ShellSingleQuote(installPathPattern),
			commonos.ShellSingleQuote(clusterGrep),
		)
		processResult, _ := hctx.Execute(processCmd, false)
		if processResult != nil && processResult.GetExitCode() == 0 && strings.TrimSpace(processResult.GetStdout()) != "" {
			processInfo := strings.TrimSpace(processResult.GetStdout())
			hctx.Logger.Warn("Found existing YashanDB processes under %s on %s (cluster: %s), but they are not using port %d; proceeding with installation. Process info: %s", installPath, th.Host, clusterName, beginPort, processInfo)
		} else {
			hctx.Logger.Info("OK: No yasdb processes found under %s on %s", installPath, th.Host)
		}
		return nil
	}

	return &runner.Step{
		ID:          "C-002",
		Name:        "Check Begin Port Available",
		Description: "Verify db begin port is not in use on the host",
		Tags:        []string{"db", "port", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			for _, th := range ctx.HostsToRun() {
				if err := checkOnHost(ctx, th, beginPort, installPath, clusterName); err != nil {
					return err
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			for _, th := range ctx.HostsToRun() {
				if err := checkOnHost(ctx, th, beginPort, installPath, clusterName); err != nil {
					return err
				}
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
