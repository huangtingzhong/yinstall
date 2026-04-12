package db

import (
	"fmt"
	"path"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepC009DeployDatabase Create/Deploy database
func StepC021DeployDatabase() *runner.Step {
	return &runner.Step{
		ID:          "C-021",
		Name:        "Deploy Database",
		Description: "Create and deploy YashanDB database",
		Tags:        []string{"db", "deploy"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			adminPassword := ctx.GetParamString("db_admin_password", "")
			if adminPassword == "" {
				return fmt.Errorf("db_admin_password is required for database deployment")
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			// Check cluster config exists
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("cluster config not found at %s", configPath)
			}

			// Force mode: clean up existing cluster, wipe disk headers, clean password files
			isForce := ctx.IsForceStep()
			if isForce {
				isYACMode := ctx.GetParamBool("yac_mode", false)
				user := ctx.GetParamString("os_user", "yashan")
				dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				yasbootPath := path.Join(stageDir, "bin/yasboot")

				ctx.Logger.Info("Force mode: cleaning up existing cluster, disk headers and password files")

				// 1. Clean cluster using yasboot
				if isYACMode {
					ctx.Logger.Info("YAC mode: executing yasboot cluster clean on first node")
				} else {
					ctx.Logger.Info("Standalone mode: executing yasboot cluster clean on current node")
				}
				cleanCmd := fmt.Sprintf("su - %s -c '%s cluster clean -c %s -f --purge'", user, yasbootPath, clusterName)
				result, _ := ctx.Execute(cleanCmd, true)
				if result != nil && result.GetExitCode() != 0 {
					ctx.Logger.Warn("yasboot cluster clean failed (may not exist): %s", result.GetStderr())
				} else {
					ctx.Logger.Info("yasboot cluster clean completed")
				}

				// 2. Wipe shared disk headers (dd zero first 10MB) to clear YFS metadata
				if isYACMode {
					systemdgStr := ctx.GetParamString("yac_systemdg", "")
					datadgStr := ctx.GetParamString("yac_datadg", "")
					archdgStr := ctx.GetParamString("yac_archdg", "")

					var allDisks []string
					for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
						if dgStr == "" {
							continue
						}
						parts := strings.SplitN(dgStr, ":", 2)
						if len(parts) == 2 {
							for _, d := range strings.Split(parts[1], ",") {
								d = strings.TrimSpace(d)
								if d != "" {
									allDisks = append(allDisks, d)
								}
							}
						}
					}

					// Deduplicate (archdg may share disk with datadg)
					seen := make(map[string]bool)
					var uniqueDisks []string
					for _, d := range allDisks {
						if !seen[d] {
							seen[d] = true
							uniqueDisks = append(uniqueDisks, d)
						}
					}

					if len(uniqueDisks) > 0 {
						firstHost := ctx.TargetHosts[0]
						firstHctx := ctx.ForHost(firstHost)
						ctx.Logger.Info("Wiping YFS metadata on %d shared disks from node %s (shared disks only need one node)...", len(uniqueDisks), firstHost.Host)
						for _, disk := range uniqueDisks {
							ddCmd := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 conv=notrunc 2>/dev/null", disk)
							ddResult, _ := firstHctx.Execute(ddCmd, true)
							if ddResult != nil && ddResult.GetExitCode() == 0 {
								ctx.Logger.Info("  [%s] Wiped header: %s", firstHost.Host, disk)
							} else {
								ctx.Logger.Warn("  [%s] Failed to wipe %s", firstHost.Host, disk)
							}
						}
					}
				}

				// 3. Clean password files
				if isYACMode {
					ctx.Logger.Info("YAC mode: cleaning password files on all nodes")
					for _, th := range ctx.TargetHosts {
						hctx := ctx.ForHost(th)
						findCmd := fmt.Sprintf("find %s -type f -name 'yasdb.pwd' 2>/dev/null", dataPath)
						result, _ := hctx.Execute(findCmd, false)
						if result != nil && result.GetStdout() != "" {
							pwdFiles := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
							for _, pwdFile := range pwdFiles {
								pwdFile = strings.TrimSpace(pwdFile)
								if pwdFile != "" {
									ctx.Logger.Info("Removing password file on %s: %s", th.Host, pwdFile)
									hctx.Execute(fmt.Sprintf("rm -f %s", pwdFile), true)
								}
							}
						}
					}
				} else {
					ctx.Logger.Info("Standalone mode: cleaning password file on current node")
					findCmd := fmt.Sprintf("find %s -type f -name 'yasdb.pwd' 2>/dev/null", dataPath)
					result, _ := ctx.Execute(findCmd, false)
					if result != nil && result.GetStdout() != "" {
						pwdFiles := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
						for _, pwdFile := range pwdFiles {
							pwdFile = strings.TrimSpace(pwdFile)
							if pwdFile != "" {
								ctx.Logger.Info("Removing password file: %s", pwdFile)
								ctx.Execute(fmt.Sprintf("rm -f %s", pwdFile), true)
							}
						}
					}
				}

				ctx.Logger.Info("Force mode cleanup completed")
			}

			// Note: Do NOT clean up .yasboot files here!
			// These files are created by C-020 (Install Software) and are required for deployment.
			// Cleanup of old installations should only happen in C-020 PreCheck.

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			adminPassword := ctx.GetParamString("db_admin_password", "")
			user := ctx.GetParamString("os_user", "yashan")
			isYACMode := ctx.GetParamBool("yac_mode", false)

			yasbootPath := path.Join(stageDir, "bin/yasboot")
			configPath := path.Join(stageDir, clusterName+".toml")

			ctx.Logger.Info("Deploying database cluster: %s", clusterName)

			// Build deploy command (mask password in log)
			// YAC mode requires --yfs-force-create to force create YFS on shared storage
			deployCmd := fmt.Sprintf("%s cluster deploy -t %s -p '***'", yasbootPath, configPath)
			if isYACMode {
				deployCmd += " --yfs-force-create"
				ctx.Logger.Info("YAC mode detected: adding --yfs-force-create parameter")
			}
			ctx.Logger.Info("Command: su - %s -c '%s'", user, deployCmd)

			cmd := fmt.Sprintf("cd %s && su - %s -c '%s cluster deploy -t %s -p \"%s\"",
				stageDir, user, yasbootPath, configPath, adminPassword)
			if isYACMode {
				cmd += " --yfs-force-create"
			}
			cmd += "'"

			result, err := ctx.Execute(cmd, true)
			if err != nil {
				return fmt.Errorf("failed to deploy database: %w", err)
			}

			if result != nil && result.GetExitCode() != 0 {
				// Show detailed error information
				errMsg := result.GetStderr()
				if errMsg == "" {
					errMsg = result.GetStdout()
				}
				ctx.Logger.Error("Deploy command failed:")
				ctx.Logger.Error("  Exit code: %d", result.GetExitCode())
				if errMsg != "" {
					ctx.Logger.Error("  Output: %s", errMsg)
				}
				return fmt.Errorf("deployment failed: %s", errMsg)
			}

			ctx.Logger.Info("Database deployment completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			user := ctx.GetParamString("os_user", "yashan")
			isYACMode := ctx.GetParamBool("yac_mode", false)

			yasbootPath := path.Join(stageDir, "bin/yasboot")

			// Check cluster status
			cmd := fmt.Sprintf("su - %s -c '%s cluster status -c %s -d'", user, yasbootPath, clusterName)
			result, _ := ctx.Execute(cmd, false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Cluster status:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if strings.TrimSpace(line) != "" {
						ctx.Logger.Info("  %s", line)
					}
				}

				// Check expected status
				if isYACMode {
					// YAC: check instance_status=open and database_status=normal
					if !strings.Contains(result.GetStdout(), "open") {
						return fmt.Errorf("instance_status is not 'open'")
					}
				} else {
					// Standalone: check database_role=primary and database_status=normal
					if !strings.Contains(result.GetStdout(), "normal") {
						ctx.Logger.Warn("database_status may not be 'normal'")
					}
				}
			}

			return nil
		},
	}
}
