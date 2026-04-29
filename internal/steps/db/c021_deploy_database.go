package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC021DeployDatabase 部署数据库集群（yasboot cluster deploy）
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

			// 确认集群配置文件已存在
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("cluster config not found at %s", configPath))
			}

			// PreCheck 须只读：此处不做任何清理操作。
			if ctx.IsForceStep() {
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepID:      "C-021",
					StepName:    "Deploy Database",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityInfo,
					Code:        "PC.DB.FORCE_MODE",
					Message:     "Detected --force-steps C-021: apply will perform destructive cleanup (cluster clean / shared-disk header wipe / password-file removal). Precheck will not execute these actions.",
					Remediation: "Confirm this is intended; take backups and double-check disk parameters before applying.",
				})
			}

			// 注意：此处不要清理 .yasboot 产物！
			// 这些文件由 C-020（Install Software）生成，部署阶段仍需要。
			// 旧安装残留清理应仅在 C-020 的 PreCheck/Action 策略中处理。

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			adminPassword := ctx.GetParamString("db_admin_password", "")
			user := ctx.GetParamString("os_user", "yashan")
			isYACMode := ctx.GetParamBool("yac_mode", false)
			isForce := ctx.IsForceStep()

			yasbootPath := path.Join(stageDir, "bin/yasboot")
			configPath := path.Join(stageDir, clusterName+".toml")

			// force 模式下的破坏性清理（写操作）只能放在 Action，不能放在 PreCheck。
			if isForce {
				dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				ctx.Logger.Info("Force mode: cleaning up existing cluster, disk headers and password files")

				// 1. Clean cluster using yasboot
				if isYACMode {
					ctx.Logger.Info("YAC mode: executing yasboot cluster clean on first node")
				} else {
					ctx.Logger.Info("Standalone mode: executing yasboot cluster clean on current node")
				}
				cleanInner := fmt.Sprintf("%s cluster clean -c %s -f --purge", yasbootPath, clusterName)
				r, _ := commonos.ExecuteAsUser(ctx, user, cleanInner, true)
				if r != nil && r.GetExitCode() != 0 {
					ctx.Logger.Warn("yasboot cluster clean failed (may not exist): %s", r.GetStderr())
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

					seen := make(map[string]bool)
					var uniqueDisks []string
					for _, d := range allDisks {
						if !seen[d] {
							seen[d] = true
							uniqueDisks = append(uniqueDisks, d)
						}
					}

					if len(uniqueDisks) > 0 && len(ctx.TargetHosts) > 0 {
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
						res, _ := hctx.Execute(findCmd, false)
						if res != nil && res.GetStdout() != "" {
							pwdFiles := strings.Split(strings.TrimSpace(res.GetStdout()), "\n")
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
					res, _ := ctx.Execute(findCmd, false)
					if res != nil && res.GetStdout() != "" {
						pwdFiles := strings.Split(strings.TrimSpace(res.GetStdout()), "\n")
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

			ctx.Logger.Info("Deploying database cluster: %s", clusterName)

			// 组装 deploy 命令（日志中掩码密码）
			// YAC 模式需要 --yfs-force-create，以便在共享盘上强制创建 YFS
			deployCmd := fmt.Sprintf("%s cluster deploy -t %s -p '***'", yasbootPath, configPath)
			if isYACMode {
				deployCmd += " --yfs-force-create"
				ctx.Logger.Info("YAC mode detected: adding --yfs-force-create parameter")
			}
			ctx.Logger.Info("Command (run as %s): %s", user, deployCmd)

			inner := fmt.Sprintf("%s cluster deploy -t %s -p %s", yasbootPath, configPath, commonos.ShellSingleQuote(adminPassword))
			cmd := fmt.Sprintf("cd %s && %s", stageDir, inner)
			if isYACMode {
				cmd += " --yfs-force-create"
			}

			result, err := commonos.ExecuteAsUser(ctx, user, cmd, true)
			if err != nil {
				return fmt.Errorf("failed to deploy database: %w", err)
			}

			if result != nil && result.GetExitCode() != 0 {
				// 输出详细错误信息便于排障
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

			// 检查集群状态输出
			result, _ := commonos.ExecuteAsUser(ctx, user, fmt.Sprintf("%s cluster status -c %s -d", yasbootPath, clusterName), false)

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Cluster status:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if strings.TrimSpace(line) != "" {
						ctx.Logger.Info("  %s", line)
					}
				}

				// 校验关键状态字段
				if isYACMode {
					// YAC：期望出现 open 等正常实例状态
					if !strings.Contains(result.GetStdout(), "open") {
						return fmt.Errorf("instance_status is not 'open'")
					}
				} else {
					// 单机：期望 database_status 含 normal（尽力检查）
					if !strings.Contains(result.GetStdout(), "normal") {
						ctx.Logger.Warn("database_status may not be 'normal'")
					}
				}
			}

			return nil
		},
	}
}
