// standby_gen_expansion_config.go - 生成扩容配置文件
// SE：yasboot config node gen；CE：config group gen -t ce 并修补 standby/私网

package standby

import (
	"fmt"
	"strings"
	"time"

	commonfile "github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

// stepGenExpansionConfig 生成扩容配置文件步骤
func stepGenExpansionConfig() *runner.Step {
	return &runner.Step{
		Name:        "Generate Expansion Config",
		Description: "Generate hosts_add.toml and *_add.toml (node gen or CE group gen)",
		Tags:        []string{"standby", "config"},

		PreCheck: func(ctx *runner.StepContext) error {
			// 集群名由 CLI 入口 trySync 或 Action 内 GetPrimaryEnvFile+Sync 解析，此处不强制。
			if ctx.GetParamString("os_user_password", "") == "" {
				return fmt.Errorf("os_user_password is required for yasboot config gen")
			}
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("standby_targets is required")
			}
			// 跳过 E-002 时在此解析 CE/SE，避免误走 node gen
			if err := EnsureStandbyCEPath(ctx, ""); err != nil {
				return err
			}
			if ctx.GetParamBool("standby_ce_path", false) {
				return ValidateStandbyCEParams(
					ctx.GetParamString("yac_inter_cidr", ""),
					ctx.GetParamString("yac_systemdg", ""),
					ctx.GetParamString("yac_datadg", ""),
					ctx.GetParamStringSlice("yac_vips"),
					ctx.GetParamInt("standby_node_count", len(targets)),
				)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Generate Expansion Config")
			standbyLogPhase(ctx, "config-gen-start", "expansion config")
			if err := EnsureStandbyCEPath(ctx, ""); err != nil {
				return err
			}
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)
			password := ctx.GetParamString("os_user_password", "")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
			logPath := ctx.GetParamString("db_log_path", "/data/yashan/log")
			nodeCount := ctx.GetParamInt("standby_node_count", 1)
			targetsStr := ctx.GetParamString("standby_targets_str", "")

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return fmt.Errorf("failed to sync cluster name from primary env: %w", err)
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ctx.Logger.Info("Using cluster name: %s", clusterName)

			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			ybSSHPort := ctx.YasbootRemoteSSHPort(22)

			ctx.Logger.Info("Generating expansion configuration files")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Standby targets: %s", targetsStr)
			ctx.Logger.Info("  Node count: %d", nodeCount)
			ctx.Logger.Info("  Begin port: %d", beginPort)
			ctx.Logger.Info("  Yasboot remote SSH port: %d", ybSSHPort)
			ctx.Logger.Info("  Primary user: %s", primaryUser)
			ctx.Logger.Info("  CE path: %v", ctx.GetParamBool("standby_ce_path", false))

			if ctx.GetParamBool("standby_ce_path", false) {
				if err := genCEGroupExpansionConfig(ctx, primaryUser, envFile, clusterName, stageDir, password, installPath, dataPath, logPath, targetsStr, nodeCount, beginPort, ybSSHPort); err != nil {
					return err
				}
				ctx.Results["extracted_cluster_name"] = clusterName
				standbyLogPhase(ctx, "config-gen-done", "CE group expansion config")
				return nil
			}

			// Check if host-id is provided or if we need to query it
			hostID := ctx.GetParamString("standby_host_id", "")
			targets := ctx.GetParamStringSlice("standby_targets")

			// Build yasboot config node gen command
			var genCmd string
			escapedPwd := commonos.ShellSingleQuote(password)
			if hostID != "" {
				ctx.Logger.Info("Using provided host-id: %s", hostID)
				genCmd = fmt.Sprintf(
					"cd %s && yasboot config node gen -c %s -u %s -p %s --host-ids %s --port %d --install-path %s --data-path %s --log-path %s --begin-port %d --node %d",
					stageDir, clusterName, primaryUser, escapedPwd,
					hostID,
					ybSSHPort,
					installPath, dataPath, logPath,
					beginPort,
					nodeCount)
			} else {
				genCmd = fmt.Sprintf(
					"cd %s && yasboot config node gen -c %s -u %s -p %s --ip %s --port %d --install-path %s --data-path %s --log-path %s --begin-port %d --node %d",
					stageDir, clusterName, primaryUser, escapedPwd,
					targetsStr,
					ybSSHPort,
					installPath, dataPath, logPath,
					beginPort,
					nodeCount)
			}

			extra := ctx.GetParamString(dbsteps.ParamYasbootGenExtraArgs, "")
			genCmd = dbsteps.AppendYasbootGenExtraArgs(genCmd, extra)
			if strings.TrimSpace(extra) != "" {
				ctx.Logger.Info("yasboot config node gen: appending extra args: %s", strings.TrimSpace(extra))
			}

			// Run with primary env sourced.
			ctx.Logger.Info("Running: yasboot config node gen ...")
			result, err := runYasbootOnPrimaryWithEnvFileQuiet(ctx, primaryUser, envFile, genCmd)
			if err != nil {
				// If command failed and error indicates host exists, try to get host-id and retry
				if result != nil {
					stdout := result.GetStdout()
					stderr := result.GetStderr()
					output := stdout + stderr
					if strings.Contains(output, "host") && strings.Contains(output, "exist") && strings.Contains(output, "--host-id") {
						ctx.Logger.Warn("Host exists, attempting to query host-id from cluster status")

						// Query cluster status to get host-id
						statusCmd := fmt.Sprintf("yasboot process yasagent status -c %s", clusterName)
						statusResult, statusErr := runYasbootOnPrimaryWithEnvFileQuiet(ctx, primaryUser, envFile, statusCmd)
						if statusErr == nil && statusResult != nil && statusResult.GetExitCode() == 0 {
							// Parse status output to extract host-id for the target IP
							statusOutput := statusResult.GetStdout()
							lines := strings.Split(statusOutput, "\n")
							for _, line := range lines {
								line = strings.TrimSpace(line)
								if strings.HasPrefix(line, "|") {
									parts := strings.Split(line, "|")
									if len(parts) >= 5 {
										hostIDFromStatus := strings.TrimSpace(parts[1])
										listenAddr := strings.TrimSpace(parts[4])
										// Extract IP from listen_address (format: IP:PORT)
										if idx := strings.Index(listenAddr, ":"); idx > 0 {
											ip := listenAddr[:idx]
											// Check if this IP matches any target
											for _, target := range targets {
												if ip == strings.TrimSpace(target) && hostIDFromStatus != "" {
													ctx.Logger.Info("Found host-id %s for IP %s, retrying with --host-ids", hostIDFromStatus, ip)
													genCmd = fmt.Sprintf(
														"cd %s && yasboot config node gen -c %s -u %s -p %s --host-ids %s --port %d --install-path %s --data-path %s --log-path %s --begin-port %d --node %d",
														stageDir, clusterName, primaryUser, escapedPwd,
														hostIDFromStatus,
														ybSSHPort,
														installPath, dataPath, logPath,
														beginPort,
														nodeCount)
													genCmd = dbsteps.AppendYasbootGenExtraArgs(genCmd, ctx.GetParamString(dbsteps.ParamYasbootGenExtraArgs, ""))
													result, err = commonos.ExecuteAsUserWithEnvCheck(ctx, primaryUser, envFile, genCmd, true)
													if err == nil {
														break
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
				if err != nil {
					if !runner.CommandExitLogged(err) && result != nil {
						commonos.LogTerminalCommandFailure(ctx, genCmd, result)
					}
					return fmt.Errorf("failed to generate expansion config: %w", err)
				}
			}

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Command output:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
				if strings.Contains(strings.ToLower(result.GetStdout()), "scan failed") {
					ctx.Logger.Warn("yasboot output contains scan failed: check standby SSH, ~/.yasboot leftovers, or yasboot --force hints; if E-012 fails next, it is often non-empty paths on standby - run yinstall clean on the standby with the same paths as install")
				}
			}

			ctx.Logger.Info("Expansion configuration generated successfully")

			// Store cluster name in context for PostCheck
			ctx.Results["extracted_cluster_name"] = clusterName

			standbyLogPhase(ctx, "config-gen-done", "expansion config")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// Get cluster name from context (set in Action)
			var clusterName string
			if storedName, ok := ctx.Results["extracted_cluster_name"].(string); ok && storedName != "" {
				clusterName = storedName
			} else {
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
			}

			// 同机扩容（复用 hostid）时 yasboot 只生成 <cluster>_add.toml，无 hosts_add.toml
			sameHost := strings.TrimSpace(ctx.GetParamString("standby_host_id", "")) != ""
			hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsAddFile), false)
			hostsAddOK := result != nil && result.GetExitCode() == 0
			if !hostsAddOK && !sameHost {
				return fmt.Errorf("hosts_add.toml not found at %s", hostsAddFile)
			}
			if hostsAddOK {
				ctx.Logger.Info("Found: %s", hostsAddFile)
			} else {
				ctx.Logger.Info("hosts_add.toml absent (same-host expansion with host-id=%s); skip host-add artifact check",
					ctx.GetParamString("standby_host_id", ""))
			}

			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", clusterAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("%s_add.toml not found at %s", clusterName, clusterAddFile)
			}
			ctx.Logger.Info("Found: %s", clusterAddFile)

			return nil
		},
	}
}

// genCEGroupExpansionConfig 执行 config group gen -t ce 并修补 *_add.toml。
func genCEGroupExpansionConfig(ctx *runner.StepContext, primaryUser, envFile, clusterName, stageDir, password, installPath, dataPath, logPath, targetsStr string, nodeCount, beginPort, ybSSHPort int) error {
	sysDisks, err := dbsteps.DiskPathsFromYACDG(ctx.GetParamString("yac_systemdg", ""))
	if err != nil {
		return fmt.Errorf("yac_systemdg: %w", err)
	}
	dataDisks, err := dbsteps.DiskPathsFromYACDG(ctx.GetParamString("yac_datadg", ""))
	if err != nil {
		return fmt.Errorf("yac_datadg: %w", err)
	}
	escapedPwd := commonos.ShellSingleQuote(password)
	extra := ctx.GetParamString(dbsteps.ParamYasbootGenExtraArgs, "")
	genCmd := BuildConfigGroupGenCmd(StandbyCEGroupGenParams{
		StageDir:      stageDir,
		ClusterName:   clusterName,
		User:          primaryUser,
		Password:      escapedPwd,
		IPs:           targetsStr,
		SSHPort:       ybSSHPort,
		InstallPath:   installPath,
		DataPath:      dataPath,
		LogPath:       logPath,
		BeginPort:     beginPort,
		NodeCount:     nodeCount,
		SystemDisks:   sysDisks,
		DataDisks:     dataDisks,
		DiskFoundPath: ctx.GetParamString("yac_disk_found_path", dbsteps.DefaultYACDiskFoundPath),
		VIPs:          ctx.GetParamStringSlice("yac_vips"),
		PublicNetwork: ctx.GetParamString("yac_public_network", ""),
		InterCIDR:     ctx.GetParamString("yac_inter_cidr", ""),
		ExtraArgs:     extra,
	})
	hostsAddPath := fmt.Sprintf("%s/hosts_add.toml", stageDir)
	addPath := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)

	// 扩前再探测一次已有 ceg，刷新 baseline / next group 预期
	if gRes, gErr := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile,
		fmt.Sprintf("yasboot cluster status -c %s -b group -d", clusterName), true); gErr == nil && gRes != nil && gRes.GetExitCode() == 0 {
		RecordCEGroupBaseline(ctx, gRes.GetStdout())
	}

	// 生成前备份已有产物，便于回滚对照
	if err := backupRemoteTomlIfExists(ctx, primaryUser, envFile, hostsAddPath); err != nil {
		return err
	}
	if err := backupRemoteTomlIfExists(ctx, primaryUser, envFile, addPath); err != nil {
		return err
	}

	ctx.Logger.Info("Running: yasboot config group gen -t ce ...")
	if strings.TrimSpace(extra) != "" {
		ctx.Logger.Info("yasboot config group gen: appending extra args: %s", strings.TrimSpace(extra))
	}
	result, err := runYasbootOnPrimaryWithEnvFileQuiet(ctx, primaryUser, envFile, genCmd)
	if err != nil {
		if !runner.CommandExitLogged(err) && result != nil {
			commonos.LogTerminalCommandFailure(ctx, genCmd, result)
		}
		return fmt.Errorf("failed to generate CE group expansion config: %w", err)
	}
	if result != nil && result.GetStdout() != "" {
		for _, line := range strings.Split(result.GetStdout(), "\n") {
			if line != "" {
				ctx.Logger.Info("  %s", line)
			}
		}
	}

	opt := StandbyCETomlPatchOpt{
		InterCIDR:     ctx.GetParamString("yac_inter_cidr", ""),
		PublicNetwork: ctx.GetParamString("yac_public_network", ""),
		DataPath:      dataPath,
		LogPath:       logPath,
		ReplicaPort:   dbsteps.ReplicaPort(beginPort, true),
		InterPort:     beginPort + 1,
		InterURLPort:  beginPort + 100,
	}
	// 主库 YFS/路径探测 → CONVERT/DEST/数据组名（失败硬失败）
	layout, err := probePrimaryYFSLayout(ctx, primaryUser, envFile, clusterName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(layout.DataDG) == "" {
		ctx.Logger.Info("CE YFS probe: no +diskgroup data paths on primary; skip CONVERT/DEST diskgroup patch")
	} else {
		yfsPatch := DeriveStandbyCEYFSPatch(layout, StandbyYFSAvailability{})
		if len(layout.DataDGs) > 1 {
			ctx.Logger.Warn("CE YFS: primary has multiple data diskgroups %v; align standby to majority %s and map extras via DB_FILE_NAME_CONVERT=%q",
				layout.DataDGs, yfsPatch.DataDiskgroup, yfsPatch.DBFileNameConvert)
		}
		opt.ApplyYFSPatch = true
		opt.DataDiskgroupName = yfsPatch.DataDiskgroup
		opt.ArchiveLocalDest = yfsPatch.ArchiveLocalDest
		opt.RedoFileNameConvert = yfsPatch.RedoFileNameConvert
		opt.DBFileNameConvert = yfsPatch.DBFileNameConvert
		if ctx.Params == nil {
			ctx.Params = map[string]interface{}{}
		}
		ctx.Params["ce_primary_data_dg"] = layout.DataDG
		ctx.Params["ce_primary_data_dgs"] = strings.Join(layout.DataDGs, ",")
		ctx.Params["ce_primary_redo_dg"] = layout.RedoDG
		ctx.Params["ce_standby_data_dg"] = yfsPatch.DataDiskgroup
		ctx.Params["ce_standby_archive_local_dest"] = yfsPatch.ArchiveLocalDest
		ctx.Params["ce_redo_file_name_convert"] = yfsPatch.RedoFileNameConvert
		ctx.Params["ce_db_file_name_convert"] = yfsPatch.DBFileNameConvert
		ctx.Logger.Info("CE YFS probe: data_dg=%s data_dgs=%v redo_dg=%s -> archive=%s redo_convert=%q db_convert=%q",
			yfsPatch.DataDiskgroup, layout.DataDGs, layout.RedoDG, yfsPatch.ArchiveLocalDest,
			yfsPatch.RedoFileNameConvert, yfsPatch.DBFileNameConvert)
	}
	var patchedAdd string
	if err := patchAndWriteRemoteToml(ctx, primaryUser, envFile, addPath, func(raw string) (string, error) {
		p, err := PatchStandbyCEAddTOML(raw, opt)
		if err != nil {
			return "", err
		}
		patchedAdd = p
		return p, nil
	}); err != nil {
		return err
	}
	if gn := GroupNameFromAddTOML(patchedAdd); gn != "" {
		ctx.Params["ce_new_group_name"] = gn
		ctx.Logger.Info("CE new group name from add.toml: %s (expected was %s)",
			gn, ctx.GetParamString("ce_expected_new_group", ""))
	}
	ctx.Logger.Info("Patched CE %s_add.toml for standby role and private interconnect/replication", clusterName)

	// hosts_add.toml：补齐私网 yasdb_ip（与 add.toml 一并处理）
	if exists, _ := remoteFileExists(ctx, hostsAddPath); exists {
		if err := patchAndWriteRemoteToml(ctx, primaryUser, envFile, hostsAddPath, func(raw string) (string, error) {
			return PatchStandbyCEHostsAddTOML(raw, opt.InterCIDR)
		}); err != nil {
			return err
		}
		ctx.Logger.Info("Patched CE hosts_add.toml private yasdb_ip: %s", hostsAddPath)
	} else {
		ctx.Logger.Info("hosts_add.toml not present after group gen (skip hosts patch)")
	}
	ctx.Logger.Info("CE group expansion configuration generated successfully")
	return nil
}

// probePrimaryYFSLayout 在主库查询 datafile/redo/arch/diskgroup；SQL 失败则硬失败。
func probePrimaryYFSLayout(ctx *runner.StepContext, primaryUser, envFile, clusterName string) (PrimaryYFSLayout, error) {
	sql := strings.Join([]string{
		`SELECT name FROM v$datafile ORDER BY id;`,
		`SELECT name FROM v$logfile ORDER BY thread#, id;`,
		`SHOW PARAMETER ARCHIVE_LOCAL_DEST;`,
		`SELECT name FROM v$yfs_diskgroup ORDER BY 1;`,
	}, "\n")
	res, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
	if err != nil {
		return PrimaryYFSLayout{}, fmt.Errorf("probe primary YFS layout: %w", err)
	}
	stdout := ""
	if res != nil {
		stdout = res.Stdout
	}
	layout, err := ParsePrimaryYFSProbe(stdout)
	if err != nil {
		return PrimaryYFSLayout{}, fmt.Errorf("parse primary YFS layout: %w", err)
	}
	return layout, nil
}

// backupRemoteTomlIfExists 若远端文件存在则 cp -a 为 path.bak.<timestamp>。
func backupRemoteTomlIfExists(ctx *runner.StepContext, primaryUser, envFile, path string) error {
	exists, err := remoteFileExists(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	bak := fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102150405"))
	cmd := fmt.Sprintf("cp -a %s %s", commonos.ShellSingleQuote(path), commonos.ShellSingleQuote(bak))
	if _, err := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, cmd, true); err != nil {
		// root 写的备份也可能：用 ExecuteWithCheck
		if _, err2 := ctx.ExecuteWithCheck(cmd, true); err2 != nil {
			return fmt.Errorf("backup %s -> %s: %v / %v", path, bak, err, err2)
		}
	}
	ctx.Logger.Info("Backed up existing toml: %s -> %s", path, bak)
	return nil
}

func remoteFileExists(ctx *runner.StepContext, path string) (bool, error) {
	res, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(path)), false)
	if res == nil {
		return false, nil
	}
	return res.GetExitCode() == 0, nil
}

func patchAndWriteRemoteToml(ctx *runner.StepContext, primaryUser, envFile, path string, patchFn func(string) (string, error)) error {
	catCmd := fmt.Sprintf("cat %s", commonos.ShellSingleQuote(path))
	catRes, catErr := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, catCmd, true)
	if catErr != nil {
		return fmt.Errorf("read %s: %w", path, catErr)
	}
	patched, pErr := patchFn(catRes.GetStdout())
	if pErr != nil {
		return fmt.Errorf("patch %s: %w", path, pErr)
	}
	if !strings.HasSuffix(patched, "\n") {
		patched += "\n"
	}
	if err := commonfile.RemoteWriteTextFile(ctx, path, patched, false); err != nil {
		return fmt.Errorf("write patched %s: %w", path, err)
	}
	chownCmd := fmt.Sprintf("chown %s %s", commonos.ShellSingleQuote(primaryUser), commonos.ShellSingleQuote(path))
	if _, err := ctx.ExecuteWithCheck(chownCmd, true); err != nil {
		return fmt.Errorf("chown patched %s to %s: %w", path, primaryUser, err)
	}
	return nil
}
