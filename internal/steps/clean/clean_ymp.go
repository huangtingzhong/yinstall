package clean

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepCleanYMP Clean YMP installation
func StepCleanYMP() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-YMP",
		Name:        "Clean YMP",
		Description: "Clean YMP installation by stopping processes and removing directories",
		Tags:        []string{"clean", "ymp"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			ympHome := ctx.GetParamString("ymp_home", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			ctx.Logger.Info("YMP cleanup parameters:")
			ctx.Logger.Info("  YMP_HOME: %s", ympHome)
			ctx.Logger.Info("  YMP_USER: %s", ympUser)

			if !commonos.IsSafeUnixRmRfPath(ympHome) {
				return fmt.Errorf("YMP_HOME %q is not an allowed rm -rf target; refusing YMP cleanup", ympHome)
			}
			ympEnvFilePre := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
			if !commonos.IsSafeUnixRmRfPath(ympEnvFilePre) {
				return fmt.Errorf("ymp.env path %q is not an allowed target; refusing YMP cleanup", ympEnvFilePre)
			}

			// Check if YMP_HOME directory exists
			ctx.Logger.Info("Checking if YMP_HOME directory exists...")
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(ympHome)), false)
			ympHomeExists := result != nil && result.GetExitCode() == 0
			if ympHomeExists {
				ctx.Logger.Info("[OK] YMP_HOME directory exists")
			} else {
				ctx.Logger.Info("YMP_HOME directory does not exist (%s)", ympHome)
			}

			// Check if ymp.env file exists
			ympEnvFile := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
			ctx.Logger.Info("Checking if ymp.env file exists...")
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(ympEnvFile)), false)
			ympEnvExists := result != nil && result.GetExitCode() == 0
			if ympEnvExists {
				ctx.Logger.Info("[OK] ymp.env file exists: %s", ympEnvFile)
			} else {
				ctx.Logger.Info("ymp.env file does not exist (%s)", ympEnvFile)
			}

			// If neither YMP_HOME nor ymp.env exists, skip cleanup
			if !ympHomeExists && !ympEnvExists {
				ctx.Logger.Info("Both YMP_HOME and ymp.env do not exist, skipping cleanup")
				return fmt.Errorf("skip: YMP_HOME and ymp.env do not exist")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympHome := ctx.GetParamString("ymp_home", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")
			if !commonos.IsSafeUnixRmRfPath(ympHome) {
				return fmt.Errorf("YMP_HOME %q failed safety check; refusing cleanup", ympHome)
			}
			ympEnvFile := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
			if !commonos.IsSafeUnixRmRfPath(ympEnvFile) {
				return fmt.Errorf("ymp.env path %q failed safety check", ympEnvFile)
			}
			ympHQ := commonos.ShellSingleQuote(ympHome)
			ympEnvQ := commonos.ShellSingleQuote(ympEnvFile)

			ctx.Logger.Info("Starting YMP cleanup process")

			// 1. Find all YMP processes
			ctx.Logger.Info("Step 1: Finding YMP processes")
			ympPat := PathLiteralPrefixForPS(ympHome)
			var findProcessCmd string
			if ympPat == "" {
				findProcessCmd = `false`
			} else {
				findProcessCmd = fmt.Sprintf("ps -ef | grep -F %s | grep -v grep | grep -v yinstall | awk '{print $2}'",
					commonos.ShellSingleQuote(ympPat))
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
				ctx.Logger.Info("No YMP processes found")
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

			// 4. Remove YMP_HOME directory
			ctx.Logger.Info("Step 4: Removing YMP directory")
			result, _ = ctx.Execute(fmt.Sprintf("test -d %s", ympHQ), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Removing YMP_HOME: %s", ympHome)
				result, err := ctx.Execute(fmt.Sprintf("rm -rf %s", ympHQ), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove YMP_HOME: %v", err)
				} else {
					ctx.Logger.Info("YMP_HOME removed successfully")
				}
			} else {
				ctx.Logger.Info("YMP_HOME does not exist, skipping removal")
			}

			// 5. Remove ymp.env file
			ctx.Logger.Info("Step 5: Removing ymp.env configuration file")
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", ympEnvQ), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Removing ymp.env: %s", ympEnvFile)
				result, err := ctx.Execute(fmt.Sprintf("rm -f %s", ympEnvQ), true)
				if err != nil || (result != nil && result.GetExitCode() != 0) {
					ctx.Logger.Warn("Failed to remove ymp.env: %v", err)
				} else {
					ctx.Logger.Info("ymp.env removed successfully")
				}
			} else {
				ctx.Logger.Info("ymp.env does not exist, skipping removal")
			}

			ctx.Logger.Info("YMP cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ympHome := ctx.GetParamString("ymp_home", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			ctx.Logger.Info("Verifying cleanup results")

			// 1. Check if processes still exist
			ympPat := PathLiteralPrefixForPS(ympHome)
			var findProcessCmd string
			if ympPat == "" {
				findProcessCmd = `false`
			} else {
				findProcessCmd = fmt.Sprintf("ps -ef | grep -F %s | grep -v grep | grep -v yinstall",
					commonos.ShellSingleQuote(ympPat))
			}
			result, _ := ctx.Execute(findProcessCmd, false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Error("WARNING: Some processes are still running:")
				ctx.Logger.Error("%s", result.GetStdout())
				return fmt.Errorf("failed to stop all YMP processes")
			} else {
				ctx.Logger.Info("[OK] All processes stopped successfully")
			}

			// 2. Check if YMP_HOME directory still exists
			result, _ = ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(ympHome)), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Warn("WARNING: YMP_HOME still exists: %s", ympHome)
			} else {
				ctx.Logger.Info("[OK] YMP_HOME removed successfully")
			}

			// 3. Check if ymp.env file still exists
			ympEnvFile := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(ympEnvFile)), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Warn("WARNING: ymp.env still exists: %s", ympEnvFile)
			} else {
				ctx.Logger.Info("[OK] ymp.env removed successfully")
			}

			ctx.Logger.Info("Cleanup verification completed")
			return nil
		},
	}
}
