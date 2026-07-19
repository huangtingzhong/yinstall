package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepDeployDatabase 部署数据库集群（yasboot cluster deploy）
func stepDeployDatabase() *runner.Step {
	return &runner.Step{
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
					StepName:    "Deploy Database",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityInfo,
					Code:        "PC.DB.FORCE_MODE",
					Message:     fmt.Sprintf("Detected %s: apply will perform destructive cleanup (cluster clean / shared-disk header wipe / password-file removal). Precheck will not execute these actions.", ctx.ForceStepsHint()),
					Remediation: "Confirm this is intended; take backups and double-check disk parameters before applying.",
				})
			}

			// 注意：此处不要清理 .yasboot 产物！
			// 这些文件由 C-020（Install Software）生成，部署阶段仍需要。
			// 旧安装残留清理应仅在 C-020 的 PreCheck/Action 策略中处理。

			user := ctx.GetParamString("os_user", "yashan")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			if err := precheckClusterNotAlreadyDeployed(ctx, user, yasbootPath, clusterName); err != nil {
				return err
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-021: Deploy Database")
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			adminPassword := ctx.GetParamString("db_admin_password", "")
			user := ctx.GetParamString("os_user", "yashan")
			isYACMode := ctx.GetParamBool("yac_mode", false)
			isForce := ctx.IsForceStep()

			yasbootPath := path.Join(stageDir, "bin/yasboot")
			configPath := path.Join(stageDir, clusterName+".toml")

			// 检查配置文件属主和权限，仅在不符合要求时修复
			if err := ensureConfigFileOwnership(ctx, configPath, user); err != nil {
				return fmt.Errorf("failed to ensure config file ownership: %w", err)
			}

			// force 模式下的破坏性清理（写操作）只能放在 Action，不能放在 PreCheck。
			if isForce {
				dbLogPhase(ctx, "force-clean-start", fmt.Sprintf("cluster=%s yac=%v", clusterName, isYACMode))
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
						allDisks = append(allDisks, DiskPathListFromYACDG(dgStr)...)
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
						dbLogPhase(ctx, "wipe-disk-start", fmt.Sprintf("disks=%d", len(uniqueDisks)))
						firstHost := ctx.TargetHosts[0]
						firstHctx := ctx.ForHost(firstHost)
						ctx.Logger.Info("Wiping YFS metadata on %d shared disks from node %s (shared disks only need one node)...", len(uniqueDisks), firstHost.Host)
						for _, disk := range uniqueDisks {
							if !commonos.IsSafeUnixBlockDevicePath(disk) {
								ctx.Logger.Warn("  [%s] Skipping unsafe disk path for dd: %s", firstHost.Host, disk)
								continue
							}
							diskQ := commonos.ShellSingleQuote(disk)
							ddCmd := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 conv=notrunc 2>/dev/null", diskQ)
							ddResult, _ := firstHctx.Execute(ddCmd, true)
							if ddResult != nil && ddResult.GetExitCode() == 0 {
								ctx.Logger.Info("  [%s] Wiped header: %s", firstHost.Host, disk)
							} else {
								ctx.Logger.Warn("  [%s] Failed to wipe %s", firstHost.Host, disk)
							}
						}
						dbLogPhase(ctx, "wipe-disk-done", fmt.Sprintf("disks=%d", len(uniqueDisks)))
					}
				}

				dataClean := path.Clean(strings.ReplaceAll(dataPath, `\`, `/`))
				dataQ := commonos.ShellSingleQuote(dataClean)

				// 3. Clean password files
				if isYACMode {
					ctx.Logger.Info("YAC mode: cleaning password files on all nodes")
					for _, th := range ctx.TargetHosts {
						hctx := ctx.ForHost(th)
						findCmd := fmt.Sprintf("find %s -type f -name 'yasdb.pwd' 2>/dev/null", dataQ)
						res, _ := hctx.Execute(findCmd, false)
						if res != nil && res.GetStdout() != "" {
							pwdFiles := strings.Split(strings.TrimSpace(res.GetStdout()), "\n")
							for _, pwdFile := range pwdFiles {
								pwdFile = strings.TrimSpace(pwdFile)
								if pwdFile == "" {
									continue
								}
								pc := path.Clean(strings.ReplaceAll(pwdFile, `\`, `/`))
								if !commonos.DeletePathUnder(pc, dataClean) {
									ctx.Logger.Warn("Skipping pwd path outside db_data_path on %s: %s", th.Host, pwdFile)
									continue
								}
								if err := commonos.ValidateDeletePath(pc); err != nil {
									ctx.Logger.Warn("Skipping pwd path failed delete validation on %s: %s (%v)", th.Host, pwdFile, err)
									continue
								}
								ctx.Logger.Info("Removing password file on %s: %s", th.Host, pwdFile)
								hctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(pc)), true)
							}
						}
					}
				} else {
					ctx.Logger.Info("Standalone mode: cleaning password file on current node")
					findCmd := fmt.Sprintf("find %s -type f -name 'yasdb.pwd' 2>/dev/null", dataQ)
					res, _ := ctx.Execute(findCmd, false)
					if res != nil && res.GetStdout() != "" {
						pwdFiles := strings.Split(strings.TrimSpace(res.GetStdout()), "\n")
						for _, pwdFile := range pwdFiles {
							pwdFile = strings.TrimSpace(pwdFile)
							if pwdFile == "" {
								continue
							}
							pc := path.Clean(strings.ReplaceAll(pwdFile, `\`, `/`))
							if !commonos.DeletePathUnder(pc, dataClean) {
								ctx.Logger.Warn("Skipping pwd path outside db_data_path: %s", pwdFile)
								continue
							}
							if err := commonos.ValidateDeletePath(pc); err != nil {
								ctx.Logger.Warn("Skipping pwd path failed delete validation: %s (%v)", pwdFile, err)
								continue
							}
							ctx.Logger.Info("Removing password file: %s", pwdFile)
							ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(pc)), true)
						}
					}
				}

				ctx.Logger.Info("Force mode cleanup completed")
				dbLogPhase(ctx, "force-clean-done", clusterName)
			}

			ctx.Logger.Info("Deploying database cluster: %s", clusterName)

			// 组装 deploy 命令（日志中掩码密码）
			// YAC 模式需要 --yfs-force-create，以便在共享盘上强制创建 YFS
			deployExtra := ctx.GetParamString(ParamYasbootDeployExtraArgs, "")
			if isYACMode {
				ctx.Logger.Info("YAC mode detected: adding --yfs-force-create parameter")
			}
			deployCmd := BuildClusterDeployInner(yasbootPath, configPath, "***", isYACMode, deployExtra)
			ctx.Logger.Info("Command (run as %s): %s", user, deployCmd)

			inner := BuildClusterDeployInner(yasbootPath, configPath, adminPassword, isYACMode, deployExtra)
			if strings.TrimSpace(deployExtra) != "" {
				ctx.Logger.Info("yasboot cluster deploy: appending extra args: %s", strings.TrimSpace(deployExtra))
			}
			cmd := fmt.Sprintf("cd %s && %s", stageDir, inner)

			dbLogPhase(ctx, "deploy-start", fmt.Sprintf("cluster=%s yac=%v", clusterName, isYACMode))
			if _, err := commonos.ExecuteAsUserWithCheck(ctx, user, cmd, true); err != nil {
				dbLogPhase(ctx, "deploy-fail", runner.TruncateForLog(err.Error(), 120))
				return fmt.Errorf("failed to deploy database: %w", err)
			}
			dbLogPhase(ctx, "deploy-done", clusterName)

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

// precheckClusterNotAlreadyDeployed 只读：yasboot cluster status 显示已 OPEN/normal 则拒绝（除非 force）。
func precheckClusterNotAlreadyDeployed(ctx *runner.StepContext, user, yasbootPath, clusterName string) error {
	ybRes, _ := ctx.Execute(fmt.Sprintf("test -x %s && echo OK", commonos.ShellSingleQuote(yasbootPath)), false)
	if ybRes == nil || !strings.Contains(ybRes.GetStdout(), "OK") {
		return nil // 无 yasboot 时交给上游/Action
	}
	result, _ := commonos.ExecuteAsUser(ctx, user, fmt.Sprintf("%s cluster status -c %s -d", yasbootPath, clusterName), false)
	if result == nil || result.GetExitCode() != 0 {
		return nil
	}
	out := result.GetStdout()
	if strings.TrimSpace(out) == "" {
		return nil
	}
	looksLive := strings.Contains(out, "open") || strings.Contains(strings.ToLower(out), "normal")
	if !looksLive {
		return nil
	}
	if ctx.IsForceStep() {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName:    "Deploy Database",
			Host:        ctx.Executor.Host(),
			Severity:    runner.PrecheckSeverityInfo,
			Code:        "PC.DB.ALREADY_DEPLOYED_FORCE",
			Message:     fmt.Sprintf("cluster %s appears already deployed (status shows open/normal); apply with %s will clean then redeploy", clusterName, ctx.ForceStepsHint()),
			Remediation: "Use yinstall clean first for a safer wipe, or proceed with force only if intentional.",
		})
		return nil
	}
	return fmt.Errorf("cluster %s appears already deployed (yasboot cluster status shows open/normal); run yinstall clean first or use %s to force redeploy", clusterName, ctx.ForceStepsHint())
}

// ensureConfigFileOwnership 检查配置文件的属主和权限，仅在不符合要求时修复。
// 属主不是 targetUser 则 chown；文件不可读则 chmod。
func ensureConfigFileOwnership(ctx *runner.StepContext, filePath, targetUser string) error {
	// 查询文件属主和权限
	statFmt := `%U %a`
	statCmd := fmt.Sprintf("stat -c '%s' %s 2>/dev/null", statFmt, commonos.ShellSingleQuote(filePath))
	result, err := ctx.Execute(statCmd, false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return fmt.Errorf("cannot stat %s: %v", filePath, err)
	}

	fields := strings.Fields(strings.TrimSpace(result.GetStdout()))
	if len(fields) < 2 {
		return fmt.Errorf("unexpected stat output for %s: %s", filePath, result.GetStdout())
	}

	owner := fields[0]
	perm := fields[1]

	needFix := false

	// 检查属主
	if owner != targetUser {
		ctx.Logger.Info("Config file %s owner is %s, expected %s — fixing with chown", filePath, owner, targetUser)
		chownCmd := fmt.Sprintf("chown %s:%s %s", targetUser, targetUser, commonos.ShellSingleQuote(filePath))
		if r, err := ctx.Execute(chownCmd, true); err != nil || (r != nil && r.GetExitCode() != 0) {
			return fmt.Errorf("chown %s failed: %v", filePath, err)
		}
		needFix = true
	} else {
		ctx.Logger.Info("Config file %s owner OK (%s)", filePath, owner)
	}

	// 检查权限：至少需要属主可读（r—— = 4xx）
	// perm 是八进制字符串如 "0644" 或 "644"
	permInt := 0
	for _, ch := range perm {
		if ch >= '0' && ch <= '7' {
			permInt = permInt*8 + int(ch-'0')
		}
	}
	// 取最后3位八进制（owner bits）
	ownerBits := (permInt >> 6) & 7
	if ownerBits&4 == 0 {
		ctx.Logger.Info("Config file %s permission %s lacks owner read — fixing with chmod", filePath, perm)
		chmodCmd := fmt.Sprintf("chmod 644 %s", commonos.ShellSingleQuote(filePath))
		if r, err := ctx.Execute(chmodCmd, true); err != nil || (r != nil && r.GetExitCode() != 0) {
			return fmt.Errorf("chmod %s failed: %v", filePath, err)
		}
		needFix = true
	} else {
		ctx.Logger.Info("Config file %s permission OK (%s)", filePath, perm)
	}

	if needFix {
		ctx.Logger.Info("Config file %s ownership/permissions fixed", filePath)
	}

	return nil
}
