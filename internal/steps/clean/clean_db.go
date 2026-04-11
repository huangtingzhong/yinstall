package clean

import (
	"fmt"
	"path"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// isSafePath checks that a remote Linux path is safe to rm -rf (not empty, not root-only).
// Control plane may be Windows while targets are Linux; paths here are always remote Unix paths,
// so we use path.Clean (forward slashes) like YMP/db steps — not filepath.Clean, which on Windows
// turns "/data/yashan/..." into "\\data\\..." and breaks depth checks when splitting on "/".
func isSafePath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return false
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	cleaned := path.Clean(p)
	if cleaned == "/" || cleaned == "." || cleaned == "" {
		return false
	}
	if !strings.HasPrefix(cleaned, "/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for _, seg := range parts {
		if seg == "" || seg == "." {
			return false
		}
	}
	return len(parts) >= 2
}

// removeDir removes a directory with rm -rf after safety validation
func removeDir(ctx *runner.StepContext, path, label string) {
	if !isSafePath(path) {
		ctx.Logger.Warn("Skipping removal of %s: path '%s' is unsafe", label, path)
		return
	}

	// Check if directory exists
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", path), false)
	if result == nil || result.GetExitCode() != 0 {
		ctx.Logger.Info("Skipping removal of %s: directory does not exist (%s)", label, path)
		return
	}

	ctx.Logger.Info("Removing %s: %s", label, path)
	result, err := ctx.Execute(fmt.Sprintf("rm -rf %s", path), true)
	if err != nil || (result != nil && result.GetExitCode() != 0) {
		ctx.Logger.Warn("Failed to remove %s: %v", label, err)
	} else {
		ctx.Logger.Info("%s removed successfully", label)
	}
}

// verifyDirRemoved checks that a directory no longer exists
func verifyDirRemoved(ctx *runner.StepContext, path, label string) {
	result, _ := ctx.Execute(fmt.Sprintf("test -d %s", path), false)
	if result != nil && result.GetExitCode() == 0 {
		ctx.Logger.Warn("WARNING: %s still exists: %s", label, path)
	} else {
		ctx.Logger.Info("[OK] %s removed successfully", label)
	}
}

// verifyFileRemoved checks that a file no longer exists
func verifyFileRemoved(ctx *runner.StepContext, path, label string) {
	result, _ := ctx.Execute(fmt.Sprintf("test -f %s", path), false)
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
		Optional:    true,

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
				if !isSafePath(p.path) {
					return fmt.Errorf("unsafe path for %s: '%s' — refusing to proceed", p.name, p.path)
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
				result, _ := ctx.Execute(fmt.Sprintf("test -d %s", p.path), false)
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

			// 1. Find all YashanDB processes
			ctx.Logger.Info("Step 1: Finding YashanDB processes")
			yasdbHomePattern := yasdbHome
			if !strings.HasSuffix(yasdbHomePattern, "/") {
				yasdbHomePattern = yasdbHomePattern + "/"
			}
			yasdbDataPattern := yasdbData
			if !strings.HasSuffix(yasdbDataPattern, "/") {
				yasdbDataPattern = yasdbDataPattern + "/"
			}
			// Only target YashanDB-related processes, and avoid matching this cleanup command itself.
			// Note: clusterName / paths may appear in this tool's own command line flags.
			findProcessCmd := fmt.Sprintf(
				"ps -ef | grep -E '(yasdb|yasagent|yasom|monit)' | grep -E '(%s|%s|%s)' | grep -v grep | grep -v yinstall | awk '{print $2}'",
				yasdbHomePattern, yasdbDataPattern, clusterName,
			)
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
			result, err = ctx.Execute(fmt.Sprintf("rm -f %s", envFile), true)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				ctx.Logger.Warn("Failed to remove yasboot env file: %v", err)
			} else {
				ctx.Logger.Info("Yasboot env file removed successfully")
			}

			homeFile := fmt.Sprintf("%s/%s_yasdb_home", yasbootDir, clusterName)
			ctx.Logger.Info("Removing yasboot home file: %s", homeFile)
			result, err = ctx.Execute(fmt.Sprintf("rm -f %s", homeFile), true)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				ctx.Logger.Warn("Failed to remove yasboot home file: %v", err)
			} else {
				ctx.Logger.Info("Yasboot home file removed successfully")
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

			// 1. Check if processes still exist
			// 在路径后添加 / 以避免误匹配
			yasdbHomePattern := yasdbHome
			if !strings.HasSuffix(yasdbHomePattern, "/") {
				yasdbHomePattern = yasdbHomePattern + "/"
			}
			yasdbDataPattern := yasdbData
			if !strings.HasSuffix(yasdbDataPattern, "/") {
				yasdbDataPattern = yasdbDataPattern + "/"
			}
			// Only consider YashanDB-related processes and avoid matching this cleanup tool itself.
			findProcessCmd := fmt.Sprintf(
				"ps -ef | grep -E '(yasdb|yasagent|yasom|monit)' | grep -E '(%s|%s|%s)' | grep -v grep | grep -v yinstall",
				yasdbHomePattern, yasdbDataPattern, clusterName,
			)
			result, _ := ctx.Execute(findProcessCmd, false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Error("WARNING: Some processes are still running:")
				ctx.Logger.Error("%s", result.GetStdout())
				return fmt.Errorf("failed to stop all YashanDB processes")
			} else {
				ctx.Logger.Info("[OK] All processes stopped successfully")
			}

			// 2. Check if directories still exist
			verifyDirRemoved(ctx, yasdbHome, "YASDB_HOME")
			verifyDirRemoved(ctx, yasdbData, "YASDB_DATA")
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
			checkCmd := fmt.Sprintf("grep -c '%s_yasdb_home' %s 2>/dev/null || echo 0", clusterName, bashrc)
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
