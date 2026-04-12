package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC008CleanStaleBashrc removes stale source lines from .bashrc and ~/.port* files
// that reference non-existent files, preventing errors when running
// `su - yashan` in subsequent steps.
func StepC008CleanStaleBashrc() *runner.Step {
	return &runner.Step{
		ID:          "C-008",
		Name:        "Clean Stale Bashrc Entries",
		Description: "Remove stale environment entries from .bashrc and ~/.port* files before installation",
		Tags:        []string{"db", "env"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				_, err := commonos.GetUserHomeDir(hctx, user)
				if err != nil {
					return err
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				homeDir, err := commonos.GetUserHomeDir(hctx, user)
				if err != nil {
					continue
				}

				// Collect all files to scan:
				// 1. ~/.bashrc (default port 1688)
				// 2. ~/.port* (non-default ports)
				var filesToScan []string

				bashrc := fmt.Sprintf("%s/.bashrc", homeDir)
				r, _ := hctx.Execute(fmt.Sprintf("test -f %s", bashrc), false)
				if r != nil && r.GetExitCode() == 0 {
					filesToScan = append(filesToScan, bashrc)
				}

				// Find all ~/.port* files
				portResult, _ := hctx.Execute(fmt.Sprintf("ls %s/.port* 2>/dev/null", homeDir), false)
				if portResult != nil && portResult.GetStdout() != "" {
					for _, f := range strings.Split(strings.TrimSpace(portResult.GetStdout()), "\n") {
						f = strings.TrimSpace(f)
						if f != "" {
							filesToScan = append(filesToScan, f)
						}
					}
				}

				if len(filesToScan) == 0 {
					hctx.Logger.Info("No env files found on %s, skipping", th.Host)
					continue
				}

				for _, envFile := range filesToScan {
					cleaned := cleanStaleEntriesFromFile(hctx, th.Host, envFile)
					if cleaned > 0 {
						hctx.Logger.Info("Cleaned %d stale entries from %s on %s", cleaned, envFile, th.Host)
					} else {
						hctx.Logger.Info("No stale entries found in %s on %s", envFile, th.Host)
					}
				}
			}
			return nil
		},
	}
}

// cleanStaleEntriesFromFile scans a single env file and removes entries
// that reference non-existent files/directories. Returns count of removed entries.
func cleanStaleEntriesFromFile(hctx *runner.StepContext, host, envFile string) int {
	cleaned := 0

	// Find all `source ...yasboot...bashrc` lines pointing to missing files
	cmd := fmt.Sprintf("grep -n 'source.*\\.yasboot/.*_yasdb_home/conf/.*\\.bashrc' %s 2>/dev/null", envFile)
	result, _ := hctx.Execute(cmd, false)
	if result != nil && result.GetStdout() != "" {
		lines := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
		for _, line := range lines {
			// Extract the file path from `source /path/to/file`
			parts := strings.SplitN(line, "source ", 2)
			if len(parts) < 2 {
				continue
			}
			sourcePath := strings.TrimSpace(parts[1])

			// Check if the referenced file exists
			checkResult, _ := hctx.Execute(fmt.Sprintf("test -f %s", sourcePath), false)
			if checkResult != nil && checkResult.GetExitCode() == 0 {
				continue
			}

			hctx.Logger.Info("Removing stale entry on %s from %s: source %s (file not found)", host, envFile, sourcePath)
			escapedPath := strings.ReplaceAll(sourcePath, "/", "\\/")
			sedCmd := fmt.Sprintf("sed -i '/source.*%s/d' %s", escapedPath, envFile)
			hctx.Execute(sedCmd, false)
			cleaned++
		}
	}

	// Clean stale YASCS_HOME if the path doesn't exist
	cmd = fmt.Sprintf("grep -oP 'export YASCS_HOME=\\K.*' %s 2>/dev/null", envFile)
	result, _ = hctx.Execute(cmd, false)
	if result != nil && result.GetStdout() != "" {
		yascsPath := strings.TrimSpace(result.GetStdout())
		checkResult, _ := hctx.Execute(fmt.Sprintf("test -d %s", yascsPath), false)
		if checkResult == nil || checkResult.GetExitCode() != 0 {
			hctx.Logger.Info("Removing stale YASCS_HOME on %s from %s: %s (dir not found)", host, envFile, yascsPath)
			commonos.BashrcRemoveLine(hctx, envFile, "export YASCS_HOME=")
			cleaned++
		}
	}

	return cleaned
}
