// standby_check_replication_addr.go - 检查主库 replication_addr 参数
// SE：校验非空；CE：按 --yac-inter-cidr 自动写 SPFILE 并整集群重启

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

// stepCheckReplicationAddr 检查主库 replication_addr 参数步骤
func stepCheckReplicationAddr() *runner.Step {
	return &runner.Step{
		Name:        "Check Replication Address",
		Description: "Verify primary REPLICATION_ADDR; CE may plan private addr (restart only with --standby-restart-primary)",
		Tags:        []string{"standby", "primary", "replication"},

		PreCheck: func(ctx *runner.StepContext) error {
			return precheckReplicationAddr(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Replication Address")
			return applyReplicationAddr(ctx)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// precheckReplicationAddr 只读预检：SE 校验已配置；CE 评估 pending（--precheck 时走与 dry-run 相同报告，不写 SPFILE）。
func precheckReplicationAddr(ctx *runner.StepContext) error {
	if strings.TrimSpace(ctx.GetParamString("primary_env_file", "")) == "" &&
		strings.TrimSpace(ctx.GetParamString("db_cluster_name", "")) == "" {
		return fmt.Errorf("db_cluster_name is required unless primary_env_file is set")
	}
	standbyLogPhase(ctx, "check-start", "REPLICATION_ADDR")
	primaryUser := GetPrimaryOSUser(ctx)
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("failed to get primary environment file: %w", err)
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		return err
	}
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	ctx.Logger.Info("Checking primary database REPLICATION_ADDR (cluster=%s user=%s)", clusterName, primaryUser)
	if err := EnsureStandbyCEPath(ctx, ""); err != nil {
		return err
	}
	if ctx.GetParamBool("standby_ce_path", false) {
		// 只读规划（不写 SPFILE）；与 ensureCE 在 DryRun/Precheck 下行为一致
		err := ensureCEPrimaryReplicationAddrsMode(ctx, primaryUser, envFile, clusterName, true)
		if err != nil {
			// 正常安装：pending 需写 SPFILE 时 PreCheck 只告警，交给 Action 真正写入
			if !ctx.Precheck && !ctx.DryRun && strings.Contains(err.Error(), "REPLICATION_ADDR not yet effective") {
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepName:    "Check Replication Address",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityInfo,
					Code:        "PC.STANDBY.REPLICATION_ADDR_PENDING",
					Message:     err.Error(),
					Remediation: "Apply will write SPFILE; use --standby-restart-primary to restart primary in the same run, or restart manually then re-run.",
				})
				return nil
			}
			return err
		}
		return nil
	}
	return checkSEReplicationAddrConfigured(ctx, primaryUser, envFile, clusterName)
}

func applyReplicationAddr(ctx *runner.StepContext) error {
	standbyLogPhase(ctx, "check-start", "REPLICATION_ADDR")
	primaryUser := GetPrimaryOSUser(ctx)
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("failed to get primary environment file: %w", err)
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		return err
	}
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	ctx.Logger.Info("Checking primary database REPLICATION_ADDR (cluster=%s user=%s)", clusterName, primaryUser)
	if err := EnsureStandbyCEPath(ctx, ""); err != nil {
		return err
	}
	if ctx.GetParamBool("standby_ce_path", false) {
		return ensureCEPrimaryReplicationAddrsMode(ctx, primaryUser, envFile, clusterName, false)
	}
	return checkSEReplicationAddrConfigured(ctx, primaryUser, envFile, clusterName)
}

func checkSEReplicationAddrConfigured(ctx *runner.StepContext, primaryUser, envFile, clusterName string) error {
	ctx.Logger.Info("Querying REPLICATION_ADDR parameter...")
	sql := "SELECT name, value FROM v$parameter WHERE name = 'REPLICATION_ADDR';"
	result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
	if err != nil {
		return fmt.Errorf("failed to query REPLICATION_ADDR: %w", err)
	}
	ctx.Logger.Info("Query result:\n%s", result.Stdout)

	replicationAddr, isConfigured := ParseReplicationAddrValue(result.Stdout)
	if !isConfigured {
		ctx.Logger.Error("ERROR: REPLICATION_ADDR parameter is NOT configured.")
		ctx.Logger.Error("REPLICATION_ADDR is REQUIRED for standby to reach the primary.")
		ctx.Logger.Error("Configure on the primary: ALTER SYSTEM SET REPLICATION_ADDR = '<IP>:<PORT>';")
		return fmt.Errorf("REPLICATION_ADDR parameter is not configured")
	}
	ctx.Logger.Info("OK: Primary database REPLICATION_ADDR is configured: %s", replicationAddr)
	ctx.SetResult("primary_replication_addr", replicationAddr)
	standbyLogPhase(ctx, "check-done", fmt.Sprintf("addr=%s", runner.TruncateForLog(replicationAddr, 60)))
	return nil
}

func ensureCEPrimaryReplicationAddrs(ctx *runner.StepContext, primaryUser, envFile, clusterName string) error {
	return ensureCEPrimaryReplicationAddrsMode(ctx, primaryUser, envFile, clusterName, false)
}

// ensureCEPrimaryReplicationAddrsMode planOnly=true 时只规划/报告，不写 SPFILE、不重启。
func ensureCEPrimaryReplicationAddrsMode(ctx *runner.StepContext, primaryUser, envFile, clusterName string, planOnly bool) error {
	interCIDR := strings.TrimSpace(ctx.GetParamString("yac_inter_cidr", ""))
	if interCIDR == "" {
		return fmt.Errorf("--yac-inter-cidr is required to set primary REPLICATION_ADDR on CE path")
	}
	sysPass := strings.TrimSpace(ctx.GetParamString("db_admin_password", ""))
	if err := RequireCEAdminPassword(sysPass); err != nil {
		return err
	}
	beginPort := ctx.GetParamInt("db_begin_port", 1688)
	repPort := dbsteps.ReplicaPort(beginPort, true)
	osPassword := ctx.GetParamString("os_user_password", "")
	ybSSHPort := ctx.YasbootRemoteSSHPort(22)

	statusRes, err := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile,
		fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
	if err != nil {
		return fmt.Errorf("cluster status for REPLICATION_ADDR plan: %w", err)
	}
	rows := dbsteps.ParseClusterStatusTable(statusRes.GetStdout())
	stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
	hostsToml := ""
	for _, name := range []string{"hosts.toml", clusterName + ".toml"} {
		cat := fmt.Sprintf("test -f %s/%s && cat %s/%s || true", stageDir, name, stageDir, name)
		if r, _ := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, cat, true); r != nil {
			hostsToml += r.GetStdout() + "\n"
		}
	}

	var plans []ReplicationAddrPlan
	for _, row := range rows {
		role := strings.ToLower(row.DatabaseRole)
		if role != "" && role != "primary" {
			continue
		}
		listenIP, _, _ := splitHostPortLoose(row.ListenAddress)
		tomlInter := InterIPFromHostsTOML(hostsToml, row.Hostid)
		probed := probeHostIPv4List(ctx, primaryUser, osPassword, listenIP, ybSSHPort)
		interIP, rErr := ResolveNodeInterIP(row.Hostid, listenIP, interCIDR, tomlInter, probed)
		if rErr != nil {
			return rErr
		}
		plan, pErr := PlanPrimaryReplicationAddrs(row.Hostid, row.Nodeid, listenIP, interIP, interCIDR, repPort)
		if pErr != nil {
			return pErr
		}
		plans = append(plans, plan)
		ctx.Logger.Info("REPLICATION_ADDR plan: node=%s host=%s toml=%s probed=%v -> %s",
			plan.Nodeid, plan.Hostid, tomlInter, probed, plan.Addr)
	}
	if len(plans) == 0 {
		return fmt.Errorf("no primary nodes found in cluster status to set REPLICATION_ADDR")
	}

	allowRestart := ctx.GetParamBool("standby_restart_primary", false)
	var pending []ReplicationAddrPlan
	var pendingCur []string
	for _, plan := range plans {
		listenIP := ""
		for _, row := range rows {
			if row.Nodeid == plan.Nodeid || row.Hostid == plan.Hostid {
				listenIP, _, _ = splitHostPortLoose(row.ListenAddress)
				break
			}
		}
		if listenIP == "" {
			return fmt.Errorf("listen IP not found for node %s", plan.Nodeid)
		}
		cur, qErr := queryReplicationAddrTCP(ctx, primaryUser, envFile, sysPass, listenIP, beginPort)
		if qErr != nil {
			ctx.Logger.Warn("query REPLICATION_ADDR via TCP %s:%d failed: %v; treat as unset", listenIP, beginPort, qErr)
			cur = ""
		}
		if strings.EqualFold(strings.TrimSpace(cur), plan.Addr) {
			ctx.Logger.Info("REPLICATION_ADDR already %s on %s, skip", plan.Addr, plan.Nodeid)
			continue
		}
		pending = append(pending, plan)
		pendingCur = append(pendingCur, cur)
		ctx.Logger.Info("REPLICATION_ADDR change needed: node=%s current=%q -> %s", plan.Nodeid, cur, plan.Addr)
	}

	if len(pending) == 0 {
		// 全部已生效，无需写 SPFILE / 重启
		sample := plans[0].Addr
		ctx.SetResult("primary_replication_addr", sample)
		standbyLogPhase(ctx, "check-done", fmt.Sprintf("already ok addr=%s", sample))
		ctx.Logger.Info("OK: CE primary REPLICATION_ADDR already effective (no SPFILE write, no restart)")
		return nil
	}

	if planOnly || ctx.DryRun || ctx.Precheck {
		for i, plan := range pending {
			if allowRestart {
				ctx.Logger.Info("[dry-run/precheck] would set REPLICATION_ADDR=%s SCOPE=SPFILE on %s (current=%q) then restart primary (--standby-restart-primary)",
					plan.Addr, plan.Nodeid, pendingCur[i])
			} else {
				ctx.Logger.Info("[dry-run/precheck] would set REPLICATION_ADDR=%s SCOPE=SPFILE on %s (current=%q); will NOT restart (default)",
					plan.Addr, plan.Nodeid, pendingCur[i])
			}
		}
		if !allowRestart {
			standbyLogPhase(ctx, "check-done", "ce plan: SPFILE only, restart deferred")
			return fmt.Errorf("REPLICATION_ADDR not yet effective; apply will write SPFILE then stop until primary is restarted (or pass --standby-restart-primary)")
		}
		standbyLogPhase(ctx, "check-done", "ce plan: SPFILE+restart")
		return nil
	}

	// 默认也写 SPFILE（生产可先落盘，维护窗口再重启）
	for _, plan := range pending {
		listenIP := ""
		for _, row := range rows {
			if row.Nodeid == plan.Nodeid || row.Hostid == plan.Hostid {
				listenIP, _, _ = splitHostPortLoose(row.ListenAddress)
				break
			}
		}
		alterSQL := fmt.Sprintf("ALTER SYSTEM SET REPLICATION_ADDR='%s' SCOPE=SPFILE;", plan.Addr)
		standbyLogPhase(ctx, "replication-addr-start", plan.Nodeid)
		if err := runSysSQLTCP(ctx, primaryUser, envFile, sysPass, listenIP, beginPort, alterSQL); err != nil {
			return fmt.Errorf("set REPLICATION_ADDR on %s (%s): %w", plan.Nodeid, listenIP, err)
		}
		ctx.Logger.Info("Set REPLICATION_ADDR=%s SCOPE=SPFILE on %s", plan.Addr, plan.Nodeid)
	}

	if allowRestart {
		standbyLogPhase(ctx, "cluster-restart", clusterName)
		ctx.Logger.Info("Restarting primary cluster for REPLICATION_ADDR SPFILE (--standby-restart-primary)...")
		if _, err := runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster stop -c %s", clusterName)); err != nil {
			return fmt.Errorf("cluster stop after REPLICATION_ADDR: %w", err)
		}
		if _, err := runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster start -c %s", clusterName)); err != nil {
			return fmt.Errorf("cluster start after REPLICATION_ADDR: %w", err)
		}
	} else {
		var detail strings.Builder
		for i, plan := range pending {
			fmt.Fprintf(&detail, "\n  - node %s: was=%q spfile_now=%s", plan.Nodeid, pendingCur[i], plan.Addr)
		}
		msg := fmt.Sprintf(
			"REPLICATION_ADDR written to SPFILE on primary, but restart is disabled by default (production-safe)."+
				" Effective in-memory value is unchanged until primary cluster restart.%s\n"+
				"Next steps:\n"+
				"  1) In a maintenance window: yasboot cluster stop -c %s && yasboot cluster start -c %s\n"+
				"  2) Re-run yinstall standby (SPFILE already set; E-004 will pass without rewrite)\n"+
				"Or pass --standby-restart-primary to let yinstall restart the primary cluster now.",
			detail.String(), clusterName, clusterName)
		ctx.Logger.Warn("%s", msg)
		return fmt.Errorf("REPLICATION_ADDR SPFILE updated; primary restart required before CE standby expansion (default: no auto-restart)")
	}

	// 校验：重启后至少能看到一个已配置值
	result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName,
		"SELECT name, value FROM v$parameter WHERE name = 'REPLICATION_ADDR';", true)
	if err != nil {
		return fmt.Errorf("verify REPLICATION_ADDR after ensure: %w", err)
	}
	addr, ok := ParseReplicationAddrValue(result.Stdout)
	if !ok {
		return fmt.Errorf("REPLICATION_ADDR still empty after CE ensure")
	}
	ctx.SetResult("primary_replication_addr", addr)
	standbyLogPhase(ctx, "replication-addr-done", addr)
	ctx.Logger.Info("OK: CE primary REPLICATION_ADDR configured (sample=%s)", addr)
	return nil
}

// probeHostIPv4List 从 OM 探测目标主机 IPv4 列表（优先本机 hostname -I，否则 sshpass+ssh）。
func probeHostIPv4List(ctx *runner.StepContext, osUser, osPassword, host string, sshPort int) []string {
	host = strings.TrimSpace(host)
	if host == "" || ctx == nil {
		return nil
	}
	localIP := ""
	if ctx.Executor != nil {
		localIP = strings.TrimSpace(ctx.Executor.Host())
	}
	var out string
	if localIP != "" && (host == localIP || host == "127.0.0.1" || host == "localhost") {
		res, _ := ctx.Execute("hostname -I 2>/dev/null || true", false)
		if res != nil {
			out = res.GetStdout()
		}
	} else if strings.TrimSpace(osPassword) != "" {
		cmd := fmt.Sprintf(
			`command -v sshpass >/dev/null 2>&1 && sshpass -p %s ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 -p %d %s@%s "hostname -I" 2>/dev/null || true`,
			commonos.ShellSingleQuote(osPassword), sshPort, osUser, host)
		res, _ := ctx.Execute(cmd, false)
		if res != nil {
			out = res.GetStdout()
		}
	}
	var ips []string
	for _, f := range strings.Fields(out) {
		f = strings.TrimSpace(f)
		if f != "" {
			ips = append(ips, f)
		}
	}
	return ips
}

func queryReplicationAddrTCP(ctx *runner.StepContext, osUser, envFile, sysPass, host string, port int) (string, error) {
	if strings.TrimSpace(sysPass) == "" {
		// 无 sys 密码时退回本地 sysdba（仅当前 OM/primary 实例）
		res, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, osUser, envFile, ctx.GetParamString("db_cluster_name", "yashandb"),
			"SELECT name, value FROM v$parameter WHERE name = 'REPLICATION_ADDR';", true)
		if err != nil {
			return "", err
		}
		addr, _ := ParseReplicationAddrValue(res.Stdout)
		return addr, nil
	}
	sql := "SELECT name, value FROM v$parameter WHERE name = 'REPLICATION_ADDR';"
	out, err := runSysSQLTCPOutput(ctx, osUser, envFile, sysPass, host, port, sql)
	if err != nil {
		return "", err
	}
	addr, _ := ParseReplicationAddrValue(out)
	return addr, nil
}

func runSysSQLTCP(ctx *runner.StepContext, osUser, envFile, sysPass, host string, port int, sql string) error {
	_, err := runSysSQLTCPOutput(ctx, osUser, envFile, sysPass, host, port, sql)
	return err
}

func runSysSQLTCPOutput(ctx *runner.StepContext, osUser, envFile, sysPass, host string, port int, sql string) (string, error) {
	if strings.TrimSpace(sysPass) == "" {
		return "", fmt.Errorf("--db-admin-password is required to set REPLICATION_ADDR on each CE primary instance via TCP")
	}
	connect := commonsql.BuildYasqlTCPConnect(host, "sys", sysPass, port, "")
	// 去掉末尾多余 / 若 service 为空
	connect = strings.TrimSuffix(connect, "/")
	script := strings.TrimSpace(sql)
	if !strings.HasSuffix(script, ";") {
		script += ";"
	}
	cmd := fmt.Sprintf("yasql %s <<'EOSQL'\n%s\nEOSQL", connect, script)
	res, err := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, osUser, envFile, cmd, true)
	if err != nil {
		return "", err
	}
	return res.GetStdout() + "\n" + res.GetStderr(), nil
}
