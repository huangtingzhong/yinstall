package clean

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepCleanYCM Clean YCM installation
func StepCleanYCM() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-YCM",
		Name:        "Clean YCM",
		Description: "Clean YCM installation by stopping processes and removing directories",
		Tags:        []string{"clean", "ycm"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			ycmHome := ctx.GetParamString("ycm_home", "/opt/ycm")

			ctx.Logger.Info("YCM cleanup parameters:")
			ctx.Logger.Info("  YCM_HOME: %s", ycmHome)

			if !commonos.IsSafeUnixRmRfPath(ycmHome) {
				return fmt.Errorf("YCM_HOME %q is not an allowed rm -rf target; refusing YCM cleanup", ycmHome)
			}

			// Check if directory exists
			ctx.Logger.Info("Checking if YCM_HOME directory exists...")
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(ycmHome)), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Info("YCM_HOME directory does not exist (%s), skipping cleanup", ycmHome)
				return fmt.Errorf("skip: YCM_HOME directory does not exist")
			}
			ctx.Logger.Info("[OK] YCM_HOME directory exists")

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmHome := ctx.GetParamString("ycm_home", "/opt/ycm")
			if !commonos.IsSafeUnixRmRfPath(ycmHome) {
				return fmt.Errorf("YCM_HOME %q failed safety check; refusing to remove", ycmHome)
			}
			ycmQ := commonos.ShellSingleQuote(ycmHome)

			ctx.Logger.Info("Starting YCM cleanup process")

			// 1. Find all YCM processes
			ctx.Logger.Info("Step 1: Finding YCM processes")
			// grep -F + PathLiteralPrefixForPS：避免 /opt/ycm 匹配 /opt/ycm2、/data123 匹配 /data1234 等前缀歧义
			ycmPat := PathLiteralPrefixForPS(ycmHome)
			var findProcessCmd string
			if ycmPat == "" {
				findProcessCmd = `false`
			} else {
				findProcessCmd = fmt.Sprintf("ps -ef | grep -F %s | grep -v grep | grep -v yinstall | awk '{print $2}'",
					commonos.ShellSingleQuote(ycmPat))
			}
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
				ctx.Logger.Info("No YCM processes found")
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

				// Wait for processes to stop
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

			// 4. Remove directory
			ctx.Logger.Info("Step 4: Removing YCM directory")
			ctx.Logger.Info("Removing YCM_HOME: %s", ycmHome)
			result, err := ctx.Execute(fmt.Sprintf("rm -rf %s", ycmQ), true)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				ctx.Logger.Warn("Failed to remove YCM_HOME: %v", err)
			} else {
				ctx.Logger.Info("YCM_HOME removed successfully")
			}

			ctx.Logger.Info("YCM cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ycmHome := ctx.GetParamString("ycm_home", "")

			ctx.Logger.Info("Verifying cleanup results")

			// 1. Check if processes still exist
			ycmPat := PathLiteralPrefixForPS(ycmHome)
			var findProcessCmd string
			if ycmPat == "" {
				findProcessCmd = `false`
			} else {
				findProcessCmd = fmt.Sprintf("ps -ef | grep -F %s | grep -v grep | grep -v yinstall",
					commonos.ShellSingleQuote(ycmPat))
			}
			result, _ := ctx.Execute(findProcessCmd, false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Error("WARNING: Some processes are still running:")
				ctx.Logger.Error("%s", result.GetStdout())
				return fmt.Errorf("failed to stop all YCM processes")
			} else {
				ctx.Logger.Info("[OK] All processes stopped successfully")
			}

			// 2. Check if directory still exists
			result, _ = ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(ycmHome)), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Warn("WARNING: YCM_HOME still exists: %s", ycmHome)
			} else {
				ctx.Logger.Info("[OK] YCM_HOME removed successfully")
			}

			ctx.Logger.Info("Cleanup verification completed")
			return nil
		},
	}
}
