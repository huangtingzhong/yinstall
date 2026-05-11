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
		ID:          "CLEAN-DB-001",
		Name:        "Query YAC Disk Information",
		Description: "Query YAC shared disk information using ycsctl before cleanup",
		Tags:        []string{"clean", "db", "yac", "disk", "query"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			cleanDisks := ctx.GetParamString("clean_yac_disks", "")

			// 如果用户明确指定了不清理（空字符串），则跳过
			if cleanDisks == "" {
				// 尝试自动检测是否为 YAC 环境
				ctx.Logger.Info("Checking if this is a YAC environment...")
				if isYACEnvironment(ctx) {
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

			// auto 模式：检测 YAC 环境
			ctx.Logger.Info("Auto mode: detecting YAC environment...")
			if !isYACEnvironment(ctx) {
				ctx.Logger.Warn("Not a YAC environment, skipping disk cleanup")
				return fmt.Errorf("not a YAC environment")
			}

			ctx.Logger.Info("YAC environment confirmed, will query disk information")
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			osUser := ctx.GetParamString("os_user", "yashan")

			ctx.Logger.Info("Querying YAC disk information using ycsctl...")

			// 检查 ycsctl 命令是否存在
			ycsctlPath := path.Join(yasdbHome, "bin/ycsctl")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", ycsctlPath), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("ycsctl command not found at %s, cannot query disks", ycsctlPath)
				return fmt.Errorf("ycsctl not found, cannot query disks in auto mode")
			}

			// 切换到数据库用户执行 ycsctl query disk
			result, err := commonos.ExecuteAsUser(ctx, osUser, fmt.Sprintf("%s query disk", ycsctlPath), true)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("Failed to execute ycsctl query disk: %v", err)
				if result != nil {
					ctx.Logger.Warn("Output: %s", result.GetStdout())
					ctx.Logger.Warn("Error: %s", result.GetStderr())
				}
				return fmt.Errorf("failed to query disk information")
			}

			// 解析输出获取磁盘路径
			output := result.GetStdout()
			ctx.Logger.Info("ycsctl query disk output:")
			ctx.Logger.Info("%s", output)

			diskPaths := parseYcsctlOutput(output)
			if len(diskPaths) == 0 {
				ctx.Logger.Warn("No disks found in ycsctl output")
				return fmt.Errorf("no disks found in ycsctl output")
			}

			ctx.Logger.Info("Found %d disks: %v", len(diskPaths), diskPaths)

			// 将磁盘路径保存到 Results 中，供后续步骤使用
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
		ID:          "CLEAN-DB-002",
		Name:        "Stop YashanDB Processes",
		Description: "Stop all YashanDB related processes",
		Tags:        []string{"clean", "db", "process"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			ctx.Logger.Info("Finding YashanDB processes...")

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

			// 2. 查找所有 YashanDB 进程（grep -F 固定路径前缀，不误伤 yasdb_home_2788 等）
			ctx.Logger.Info("Step 2: Finding all YashanDB processes...")
			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, true)
			result, _ = ctx.Execute(findProcessCmd, false)

			var pids []string
			if result != nil && result.GetStdout() != "" {
				pids = strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
				ctx.Logger.Info("Found %d processes to stop", len(pids))
				for _, pid := range pids {
					if strings.TrimSpace(pid) != "" {
						ctx.Logger.Info("  PID: %s", pid)
					}
				}
			} else {
				ctx.Logger.Info("No YashanDB processes found")
				return nil
			}

			// 3. 优雅停止进程 (SIGTERM)
			if len(pids) > 0 {
				ctx.Logger.Info("Step 3: Stopping processes gracefully (SIGTERM)...")
				for _, pid := range pids {
					pid = strings.TrimSpace(pid)
					if pid != "" {
						ctx.Logger.Info("Sending SIGTERM to PID %s", pid)
						ctx.Execute(fmt.Sprintf("kill -15 %s 2>/dev/null", pid), false)
					}
				}

				// 等待进程停止
				ctx.Logger.Info("Waiting 5 seconds for processes to stop...")
				time.Sleep(5 * time.Second)

				// 4. 强制终止残留进程 (SIGKILL)
				ctx.Logger.Info("Step 4: Force killing remaining processes (SIGKILL)...")
				result, _ = ctx.Execute(findProcessCmd, false)
				if result != nil && result.GetStdout() != "" {
					remainingPids := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
					for _, pid := range remainingPids {
						pid = strings.TrimSpace(pid)
						if pid != "" {
							ctx.Logger.Info("Force killing PID %s", pid)
							ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
						}
					}
					time.Sleep(2 * time.Second)
				} else {
					ctx.Logger.Info("All processes stopped gracefully")
				}
			}

			// 5. 最后再次检查并强制终止
			ctx.Logger.Info("Step 5: Final process check...")
			time.Sleep(2 * time.Second)
			result, _ = ctx.Execute(findProcessCmd, false)
			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Warn("Still found processes, performing final kill...")
				remainingPids := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
				for _, pid := range remainingPids {
					pid = strings.TrimSpace(pid)
					if pid != "" {
						ctx.Logger.Info("Final kill PID %s", pid)
						ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
					}
				}
				time.Sleep(3 * time.Second)
			}

			ctx.Logger.Info("Process cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, false)
			result, _ := ctx.Execute(findProcessCmd, false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Warn("WARNING: Some processes are still running (will be stopped after directory removal):")
				ctx.Logger.Warn("%s", result.GetStdout())
				// 不返回错误，因为删除目录后进程会自然停止
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
		ID:          "CLEAN-DB-003",
		Name:        "Remove YashanDB Directories",
		Description: "Remove YashanDB installation, data and log directories",
		Tags:        []string{"clean", "db", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")

			// 验证路径安全性
			for _, p := range []struct{ name, path string }{
				{"YASDB_HOME", yasdbHome},
				{"YASDB_DATA", yasdbData},
				{"YASDB_LOG", yasdbLog},
			} {
				if !commonos.IsSafeUnixRmRfPath(p.path) {
					return fmt.Errorf("unsafe path for %s: '%s' - refusing to proceed (must be under allowed installation roots)", p.name, p.path)
				}
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")

			ctx.Logger.Info("Removing YashanDB directories...")
			removeDir(ctx, yasdbHome, "YASDB_HOME")
			removeDir(ctx, yasdbData, "YASDB_DATA")
			removeDir(ctx, yasdbLog, "YASDB_LOG")

			ctx.Logger.Info("Directory removal completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")

			verifyDirRemoved(ctx, yasdbHome, "YASDB_HOME")
			verifyDirRemoved(ctx, yasdbData, "YASDB_DATA")
			verifyDirRemoved(ctx, yasdbLog, "YASDB_LOG")

			return nil
		},
	}
}

// StepCleanDB004RemoveConfig Remove YashanDB configuration files
func StepCleanDB004RemoveConfig() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-DB-004",
		Name:        "Remove YashanDB Configuration",
		Description: "Remove .yasboot configuration files",
		Tags:        []string{"clean", "db", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			ctx.Logger.Info("Removing .yasboot configuration files...")

			userHome, err := commonos.GetUserHomeDir(ctx, osUser)
			if err != nil {
				ctx.Logger.Warn("Cannot determine home directory for user %s, falling back to /home/%s", osUser, osUser)
				userHome = fmt.Sprintf("/home/%s", osUser)
			}
			yasbootDir := fmt.Sprintf("%s/.yasboot", userHome)

			envFile := fmt.Sprintf("%s/%s.env", yasbootDir, clusterName)
			ctx.Logger.Info("Removing yasboot env file: %s", envFile)
			if !commonos.IsSafeUnixRmRfPath(envFile) {
				ctx.Logger.Warn("Skipping rm of env file: path failed safety check: %s", envFile)
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
			if !commonos.IsSafeUnixRmRfPath(homeFile) {
				ctx.Logger.Warn("Skipping rm of home file: path failed safety check: %s", homeFile)
			} else {
				result, err := ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(homeFile)), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove yasboot home file: %v", err)
				} else {
					ctx.Logger.Info("Yasboot home file removed successfully")
				}
			}

			ctx.Logger.Info("Configuration cleanup completed")

			// 清理 ~/.bashrc 或 ~/.port<port> 中该集群的环境变量条目
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
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
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			userHome, err := commonos.GetUserHomeDir(ctx, osUser)
			if err != nil {
				userHome = fmt.Sprintf("/home/%s", osUser)
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
		ID:          "CLEAN-DB-005",
		Name:        "Clean YAC Shared Disks",
		Description: "Clean YAC shared disk headers using dd command",
		Tags:        []string{"clean", "db", "yac", "disk"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			cleanDisks := ctx.GetParamString("clean_yac_disks", "")
			if cleanDisks == "" {
				return fmt.Errorf("YAC disk cleanup not requested (use --clean-yac-disks to enable)")
			}
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

// isYACEnvironment 检测是否为 YAC 环境
// 判断依据：
// 1. ycsctl 命令存在
// 2. 数据目录中存在 ycs 子目录
// 满足任意一个条件即认为是 YAC 环境
func isYACEnvironment(ctx *runner.StepContext) bool {
	yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
	yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")

	// 检查 ycsctl 命令
	ycsctlPath := path.Join(yasdbHome, "bin/ycsctl")
	result, _ := ctx.Execute(fmt.Sprintf("test -f %s", ycsctlPath), false)
	hasYcsctl := result != nil && result.GetExitCode() == 0

	// 检查 ycs 数据目录
	ycsDataPath := path.Join(yasdbData, "ycs")
	result, _ = ctx.Execute(fmt.Sprintf("test -d %s", ycsDataPath), false)
	hasYcsData := result != nil && result.GetExitCode() == 0

	// 记录检测结果
	if hasYcsctl {
		ctx.Logger.Info("YAC indicator: ycsctl command exists at %s", ycsctlPath)
	}
	if hasYcsData {
		ctx.Logger.Info("YAC indicator: ycs data directory exists at %s", ycsDataPath)
	}

	// 任意一个条件满足即认为是 YAC 环境（支持单节点 YAC）
	return hasYcsctl || hasYcsData
}

// StepCleanDB006FinalCheck Final cleanup check
func StepCleanDB006FinalCheck() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-DB-006",
		Name:        "Final Cleanup Check",
		Description: "Final check to ensure all processes are stopped",
		Tags:        []string{"clean", "db", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			ctx.Logger.Info("Performing final process cleanup check...")

			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, true)

			time.Sleep(2 * time.Second)
			result, _ := ctx.Execute(findProcessCmd, false)
			if result != nil && result.GetStdout() != "" {
				remainingPids := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
				var validPids []string
				for _, pid := range remainingPids {
					pid = strings.TrimSpace(pid)
					if pid != "" {
						validPids = append(validPids, pid)
					}
				}
				if len(validPids) > 0 {
					ctx.Logger.Info("Found %d processes after cleanup, force killing...", len(validPids))
					for _, pid := range validPids {
						ctx.Logger.Info("Force killing PID %s", pid)
						ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
					}
					time.Sleep(2 * time.Second)
				}
			} else {
				ctx.Logger.Info("No processes found, cleanup successful")
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Final cleanup check completed")
			return nil
		},
	}
}
