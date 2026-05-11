package clean

import (
	"fmt"
	"path"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// buildFindYashanDBProcessPSCmd 构造 ps|grep，仅匹配当前清理目标相关进程：
// - 使用 grep -F 固定串 + 路径末尾 /，避免 yasdb_home 匹配到 yasdb_home_2788；
// - 使用 ~/.yasboot/<cluster>_yasdb_home/ 匹配 monit 等，不用裸 cluster 名（避免 yashandb 匹配 yashandb_2788）。
func buildFindYashanDBProcessPSCmd(ctx *runner.StepContext, yasdbHome, yasdbData, yasdbLog, osUser, clusterName string, awkPrintPid bool) string {
	homePat := PathLiteralPrefixForPS(yasdbHome)
	dataPat := PathLiteralPrefixForPS(yasdbData)
	logPat := PathLiteralPrefixForPS(yasdbLog)

	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	userHome, err := commonos.GetUserHomeDir(ctx, u)
	if err != nil {
		userHome = path.Join("/home", u)
	}
	yasbootPat := PathLiteralPrefixForPS(path.Join(userHome, ".yasboot", clusterName+"_yasdb_home"))

	var grepFe []string
	for _, pat := range []string{homePat, dataPat, logPat, yasbootPat} {
		if pat == "" {
			continue
		}
		grepFe = append(grepFe, "-e "+commonos.ShellSingleQuote(pat))
	}
	if len(grepFe) == 0 {
		return `false`
	}

	cmd := fmt.Sprintf(
		`ps -ef | grep -E '(yasdb|yasagent|yasom|monit)' | grep -F %s | grep -v grep | grep -v yinstall`,
		strings.Join(grepFe, " "),
	)
	if awkPrintPid {
		return cmd + ` | awk '{print $2}'`
	}
	return cmd
}

// buildFindMonitPSCmd 仅匹配当前集群 monit（其 -c 指向 ~/.yasboot/<cluster>_yasdb_home/.../monitrc），
// 避免误杀其他实例的 monit。
func buildFindMonitPSCmd(ctx *runner.StepContext, osUser, clusterName string, awkPrintPid bool) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	userHome, err := commonos.GetUserHomeDir(ctx, u)
	if err != nil {
		userHome = path.Join("/home", u)
	}
	monitrc := strings.ReplaceAll(path.Join(userHome, ".yasboot", clusterName+"_yasdb_home", "om/monit/monitrc"), `\`, `/`)
	cmd := fmt.Sprintf(
		`ps -ef | grep monit | grep -F %s | grep -v grep | grep -v yinstall`,
		commonos.ShellSingleQuote(monitrc),
	)
	if awkPrintPid {
		return cmd + ` | awk '{print $2}'`
	}
	return cmd
}

// removeDir removes a directory with rm -rf after safety validation (see commonos.IsSafeUnixRmRfPath).
func removeDir(ctx *runner.StepContext, path, label string) {
	if !commonos.IsSafeUnixRmRfPath(path) {
		ctx.Logger.Warn("Skipping removal of %s: path '%s' is not an allowed rm -rf target", label, path)
		return
	}
	pathQ := commonos.ShellSingleQuote(path)

	// Check if directory exists
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", pathQ), false)
	if result == nil || result.GetExitCode() != 0 {
		ctx.Logger.Info("Skipping removal of %s: directory does not exist (%s)", label, path)
		return
	}

	ctx.Logger.Info("Removing %s: %s", label, path)
	result, err := ctx.Execute(fmt.Sprintf("rm -rf %s", pathQ), true)
	if err != nil || (result != nil && result.GetExitCode() != 0) {
		ctx.Logger.Warn("Failed to remove %s: %v", label, err)
	} else {
		ctx.Logger.Info("%s removed successfully", label)
	}
}

// verifyDirRemoved checks that a directory no longer exists
func verifyDirRemoved(ctx *runner.StepContext, path, label string) {
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(path)), false)
	if result != nil && result.GetExitCode() == 0 {
		ctx.Logger.Warn("WARNING: %s still exists: %s", label, path)
	} else {
		ctx.Logger.Info("[OK] %s removed successfully", label)
	}
}

// verifyFileRemoved checks that a file no longer exists
func verifyFileRemoved(ctx *runner.StepContext, path, label string) {
	result, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(path)), false)
	if result != nil && result.GetExitCode() == 0 {
		ctx.Logger.Warn("WARNING: %s still exists: %s", label, path)
	} else {
		ctx.Logger.Info("[OK] %s removed successfully", label)
	}
}

// StepCleanDB Clean YashanDB installation
func StepCleanDB() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-DB",
		Name:        "Clean YashanDB",
		Description: "Clean YashanDB installation by stopping processes and removing directories",
		Tags:        []string{"clean", "db"},
		// Optional: when nothing exists, treat as skip (no-op).
		Optional: true,

		PreCheck: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			ctx.Logger.Info("DB cleanup parameters:")
			ctx.Logger.Info("  YASDB_HOME: %s", yasdbHome)
			ctx.Logger.Info("  YASDB_DATA: %s", yasdbData)
			ctx.Logger.Info("  YASDB_LOG: %s", yasdbLog)
			ctx.Logger.Info("  Cluster Name: %s", clusterName)

			// Validate paths are safe before proceeding
			for _, p := range []struct{ name, path string }{
				{"YASDB_HOME", yasdbHome},
				{"YASDB_DATA", yasdbData},
				{"YASDB_LOG", yasdbLog},
			} {
				if !commonos.IsSafeUnixRmRfPath(p.path) {
					return fmt.Errorf("unsafe path for %s: '%s' - refusing to proceed (must be under allowed installation roots)", p.name, p.path)
				}
			}

			// Check if directories exist
			ctx.Logger.Info("Checking if directories exist...")
			var missingDirs []string
			for _, p := range []struct{ name, path string }{
				{"YASDB_HOME", yasdbHome},
				{"YASDB_DATA", yasdbData},
				{"YASDB_LOG", yasdbLog},
			} {
				result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(p.path)), false)
				if result == nil || result.GetExitCode() != 0 {
					ctx.Logger.Warn("Directory does not exist: %s (%s)", p.name, p.path)
					missingDirs = append(missingDirs, fmt.Sprintf("%s (%s)", p.name, p.path))
				} else {
					ctx.Logger.Info("[OK] Directory exists: %s (%s)", p.name, p.path)
				}
			}

			// If all directories are missing, skip cleanup
			if len(missingDirs) == 3 {
				ctx.Logger.Info("All YashanDB directories do not exist, skipping cleanup")
				return fmt.Errorf("skip: all YashanDB directories do not exist")
			}

			// If some directories are missing, log warning but continue
			if len(missingDirs) > 0 {
				ctx.Logger.Warn("Some directories do not exist and will be skipped: %v", missingDirs)
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			ctx.Logger.Info("Starting DB cleanup process")

			// 1. Find all YashanDB processes（固定串 + 路径尾 / + .yasboot/<cluster>_yasdb_home/，不误伤其他实例）
			ctx.Logger.Info("Step 1: Finding YashanDB processes")
			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, true)
			result, _ := ctx.Execute(findProcessCmd, false)

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
			}

			// 2. Stop processes gracefully (SIGTERM)
			if len(pids) > 0 {
				ctx.Logger.Info("Step 2: Stopping processes gracefully (SIGTERM)")
				for _, pid := range pids {
					pid = strings.TrimSpace(pid)
					if pid != "" {
						ctx.Logger.Info("Sending SIGTERM to PID %s", pid)
						ctx.Execute(fmt.Sprintf("kill -15 %s 2>/dev/null", pid), false)
					}
				}

				ctx.Logger.Info("Waiting 5 seconds for processes to stop...")
				time.Sleep(5 * time.Second)

				// 3. Force kill remaining processes (SIGKILL)
				ctx.Logger.Info("Step 3: Force killing remaining processes (SIGKILL)")
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

			// 4. Remove directories (with safety validation)
			ctx.Logger.Info("Step 4: Removing directories")
			removeDir(ctx, yasdbHome, "YASDB_HOME")
			removeDir(ctx, yasdbData, "YASDB_DATA")
			removeDir(ctx, yasdbLog, "YASDB_LOG")

			// 5. Remove .yasboot files (use dynamic home directory lookup)
			ctx.Logger.Info("Step 5: Removing .yasboot configuration files")

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
				result, err = ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(envFile)), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove yasboot env file: %v", err)
				} else {
					ctx.Logger.Info("Yasboot env file removed successfully")
				}
			}

			homeFile := fmt.Sprintf("%s/%s_yasdb_home", yasbootDir, clusterName)
			ctx.Logger.Info("Removing yasboot home file: %s", homeFile)
			if !commonos.IsSafeUnixRmRfPath(homeFile) {
				ctx.Logger.Warn("Skipping rm of home file: path failed safety check: %s", homeFile)
			} else {
				result, err = ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(homeFile)), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove yasboot home file: %v", err)
				} else {
					ctx.Logger.Info("Yasboot home file removed successfully")
				}
			}

			// 6. 清理 .bashrc 中该集群的环境变量条目
			ctx.Logger.Info("Step 6: Cleaning up .bashrc environment entries for cluster '%s'", clusterName)
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			if cleanErr := commonos.CleanEnvVars(ctx, osUser, clusterName, yasdbData, beginPort); cleanErr != nil {
				ctx.Logger.Warn("Failed to clean .bashrc entries: %v", cleanErr)
			} else {
				ctx.Logger.Info(".bashrc entries for cluster '%s' cleaned successfully", clusterName)
			}

			// 7. Final kill after directory removal
			ctx.Logger.Info("Step 7: Final process cleanup after directory removal")
			time.Sleep(2 * time.Second)
			result, _ = ctx.Execute(findProcessCmd, false)
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
					ctx.Logger.Info("Found %d processes after directory removal, force killing...", len(validPids))
					for _, pid := range validPids {
						ctx.Logger.Info("Force killing PID %s", pid)
						ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null", pid), false)
					}
					time.Sleep(2 * time.Second)
				} else {
					ctx.Logger.Info("No processes found after directory removal")
				}
			} else {
				ctx.Logger.Info("No processes found after directory removal")
			}

			ctx.Logger.Info("DB cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			yasdbHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
			yasdbData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
			yasdbLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			osUser := ctx.GetParamString("os_user", "yashan")

			ctx.Logger.Info("Verifying cleanup results")

			// 1. Check if processes still exist（与 Action 相同：精准路径 + .yasboot 目录，不含裸 cluster 名）
			findProcessCmd := buildFindYashanDBProcessPSCmd(ctx, yasdbHome, yasdbData, yasdbLog, osUser, clusterName, false)
			result, _ := ctx.Execute(findProcessCmd, false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Error("WARNING: Some processes are still running:")
				ctx.Logger.Error("%s", result.GetStdout())
				return fmt.Errorf("failed to stop all YashanDB processes")
			} else {
				ctx.Logger.Info("[OK] All processes stopped successfully")
			}

			// 2. Check if directories still exist（HOME/DATA 必须删净，否则备库扩容 yasboot host add 会报 path should be empty）
			for _, pair := range []struct {
				path  string
				label string
			}{
				{yasdbHome, "YASDB_HOME"},
				{yasdbData, "YASDB_DATA"},
			} {
				res, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(pair.path)), false)
				if res != nil && res.GetExitCode() == 0 {
					return fmt.Errorf("cleanup incomplete: %s still exists at %s (rm may have failed or wrong path; check sudo/permissions and processes holding files)", pair.label, pair.path)
				}
				ctx.Logger.Info("[OK] %s removed: %s", pair.label, pair.path)
			}
			verifyDirRemoved(ctx, yasdbLog, "YASDB_LOG")

			// 3. Check if .yasboot files still exist (use dynamic home directory)
			userHome, err := commonos.GetUserHomeDir(ctx, osUser)
			if err != nil {
				userHome = fmt.Sprintf("/home/%s", osUser)
			}
			yasbootDir := fmt.Sprintf("%s/.yasboot", userHome)

			envFile := fmt.Sprintf("%s/%s.env", yasbootDir, clusterName)
			verifyFileRemoved(ctx, envFile, "Yasboot env file")

			homeFile := fmt.Sprintf("%s/%s_yasdb_home", yasbootDir, clusterName)
			verifyFileRemoved(ctx, homeFile, "Yasboot home file")

			// 4. Check .bashrc no longer references this cluster
			bashrc := fmt.Sprintf("%s/.bashrc", userHome)
			needle := fmt.Sprintf("%s_yasdb_home", clusterName)
			checkCmd := fmt.Sprintf(
				"grep -cF %s %s 2>/dev/null || echo 0",
				commonos.ShellSingleQuote(needle),
				commonos.ShellSingleQuote(bashrc),
			)
			checkResult, _ := ctx.Execute(checkCmd, false)
			if checkResult != nil {
				count := strings.TrimSpace(checkResult.GetStdout())
				if count == "0" {
					ctx.Logger.Info("[OK] .bashrc no longer references cluster '%s'", clusterName)
				} else {
					ctx.Logger.Warn("WARNING: .bashrc still contains %s reference(s) to cluster '%s'", count, clusterName)
				}
			}

			ctx.Logger.Info("Cleanup verification completed")
			return nil
		},
	}
}
