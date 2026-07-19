package clean

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepCleanDB001QueryYACDisks Query YAC disk information before cleanup
func StepCleanDB001QueryYACDisks() *runner.Step {
	return &runner.Step{
		Name:        "Query YAC Disk Information",
		Description: "Query YAC shared disk information using ycsctl before cleanup",
		Tags:        []string{"clean", "db", "yac", "disk", "query"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			cleanDisks := ctx.GetParamString("clean_yac_disks", "")

			// 如果用户明确指定了不清理（空字符串），则跳过
			if cleanDisks == "" {
				ctx.Logger.Info("Checking if this is a YAC environment (optional env probe)...")
				if yac, _ := probeYACEnvironment(ctx); yac {
					ctx.Logger.Warn("Detected YAC environment, but --clean-yac-disks not specified")
					ctx.Logger.Warn("YAC shared disks will NOT be cleaned")
					ctx.Logger.Warn("To clean YAC shared disks, add: --clean-yac-disks auto")
				} else {
					ctx.Logger.Info("Not a YAC environment, skipping disk cleanup")
				}
				return fmt.Errorf("YAC disk cleanup not requested (use --clean-yac-disks auto to enable)")
			}

			if cleanDisks != "auto" {
				// 手动指定磁盘路径，不需要查询
				ctx.Logger.Info("Manual disk paths specified, skipping auto-detection")
				return fmt.Errorf("manual disk paths specified, skipping query")
			}

			// auto 模式：source env；YAC 可由 YASCS_HOME、ycs 目录或 --yac 判定（支持单节点）
			ctx.Logger.Info("Auto mode: prepare env and detect YAC...")
			if err := prepareCleanDBEnv(ctx); err != nil {
				return err
			}
			if !isCleanYACContext(ctx) {
				ctx.Logger.Warn("Not a YAC environment after env prepare")
				return fmt.Errorf("not a YAC environment")
			}
			ctx.Logger.Info("YAC environment confirmed (ycsctl query disk, fallback /dev/yfs on failure)")
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Querying YAC disk information using ycsctl (source env)...")

			var diskPaths []string
			output, err := runYcsctlQueryDisk(ctx)
			if err == nil {
				diskPaths = parseYcsctlOutput(output)
				if len(diskPaths) > 0 {
					ctx.Logger.Info("ycsctl query disk: %d disk(s)", len(diskPaths))
					ctx.Results["yac_disk_paths"] = diskPaths
					return nil
				}
				ctx.Logger.Warn("ycsctl returned no disks, trying /dev/yfs fallback")
			} else {
				ctx.Logger.Warn("ycsctl query disk failed: %v; trying /dev/yfs fallback (single-node YAC)", err)
			}

			diskPaths, err = discoverYACDiskPathsFromYFS(ctx, cleanYFSDiscoverRoot)
			if err != nil {
				return fmt.Errorf("failed to discover YAC disks: %w", err)
			}
			ctx.Logger.Info("Found %d disks from /dev/yfs: %v", len(diskPaths), diskPaths)
			ctx.Results["yac_disk_paths"] = diskPaths
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			diskPaths := ctx.Results["yac_disk_paths"]
			if diskPaths == nil {
				return fmt.Errorf("disk paths not found in results")
			}
			ctx.Logger.Info("YAC disk information queried successfully")
			return nil
		},
	}
}

// StepCleanDB002StopProcesses Stop YashanDB processes
func StepCleanDB002StopProcesses() *runner.Step {
	return &runner.Step{
		Name:        "Stop YashanDB Processes",
		Description: "Stop all YashanDB related processes",
		Tags:        []string{"clean", "db", "process"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			reportCleanStopImpact(ctx)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome, yasdbData, yasdbLog, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			ctx.Logger.Info("Finding YashanDB processes (paths from sourced env)...")

			// 0. 先停 systemd，避免 monit 拉起
			stopCleanDBSystemdUnit(ctx, beginPort)

			// 1. 先停止 monit 监控进程（防止自动重启）
			ctx.Logger.Info("Step 1: Stopping monit monitoring process...")
			monitCmd := buildFindMonitPSCmd(ctx, osUser, clusterName, true)
			result, _ := ctx.Execute(monitCmd, false)
			if result != nil && result.GetStdout() != "" {
				monitPids := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
				for _, pid := range monitPids {
					pid = strings.TrimSpace(pid)
					if pid != "" {
						ctx.Logger.Info("Stopping monit PID: %s", pid)
						ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
					}
				}
				time.Sleep(2 * time.Second)
			} else {
				ctx.Logger.Info("No monit process found")
			}

			// 2. 按 path/-c 与 begin-port（OM/DB 监听）合并查进程
			ctx.Logger.Info("Step 2: Finding all YashanDB processes...")
			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, true)
			findByPortCmd := buildFindYashanDBProcessByBeginPortPSCmd(ctx, beginPort, true)
			pids := collectCleanPIDs(ctx, findProcessCmd, findByPortCmd)
			if len(pids) == 0 {
				ctx.Logger.Info("No YashanDB processes found")
			} else {
				ctx.Logger.Info("Found %d processes to stop", len(pids))
				for _, pid := range pids {
					ctx.Logger.Info("  PID: %s", pid)
				}
			}

			// 3. 优雅停止进程 (SIGTERM)
			if len(pids) > 0 {
				ctx.Logger.Info("Step 3: Stopping processes gracefully (SIGTERM)...")
				for _, pid := range pids {
					ctx.Logger.Info("Sending SIGTERM to PID %s", pid)
					ctx.Execute(fmt.Sprintf("kill -15 %s 2>/dev/null", pid), false)
				}

				ctx.Logger.Info("Waiting 5 seconds for processes to stop...")
				time.Sleep(5 * time.Second)

				// 4. 强制终止残留进程 (SIGKILL)
				ctx.Logger.Info("Step 4: Force killing remaining processes (SIGKILL)...")
				for _, pid := range collectCleanPIDs(ctx, findProcessCmd, findByPortCmd) {
					ctx.Logger.Info("Force killing PID %s", pid)
					ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
				}
				time.Sleep(2 * time.Second)
			}

			// 5. 最后再次检查并强制终止
			ctx.Logger.Info("Step 5: Final process check...")
			time.Sleep(2 * time.Second)
			if remaining := collectCleanPIDs(ctx, findProcessCmd, findByPortCmd); len(remaining) > 0 {
				ctx.Logger.Warn("Still found processes, performing final kill...")
				for _, pid := range remaining {
					ctx.Logger.Info("Final kill PID %s", pid)
					ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
				}
				time.Sleep(3 * time.Second)
			}

			ctx.Logger.Info("Process cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			yasdbHome, yasdbData, yasdbLog, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, true)
			findByPortCmd := buildFindYashanDBProcessByBeginPortPSCmd(ctx, beginPort, true)
			if remaining := collectCleanPIDs(ctx, findProcessCmd, findByPortCmd); len(remaining) > 0 {
				ctx.Logger.Warn("WARNING: Some processes are still running (will be stopped after directory removal): %s",
					strings.Join(remaining, ","))
			} else {
				ctx.Logger.Info("[OK] All processes stopped successfully")
			}
			return nil
		},
	}
}

// StepCleanDB003RemoveDirectories Remove YashanDB directories
func StepCleanDB003RemoveDirectories() *runner.Step {
	return &runner.Step{
		Name:        "Remove YashanDB Directories",
		Description: "Remove YashanDB installation, data, log and stage directories",
		Tags:        []string{"clean", "db", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			yasdbHome, yasdbData, yasdbLog, _, _, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}
			stageDir, err := resolveCleanDBStageDir(ctx)
			if err != nil {
				return err
			}
			for _, p := range []struct{ name, path string }{
				{"YASDB_HOME", yasdbHome},
				{"YASDB_DATA", yasdbData},
				{"YASDB_LOG", yasdbLog},
				{"DB stage directory", stageDir},
			} {
				if err := commonos.ValidateDeletePath(p.path); err != nil {
					return fmt.Errorf("invalid delete path for %s: '%s': %w", p.name, p.path, err)
				}
			}
			reportCleanDirectoryImpact(ctx, yasdbHome, yasdbData, yasdbLog, stageDir)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			if _, _, _, _, _, err := effectiveCleanDBPaths(ctx); err != nil {
				return err
			}

			ctx.Logger.Info("Removing YashanDB directories (sourced env paths)...")
			removeCleanDBDirectoryTree(ctx)

			ctx.Logger.Info("Directory removal completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			yasdbHome, yasdbData, yasdbLog, _, _, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}
			stageDir, err := resolveCleanDBStageDir(ctx)
			if err != nil {
				return err
			}

			verifyDirRemoved(ctx, yasdbHome, "YASDB_HOME")
			verifyDirRemoved(ctx, yasdbData, "YASDB_DATA")
			verifyDirRemoved(ctx, yasdbLog, "YASDB_LOG")
			verifyDirRemoved(ctx, stageDir, "DB stage directory")

			return nil
		},
	}
}

// StepCleanDB004RemoveConfig Remove YashanDB configuration files
func StepCleanDB004RemoveConfig() *runner.Step {
	return &runner.Step{
		Name:        "Remove YashanDB Configuration",
		Description: "Remove .yasboot configuration files",
		Tags:        []string{"clean", "db", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			reportCleanConfigImpact(ctx)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			_, yasdbData, _, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}

			ctx.Logger.Info("Removing .yasboot configuration files...")

			userHome, err := commonos.GetUserHomeDir(ctx, osUser)
			if err != nil {
				return fmt.Errorf("cannot determine home directory for user %s: %w", osUser, err)
			}
			yasbootDir := fmt.Sprintf("%s/.yasboot", userHome)

			envFile := fmt.Sprintf("%s/%s.env", yasbootDir, clusterName)
			ctx.Logger.Info("Removing yasboot env file: %s", envFile)
			if err := commonos.ValidateDeletePath(envFile); err != nil {
				ctx.Logger.Warn("Skipping rm of env file: %v", err)
			} else {
				result, err := ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(envFile)), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove yasboot env file: %v", err)
				} else {
					ctx.Logger.Info("Yasboot env file removed successfully")
				}
			}

			// 删除集群 home 文件
			homeFile := fmt.Sprintf("%s/%s_yasdb_home", yasbootDir, clusterName)
			ctx.Logger.Info("Removing yasboot home file: %s", homeFile)
			if err := commonos.ValidateDeletePath(homeFile); err != nil {
				ctx.Logger.Warn("Skipping rm of home file: %v", err)
			} else {
				result, err := ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(homeFile)), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove yasboot home file: %v", err)
				} else {
					ctx.Logger.Info("Yasboot home file removed successfully")
				}
			}

			ctx.Logger.Info("Configuration cleanup completed")

			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			ctx.Logger.Info("Cleaning up env var entries for cluster '%s' (port %d)...", clusterName, beginPort)
			if cleanErr := commonos.CleanEnvVars(ctx, osUser, clusterName, yasdbData, beginPort); cleanErr != nil {
				ctx.Logger.Warn("Failed to clean env var entries: %v", cleanErr)
			} else {
				ctx.Logger.Info("Env var entries for cluster '%s' cleaned successfully", clusterName)
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			_, _, _, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}

			userHome, err := commonos.GetUserHomeDir(ctx, osUser)
			if err != nil {
				return err
			}
			yasbootDir := fmt.Sprintf("%s/.yasboot", userHome)

			envFile := fmt.Sprintf("%s/%s.env", yasbootDir, clusterName)
			verifyFileRemoved(ctx, envFile, "Yasboot env file")

			homeFile := fmt.Sprintf("%s/%s_yasdb_home", yasbootDir, clusterName)
			verifyFileRemoved(ctx, homeFile, "Yasboot home file")

			return nil
		},
	}
}

// StepCleanDB005CleanYACDisks Clean YAC shared disks
func StepCleanDB005CleanYACDisks() *runner.Step {
	return &runner.Step{
		Name:        "Clean YAC Shared Disks",
		Description: "Clean YAC shared disk headers using dd command",
		Tags:        []string{"clean", "db", "yac", "disk"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			cleanDisks := ctx.GetParamString("clean_yac_disks", "")
			if cleanDisks == "" {
				return fmt.Errorf("YAC disk cleanup not requested (use --clean-yac-disks to enable)")
			}
			reportCleanYACDiskImpact(ctx, cleanDisks)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			cleanDisks := ctx.GetParamString("clean_yac_disks", "")

			ctx.Logger.Info("Starting YAC shared disk cleanup...")

			var diskPaths []string

			// 判断是手动指定还是使用查询结果
			if cleanDisks == "auto" {
				ctx.Logger.Info("Auto mode: using disk paths from query step...")

				// 从 Results 中获取之前查询的磁盘路径
				diskPathsInterface := ctx.Results["yac_disk_paths"]
				if diskPathsInterface == nil {
					ctx.Logger.Warn("No disk paths found in query results, skipping disk cleanup")
					return nil
				}

				var ok bool
				diskPaths, ok = diskPathsInterface.([]string)
				if !ok {
					ctx.Logger.Warn("Invalid disk paths format in results, skipping disk cleanup")
					return nil
				}

				if len(diskPaths) == 0 {
					ctx.Logger.Info("No disks to clean, skipping")
					return nil
				}

				ctx.Logger.Info("Using %d disks from query: %v", len(diskPaths), diskPaths)
			} else {
				// 手动指定磁盘路径
				ctx.Logger.Info("Manual mode: using specified disk paths...")
				diskPaths = strings.Split(cleanDisks, ",")
				for i, path := range diskPaths {
					diskPaths[i] = strings.TrimSpace(path)
				}
				ctx.Logger.Info("Disks to clean: %v", diskPaths)
			}

			// 清理每个磁盘
			successCount := 0
			failCount := 0
			for _, diskPath := range diskPaths {
				diskPath = strings.TrimSpace(diskPath)
				if diskPath == "" {
					continue
				}

				ctx.Logger.Info("Cleaning disk: %s", diskPath)

				if !commonos.IsSafeUnixBlockDevicePath(diskPath) {
					ctx.Logger.Warn("Refusing dd on unsafe disk path: %s", diskPath)
					failCount++
					continue
				}
				diskQ := commonos.ShellSingleQuote(diskPath)

				// 检查磁盘是否存在
				result, _ := ctx.Execute(fmt.Sprintf("test -e %s", diskQ), false)
				if result == nil || result.GetExitCode() != 0 {
					ctx.Logger.Warn("Disk does not exist, skipping: %s", diskPath)
					failCount++
					continue
				}

				// 使用 dd 清理磁盘头（前 10MB）
				ctx.Logger.Info("Wiping disk header (first 10MB): %s", diskPath)
				cmd := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 2>&1", diskQ)
				result, err := ctx.Execute(cmd, true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Error("Failed to wipe disk %s: %v", diskPath, err)
					if result != nil {
						ctx.Logger.Error("Output: %s", result.GetStdout())
					}
					failCount++
				} else {
					ctx.Logger.Info("Successfully wiped disk: %s", diskPath)
					if result != nil && result.GetStdout() != "" {
						ctx.Logger.Info("dd output: %s", result.GetStdout())
					}
					successCount++
				}
			}

			ctx.Logger.Info("YAC disk cleanup completed: %d succeeded, %d failed", successCount, failCount)
			if failCount > 0 {
				return fmt.Errorf("failed to clean %d disk(s)", failCount)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("YAC disk cleanup verification completed")
			return nil
		},
	}
}

// parseYcsctlOutput 解析 ycsctl query disk 的输出，提取磁盘路径
// 输出格式示例：
// ID |STATUS   |PATH                             |DG
// 0  ONLINE    /dev/mapper/sys3                  SYSTEM
// 1  ONLINE    /dev/mapper/sys1                  SYSTEM
// 2  ONLINE    /dev/mapper/sys2                  SYSTEM
func parseYcsctlOutput(output string) []string {
	var diskPaths []string
	lines := strings.Split(output, "\n")

	// 跳过表头，查找包含 /dev/ 的行
	re := regexp.MustCompile(`(/dev/[^\s]+)`)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ID") {
			continue
		}

		// 提取磁盘路径
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			diskPath := strings.TrimSpace(matches[1])
			diskPaths = append(diskPaths, diskPath)
		}
	}

	return diskPaths
}

// probeYACEnvironment 轻量探测 YAC（不强制 source env）；优先 ycs 数据目录。
func probeYACEnvironment(ctx *runner.StepContext) (bool, error) {
	yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
	ycsDataPath := path.Join(yasdbData, "ycs")
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(ycsDataPath)), false)
	if result != nil && result.GetExitCode() == 0 {
		ctx.Logger.Info("YAC indicator: ycs data directory exists at %s", ycsDataPath)
		return true, nil
	}
	return false, nil
}

// StepCleanDB006FinalCheck Final cleanup check
func StepCleanDB006FinalCheck() *runner.Step {
	return &runner.Step{
		Name:        "Final Cleanup Check",
		Description: "Final check to ensure all processes are stopped",
		Tags:        []string{"clean", "db", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome, yasdbData, yasdbLog, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
			if err != nil {
				return err
			}
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			ctx.Logger.Info("Performing final process cleanup check...")
			stopCleanDBSystemdUnit(ctx, beginPort)

			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, true)
			findByPortCmd := buildFindYashanDBProcessByBeginPortPSCmd(ctx, beginPort, true)

			time.Sleep(2 * time.Second)
			validPids := collectCleanPIDs(ctx, findProcessCmd, findByPortCmd)
			if len(validPids) == 0 {
				ctx.Logger.Info("No processes found, cleanup successful")
				return nil
			}

			ctx.Logger.Info("Found %d processes after cleanup, force killing...", len(validPids))
			for _, pid := range validPids {
				ctx.Logger.Info("Force killing PID %s", pid)
				ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
			}
			time.Sleep(2 * time.Second)

			var still []string
			for _, pid := range collectCleanPIDs(ctx, findProcessCmd, findByPortCmd) {
				alive, _ := ctx.Execute(fmt.Sprintf("kill -0 %s 2>/dev/null", pid), false)
				if alive != nil && alive.GetExitCode() == 0 {
					still = append(still, pid)
				}
			}
			if len(still) > 0 {
				return fmt.Errorf("cleanup incomplete: YashanDB processes still running after force kill: %s",
					strings.Join(still, ","))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Final cleanup check completed")
			return nil
		},
	}
}

func reportCleanStopImpact(ctx *runner.StepContext) {
	_, _, _, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
	if err != nil {
		ctx.Logger.Info("Clean stop impact: path resolve deferred (%v)", err)
		return
	}
	beginPort := ctx.GetParamInt("db_begin_port", 1688)
	ctx.ReportPrecheckIssue(runner.PrecheckIssue{
		StepName: "Stop YashanDB Processes",
		Host:     ctx.Executor.Host(),
		Severity: runner.PrecheckSeverityWarn,
		Code:     "PC.CLEAN.STOP_PROCESSES",
		Message: fmt.Sprintf("apply will stop YashanDB processes for cluster=%s user=%s begin-port=%d (systemd/monit/yasom/yasagent/yasdb)",
			clusterName, osUser, beginPort),
		Remediation: "confirm this host should be wiped; use -s to limit phases if you only need a subset",
	})
}

func reportCleanDirectoryImpact(ctx *runner.StepContext, home, data, logDir, stageDir string) {
	ctx.ReportPrecheckIssue(runner.PrecheckIssue{
		StepName: "Remove YashanDB Directories",
		Host:     ctx.Executor.Host(),
		Severity: runner.PrecheckSeverityWarn,
		Code:     "PC.CLEAN.REMOVE_DIRECTORIES",
		Message: fmt.Sprintf("apply will delete directories: home=%s data=%s log=%s stage=%s",
			home, data, logDir, stageDir),
		Remediation: "back up any needed files under these paths before apply",
	})
}

func reportCleanConfigImpact(ctx *runner.StepContext) {
	_, data, _, clusterName, osUser, err := effectiveCleanDBPaths(ctx)
	if err != nil {
		ctx.Logger.Info("Clean config impact: path resolve deferred (%v)", err)
		return
	}
	userHome, herr := commonos.GetUserHomeDir(ctx, osUser)
	if herr != nil {
		userHome = "/home/" + osUser
	}
	envFile := path.Join(userHome, ".yasboot", clusterName+".env")
	homeLink := path.Join(userHome, ".yasboot", clusterName+"_yasdb_home")
	ctx.ReportPrecheckIssue(runner.PrecheckIssue{
		StepName: "Remove YashanDB Configuration",
		Host:     ctx.Executor.Host(),
		Severity: runner.PrecheckSeverityWarn,
		Code:     "PC.CLEAN.REMOVE_CONFIG",
		Message: fmt.Sprintf("apply will remove %s, %s, and clean bashrc/env entries for cluster=%s data=%s",
			envFile, homeLink, clusterName, data),
		Remediation: "confirm cluster name/port match the instance you intend to wipe",
	})
}

func reportCleanYACDiskImpact(ctx *runner.StepContext, cleanDisks string) {
	msg := fmt.Sprintf("apply will dd-wipe YAC shared disk headers (mode=%s)", cleanDisks)
	if cleanDisks == "auto" {
		if paths, ok := ctx.Results["yac_disk_paths"].([]string); ok && len(paths) > 0 {
			msg = fmt.Sprintf("apply will dd-wipe YAC disk headers: %s", strings.Join(paths, ", "))
		} else {
			msg = "apply will dd-wipe YAC disk headers from auto-discovered paths (run Query YAC Disk step first for the list)"
		}
	} else if cleanDisks != "" {
		msg = fmt.Sprintf("apply will dd-wipe YAC disk headers: %s", cleanDisks)
	}
	ctx.ReportPrecheckIssue(runner.PrecheckIssue{
		StepName:    "Clean YAC Shared Disks",
		Host:        ctx.Executor.Host(),
		Severity:    runner.PrecheckSeverityWarn,
		Code:        "PC.CLEAN.WIPE_YAC_DISKS",
		Message:     msg,
		Remediation: "DOUBLE-CHECK disk paths; header wipe is destructive and not easily reversible",
	})
}
