package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC003HomeCheck 检查 YASDB_HOME 目录下是否有 yasagent 或 yasdb 进程在运行
func StepC003HomeCheck() *runner.Step {
	checkOnHost := func(ctx *runner.StepContext, th runner.TargetHost, installPath, clusterName string, beginPort int) error {
		hctx := ctx.ForHost(th)
		hctx.Logger.Info("Checking for yasagent or yasdb processes under %s on %s...", installPath, th.Host)

		installPathPattern := installPath
		if !strings.HasSuffix(installPathPattern, "/") {
			installPathPattern = installPathPattern + "/"
		}
		homeProcessCmd := fmt.Sprintf(
			"ps -ef | grep -E '(yasdb|yasagent)' | grep -v grep | grep -F %s",
			commonos.ShellSingleQuote(installPathPattern),
		)
		homeProcessResult, _ := hctx.Execute(homeProcessCmd, false)
		if homeProcessResult != nil && homeProcessResult.GetExitCode() == 0 && strings.TrimSpace(homeProcessResult.GetStdout()) != "" {
			processLines := strings.Split(strings.TrimSpace(homeProcessResult.GetStdout()), "\n")
			var pids []string
			var processDetails []string
			for _, line := range processLines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					pids = append(pids, fields[1])
					processDetails = append(processDetails, line)
				}
			}
			if len(pids) > 0 {
				pidList := strings.Join(pids, ", ")
				return fmt.Errorf("YashanDB processes (yasdb/yasagent) are already running under %s on %s (cluster: %s, port: %d); PIDs: %s; process details: %s; please stop the database first or use clean command to remove it", installPath, th.Host, clusterName, beginPort, pidList, strings.Join(processDetails, "; "))
			}
		}
		hctx.Logger.Info("OK: No yasdb/yasagent processes found under %s on %s", installPath, th.Host)
		return nil
	}

	return &runner.Step{
		ID:          "C-003",
		Name:        "Check YASDB_HOME Processes",
		Description: "Verify no yasdb/yasagent processes are running under YASDB_HOME",
		Tags:        []string{"db", "home", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			for _, th := range ctx.HostsToRun() {
				if err := checkOnHost(ctx, th, installPath, clusterName, beginPort); err != nil {
					return err
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			for _, th := range ctx.HostsToRun() {
				if err := checkOnHost(ctx, th, installPath, clusterName, beginPort); err != nil {
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
