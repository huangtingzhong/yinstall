// clean_db_detach_standby.go - 备库 clean 前从集群摘除节点
// 在本机 CLEAN-DB 杀进程/删目录前执行 yasboot node remove --purge，并按状态收敛。
// 本机无集群 env 时，可 SSH 到 OM(om_addr / 全局 -M/--om + 全局 SSH 凭证)代跑 remove。

package clean

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	dbsteps "github.com/yinstall/internal/steps/db"
	standbysteps "github.com/yinstall/internal/steps/standby"
)

const (
	resultKeyCleanDetachedNode  = "clean_detached_node_id"
	resultKeyCleanDetachedHost  = "clean_detached_host_id"
	resultKeyCleanDetachSkipped = "clean_detach_skipped"
	resultKeyCleanDetachReason  = "clean_detach_skip_reason"
)

// StepCleanDBDetachStandby 若目标为本集群备库，先 node remove 再继续本机清理。
func StepCleanDBDetachStandby() *runner.Step {
	return &runner.Step{
		Name:        "Detach Standby From Cluster",
		Description: "Remove standby node from cluster via yasboot node remove before local wipe",
		Tags:        []string{"clean", "db", "standby", "detach"},
		Optional:    false,
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			reportCleanDetachImpact(ctx)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			if ctx.Results == nil {
				ctx.Results = make(map[string]interface{})
			}
			if ctx.GetParamBool("skip_cluster_detach", false) {
				ctx.Results[resultKeyCleanDetachSkipped] = true
				ctx.Results[resultKeyCleanDetachReason] = "skip_cluster_detach"
				ctx.Logger.Info("Skipping cluster detach (--skip-cluster-detach)")
				return nil
			}

			osUser := ctx.GetParamString("os_user", "yashan")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			sysPassword := ctx.GetParamString("db_admin_password", "")
			targetIP := cleanTargetIP(ctx)
			if targetIP == "" {
				return fmt.Errorf("cannot determine clean target IP for cluster detach")
			}

			envFile, err := resolveDetachYasbootEnv(ctx, osUser, clusterName)
			if err == nil {
				err = runDetachOnCtx(ctx, osUser, envFile, clusterName, sysPassword, targetIP)
				if err == nil {
					return nil
				}
				if !isDetachRetryableLocalFailure(err) {
					return maybeDegradeDetachForForcePrimary(ctx, err)
				}
				ctx.Logger.Warn("Local cluster detach failed (%v); trying OM SSH fallback", err)
			} else {
				ctx.Logger.Warn("Cluster detach env unavailable on target (%v); trying OM SSH fallback", err)
			}

			err = detachViaOmSSH(ctx, osUser, clusterName, sysPassword, targetIP)
			return maybeDegradeDetachForForcePrimary(ctx, err)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

func cleanTargetIP(ctx *runner.StepContext) string {
	if ctx == nil || ctx.Executor == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Executor.Host())
}

// reportCleanDetachImpact 声明 detach 将作用于的目标 IP（Dangerous 步结构化提示）。
func reportCleanDetachImpact(ctx *runner.StepContext) {
	if ctx.GetParamBool("skip_cluster_detach", false) {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName: "Detach Standby From Cluster",
			Host:     ctx.Executor.Host(),
			Severity: runner.PrecheckSeverityInfo,
			Code:     "PC.CLEAN.DETACH_SKIPPED",
			Message:  "cluster detach disabled (--skip-cluster-detach); apply will wipe local files without yasboot node remove",
		})
		return
	}
	targetIP := cleanTargetIP(ctx)
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	ctx.ReportPrecheckIssue(runner.PrecheckIssue{
		StepName: "Detach Standby From Cluster",
		Host:     ctx.Executor.Host(),
		Severity: runner.PrecheckSeverityWarn,
		Code:     "PC.CLEAN.DETACH_NODE",
		Message: fmt.Sprintf("apply will run yasboot node remove --purge for target IP %s on cluster %s (local env or OM SSH fallback)",
			targetIP, clusterName),
		Remediation: "use --skip-cluster-detach only if the node is already removed or this host is not a cluster member",
	})
}

// isDetachRetryableLocalFailure 本机 detach 失败且值得改走 OM SSH 的错误(不含业务拒绝类)。
func isDetachRetryableLocalFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "refuse clean") ||
		strings.Contains(msg, "requires --db-admin-password") ||
		strings.Contains(msg, "still appears in cluster") ||
		strings.Contains(msg, "node remove failed") {
		return false
	}
	return strings.Contains(msg, "status failed")
}

// runDetachOnCtx 在给定 ctx(本机或 OM)上执行 status → node remove → 收敛。
func runDetachOnCtx(ctx *runner.StepContext, osUser, envFile, clusterName, sysPassword, targetIP string) error {
	statusCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
	statusRes, statusErr := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, statusCmd, false)
	if statusErr != nil || statusRes == nil || statusRes.GetExitCode() != 0 {
		errMsg := "yasboot cluster status failed"
		if statusRes != nil {
			errMsg = strings.TrimSpace(statusRes.GetStderr() + " " + statusRes.GetStdout())
			if errMsg == "" {
				errMsg = "yasboot cluster status failed"
			}
		}
		return fmt.Errorf("status failed: %s", errMsg)
	}
	statusOut := statusRes.GetStdout()
	ctx.Logger.Info("Cluster status before detach:\n%s", statusOut)

	// 可选: 记录 status 中的 primary, 供日志
	if p := standbysteps.PrimaryIPFromClusterStatus(statusOut); p != "" {
		ctx.Logger.Info("Cluster primary (from status): %s", p)
		if want := strings.TrimSpace(ctx.GetParamString("primary_ip", "")); want != "" && !standbysteps.SameHostIP(want, p) {
			ctx.Logger.Warn("--primary-ip=%s differs from status primary=%s (detach still uses target IP)", want, p)
		}
	}

	rows := dbsteps.ParseClusterStatusTable(statusOut)

	// 先拉 yasagent: listen=- 的 ghost 节点仍可能靠 agent listen IP 定位 hostid
	agentCmd := fmt.Sprintf("yasboot process yasagent status -c %s", clusterName)
	agentRes, _ := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, agentCmd, false)
	agentOut := ""
	if agentRes != nil {
		agentOut = agentRes.GetStdout()
	}

	hostID, nodeID, role, via, found := ResolveClusterIdentity(rows, agentOut, targetIP)
	if !found {
		ctx.Logger.Info("Target %s is not listed in cluster status or yasagent; skip detach", targetIP)
		ctx.Results[resultKeyCleanDetachSkipped] = true
		ctx.Results[resultKeyCleanDetachReason] = "not_in_cluster"
		return nil
	}
	if via == "yasagent" {
		ctx.Logger.Info("Resolved target %s via yasagent listen IP → host=%s (cluster status listen may be empty/-)", targetIP, hostID)
	}

	forcePrimary := ctx.GetParamBool("force_clean_primary", false)
	if IsPrimaryRole(role) && !forcePrimary {
		return fmt.Errorf("target %s is primary (host=%s node=%s); refuse clean without --force-clean-primary (or use --skip-cluster-detach for local-only wipe)",
			targetIP, hostID, nodeID)
	}
	// primary，或 yasdb 已停时 role 为 - / 空：force-clean-primary 表示仅本地擦除，跳过 node remove
	if forcePrimary && (IsPrimaryRole(role) || IsBlankOrUnknownRole(role)) {
		reason := "forced_primary_local_wipe"
		if IsBlankOrUnknownRole(role) {
			reason = "forced_unknown_role_local_wipe"
			ctx.Logger.Warn("Target role is blank/unknown (%q, instance may be down); --force-clean-primary set: skip node remove, continue local wipe", role)
		} else {
			ctx.Logger.Warn("Target is primary; --force-clean-primary set: skip node remove, continue local wipe")
		}
		ctx.Results[resultKeyCleanDetachSkipped] = true
		ctx.Results[resultKeyCleanDetachReason] = reason
		return nil
	}
	if IsBlankOrUnknownRole(role) {
		return fmt.Errorf("target %s has blank/unknown database_role %q (host=%s node=%s; instance may be down); use --force-clean-primary or --skip-cluster-detach for local-only wipe",
			targetIP, role, hostID, nodeID)
	}

	nodeArg := NormalizeNodeIDForRemove(nodeID)
	if nodeArg == "" {
		return fmt.Errorf("empty node id for host %s (%s)", hostID, targetIP)
	}
	if strings.TrimSpace(sysPassword) == "" {
		return fmt.Errorf("standby detach requires --db-admin-password for yasboot node remove (host=%s node=%s)", hostID, nodeArg)
	}

	removeCmd := fmt.Sprintf("yasboot node remove -c %s -n %s -f --purge -p %s",
		clusterName, nodeArg, commonos.ShellSingleQuote(sysPassword))
	ctx.Logger.Info("Detaching standby from cluster: host=%s node=%s ip=%s (via %s)", hostID, nodeArg, targetIP, ctx.Executor.Host())
	ctx.LogPhase("detach-start", fmt.Sprintf("node remove -n %s --purge", nodeArg))

	removeRes, removeErr := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, removeCmd, false)
	out := ""
	exitCode := -1
	if removeRes != nil {
		out = removeRes.GetStdout() + "\n" + removeRes.GetStderr()
		exitCode = removeRes.GetExitCode()
	}
	if removeErr != nil {
		ctx.Logger.Warn("node remove execute error: %v", removeErr)
	}
	if !NodeRemoveLooksSuccessful(out) && exitCode != 0 {
		msg := strings.TrimSpace(out)
		// ghost: 本机已擦除后 OM 侧 node remove 常因 yasagent 不可达失败
		if via == "yasagent" && isYasagentUnreachableRemoveError(msg) {
			return fmt.Errorf("yasboot node remove failed for ghost node %s (yasagent unreachable on %s): %s; cluster metadata may need OM-side repair, or re-run with --skip-cluster-detach only after metadata is clean",
				nodeArg, targetIP, msg)
		}
		return fmt.Errorf("yasboot node remove failed for node %s (exit=%d): %s", nodeArg, exitCode, msg)
	}
	if !NodeRemoveLooksSuccessful(out) && exitCode == 0 {
		ctx.Logger.Warn("node remove exit=0 but SUCCESS marker not found; verifying cluster status")
	}

	postRes, _ := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, statusCmd, false)
	postOut := ""
	if postRes != nil {
		postOut = postRes.GetStdout()
	}
	// 收敛: listen IP 或 hostid 任一仍在则失败(覆盖 ghost listen=-)
	if ClusterStatusContainsIP(postOut, targetIP) || ClusterStatusContainsHostID(postOut, hostID) {
		return fmt.Errorf("after node remove, target %s (host=%s) still appears in cluster status", targetIP, hostID)
	}
	ctx.Logger.Info("Target %s (host=%s) no longer in cluster status after node remove", targetIP, hostID)

	agentRes2, _ := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, agentCmd, false)
	agentOut2 := agentOut
	if agentRes2 != nil {
		agentOut2 = agentRes2.GetStdout()
	}
	if YasagentStatusContainsHostID(agentOut2, hostID) {
		hostRm := fmt.Sprintf("yasboot host remove -c %s --host-ids %s -f", clusterName, hostID)
		ctx.Logger.Info("yasagent still lists %s; attempting host remove", hostID)
		hr, _ := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, hostRm, false)
		if hr != nil && hr.GetExitCode() != 0 {
			ctx.Logger.Warn("host remove exit=%d (often OK if OM already dropped host): %s",
				hr.GetExitCode(), strings.TrimSpace(hr.GetStdout()+" "+hr.GetStderr()))
		}
		agentRes3, _ := commonos.ExecuteAsUserWithEnv(ctx, osUser, envFile, agentCmd, false)
		if agentRes3 != nil && YasagentStatusContainsHostID(agentRes3.GetStdout(), hostID) {
			ctx.Logger.Warn("host %s still listed in yasagent after host remove; local cleanup will continue", hostID)
		}
	}

	ctx.Results[resultKeyCleanDetachedNode] = nodeArg
	ctx.Results[resultKeyCleanDetachedHost] = hostID
	ctx.Results[resultKeyCleanDetachSkipped] = false
	ctx.LogPhase("detach-done", fmt.Sprintf("removed node=%s host=%s", nodeArg, hostID))
	return nil
}

// isDetachUnavailableError 无法定位 OM/env 时的 detach 失败（非 node remove 业务失败）。
func isDetachUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no usable yasboot env") ||
		strings.Contains(msg, "primary environment file not found") ||
		strings.Contains(msg, "cannot detach from cluster") ||
		strings.Contains(msg, "cannot ssh to om")
}

// maybeDegradeDetachForForcePrimary env/OM 不可用且 --force-clean-primary 时降级为仅本地擦除。
func maybeDegradeDetachForForcePrimary(ctx *runner.StepContext, err error) error {
	if err == nil {
		return nil
	}
	if !ctx.GetParamBool("force_clean_primary", false) || !isDetachUnavailableError(err) {
		return err
	}
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}
	ctx.Results[resultKeyCleanDetachSkipped] = true
	ctx.Results[resultKeyCleanDetachReason] = "force_clean_primary_detach_unavailable"
	ctx.Logger.Warn("Cluster detach unavailable (%v); --force-clean-primary set: continue with local wipe only", err)
	return nil
}

// IsDetachUnavailableError 导出供单测。
func IsDetachUnavailableError(err error) bool {
	return isDetachUnavailableError(err)
}

// detachViaOmSSH 本机无法 detach 时, SSH 到 OM 代跑 node remove。
func detachViaOmSSH(ctx *runner.StepContext, osUser, clusterName, sysPassword, targetIP string) error {
	omHost, err := resolveOmHostForClean(ctx, osUser, clusterName)
	if err != nil {
		return fmt.Errorf("cannot detach from cluster (%v); set -M/--om (SSH uses global -u/-p) or use --skip-cluster-detach for local-only wipe", err)
	}
	ctx.Logger.Info("Cluster detach via OM SSH: %s (target=%s)", omHost, targetIP)

	port := ctx.GetParamInt("ssh_port", 22)
	user := ctx.GetParamString("ssh_user", "root")
	pass := ctx.GetParamString("ssh_password", "")
	key := ctx.GetParamString("ssh_key_path", "")
	auth := ctx.GetParamString("ssh_auth", "")

	omExec, err := ssh.NewExecutorWithFallback(ssh.Config{
		Host:       omHost,
		Port:       port,
		User:       user,
		Password:   pass,
		KeyPath:    key,
		AuthMethod: auth,
		Logger:     ctx.Logger,
		StepID:     ctx.CurrentStepID,
	}, pass)
	if err != nil {
		return fmt.Errorf("cannot SSH to OM %s for detach (%v); check global SSH flags or use --skip-cluster-detach for local-only wipe", omHost, err)
	}
	defer omExec.Close()

	omCtx := &runner.StepContext{
		Executor:      runner.SSHExecutorAdapter(omExec),
		Logger:        ctx.Logger,
		Params:        ctx.Params,
		Results:       ctx.Results,
		DryRun:        ctx.DryRun,
		Precheck:      ctx.Precheck,
		CurrentStepID: ctx.CurrentStepID,
		ForceAll:      ctx.ForceAll,
		ForceSteps:    ctx.ForceSteps,
	}

	envFile, err := resolveDetachYasbootEnv(omCtx, osUser, clusterName)
	if err != nil {
		return fmt.Errorf("OM host %s has no usable yasboot env (%v); fix OM env or use --skip-cluster-detach for local-only wipe", omHost, err)
	}
	return runDetachOnCtx(omCtx, osUser, envFile, clusterName, sysPassword, targetIP)
}

// resolveOmHostForClean 解析 OM IP: 全局 -M/--om > 目标机 om_addr > primary_ip 上读 om_addr(可选)。
func resolveOmHostForClean(ctx *runner.StepContext, osUser, clusterName string) (string, error) {
	if h := strings.TrimSpace(ctx.GetParamString("om_ip", "")); h != "" {
		return h, nil
	}
	if h, err := tryOmHostFromTargetEnvFiles(ctx, osUser, clusterName); err == nil && h != "" {
		ctx.Logger.Info("OM IP from target om_addr: %s", h)
		return h, nil
	}
	// 若用户给了 primary_ip 且与当前 target 不同, 可在后续扩展; 此处要求显式 -M/--om 或本机能读到 om_addr
	return "", fmt.Errorf("om_addr not found on target and -M/--om not set")
}

// tryOmHostFromTargetEnvFiles 不依赖完整 bashrc, 直接 cat ~/.yasboot/*.env 解析 om_addr。
func tryOmHostFromTargetEnvFiles(ctx *runner.StepContext, osUser, clusterName string) (string, error) {
	home, err := commonos.GetUserHomeDir(ctx, osUser)
	if err != nil || home == "" {
		return "", fmt.Errorf("home unavailable: %v", err)
	}
	candidates := []string{
		path.Join(home, ".yasboot", clusterName+".env"),
	}
	// 通配若干 .env(限前几个), 避免无 cluster 名时失败
	ls, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/.yasboot/*.env 2>/dev/null | head -5", commonos.ShellSingleQuote(home)), false)
	if ls != nil && ls.GetExitCode() == 0 {
		for _, line := range strings.Split(ls.GetStdout(), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				candidates = append(candidates, line)
			}
		}
	}
	var lastErr error
	for _, p := range candidates {
		res, _ := ctx.Execute(fmt.Sprintf("test -f %s && cat %s", commonos.ShellSingleQuote(p), commonos.ShellSingleQuote(p)), false)
		if res == nil || res.GetExitCode() != 0 {
			lastErr = fmt.Errorf("cannot read %s", p)
			continue
		}
		h, err := standbysteps.OmHostFromEnvFileContent(res.GetStdout())
		if err != nil {
			lastErr = err
			continue
		}
		return h, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no .env with om_addr")
	}
	return "", lastErr
}

// resolveDetachYasbootEnv 优先集群生成的 bashrc，其次 clean 探测的 env 文件。
func resolveDetachYasbootEnv(ctx *runner.StepContext, osUser, clusterName string) (string, error) {
	home, err := commonos.GetUserHomeDir(ctx, osUser)
	if err == nil && home != "" {
		generated := commonos.GetBashrcPath(home, clusterName)
		if remoteFileExists(ctx, generated) {
			return generated, nil
		}
		legacyEnv := path.Join(home, ".yasboot", clusterName+".env")
		if remoteFileExists(ctx, legacyEnv) {
			return legacyEnv, nil
		}
	}
	envFile, err := resolveCleanEnvFile(ctx)
	if err != nil {
		return "", err
	}
	if envFile == "" {
		return "", fmt.Errorf("empty env file")
	}
	return envFile, nil
}

func remoteFileExists(ctx *runner.StepContext, p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	res, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(p)), false)
	return res != nil && res.GetExitCode() == 0
}

// isYasagentUnreachableRemoveError 判断 node remove 是否因目标 yasagent 不可达失败。
func isYasagentUnreachableRemoveError(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "failed to connect host") && strings.Contains(low, "yasagent")
}

// NormalizeNodeIDForRemove 将 status 中的 nodeid（如 1-3:3）规范为 node remove -n 参数（1-3）。
func NormalizeNodeIDForRemove(nodeid string) string {
	nodeid = strings.TrimSpace(nodeid)
	if nodeid == "" {
		return ""
	}
	if i := strings.Index(nodeid, ":"); i > 0 {
		return nodeid[:i]
	}
	return nodeid
}

// IsPrimaryRole 判断 database_role / pdb_role 是否为主库。
func IsPrimaryRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "primary")
}

// IsBlankOrUnknownRole yasdb 停后 cluster status 常给出 "-" / 空 role。
func IsBlankOrUnknownRole(role string) bool {
	r := strings.TrimSpace(strings.ToLower(role))
	return r == "" || r == "-" || r == "unknown"
}

// FindClusterIdentityForIP 按 listen_address 中的 IP 匹配集群行。
func FindClusterIdentityForIP(rows []dbsteps.ClusterStatusRow, ip string) (hostID, nodeID, role string, found bool) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "", "", "", false
	}
	for _, r := range rows {
		listenIP := listenIPFromAddress(r.ListenAddress)
		if listenIP == ip {
			return r.Hostid, r.Nodeid, r.DatabaseRole, true
		}
	}
	return "", "", "", false
}

// FindClusterIdentityForHostID 按 hostid 匹配集群行(用于 listen=- 的 ghost 节点)。
func FindClusterIdentityForHostID(rows []dbsteps.ClusterStatusRow, hostID string) (nodeID, role string, found bool) {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		return "", "", false
	}
	for _, r := range rows {
		if strings.EqualFold(strings.TrimSpace(r.Hostid), hostID) {
			return r.Nodeid, r.DatabaseRole, true
		}
	}
	return "", "", false
}

// HostIDFromYasagentListenIP 从 yasagent status 表 listen_address 反查 hostid。
// ghost 场景: cluster status.listen 已空/-，但 yasagent 仍列 manage IP。
func HostIDFromYasagentListenIP(agentOut, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || strings.TrimSpace(agentOut) == "" {
		return ""
	}
	listenCol := -1
	hostCol := -1
	for _, line := range strings.Split(agentOut, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.Split(line, "|")
		fields := make([]string, 0, len(parts))
		for _, p := range parts {
			fields = append(fields, strings.TrimSpace(p))
		}
		if hostCol < 0 || listenCol < 0 {
			for i, f := range fields {
				switch strings.ToLower(f) {
				case "hostid":
					hostCol = i
				case "listen_address", "listen_addr":
					listenCol = i
				}
			}
			continue
		}
		if len(fields) <= hostCol || len(fields) <= listenCol {
			continue
		}
		hid := fields[hostCol]
		if hid == "" || strings.EqualFold(hid, "hostid") {
			continue
		}
		if listenIPFromAddress(fields[listenCol]) == ip {
			return hid
		}
	}
	// 无表头兜底: 列顺序 hostid|pid|run_user|listen_address|...
	for _, line := range strings.Split(agentOut, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		hid := strings.TrimSpace(parts[1])
		if hid == "" || strings.EqualFold(hid, "hostid") {
			continue
		}
		listen := strings.TrimSpace(parts[4])
		if listenIPFromAddress(listen) == ip {
			return hid
		}
	}
	return ""
}

// ResolveClusterIdentity 定位目标: 优先 cluster status.listen IP, 其次 yasagent.listen IP→hostid。
// via 为 "listen" / "yasagent" / ""。
func ResolveClusterIdentity(rows []dbsteps.ClusterStatusRow, agentOut, ip string) (hostID, nodeID, role, via string, found bool) {
	hostID, nodeID, role, found = FindClusterIdentityForIP(rows, ip)
	if found {
		return hostID, nodeID, role, "listen", true
	}
	hid := HostIDFromYasagentListenIP(agentOut, ip)
	if hid == "" {
		return "", "", "", "", false
	}
	nodeID, role, found = FindClusterIdentityForHostID(rows, hid)
	if !found {
		// yasagent 有 IP 但 status 无该 host: 仍返回 hostid, node 空(调用方无法 node remove)
		return hid, "", "", "yasagent", false
	}
	return hid, nodeID, role, "yasagent", true
}

func listenIPFromAddress(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" || listen == "-" {
		return ""
	}
	if i := strings.Index(listen, ":"); i > 0 {
		return listen[:i]
	}
	return listen
}

// ClusterStatusContainsIP 判断 status 输出是否仍含该 listen IP。
func ClusterStatusContainsIP(statusOut, ip string) bool {
	rows := dbsteps.ParseClusterStatusTable(statusOut)
	_, _, _, found := FindClusterIdentityForIP(rows, ip)
	return found
}

// ClusterStatusContainsHostID 判断 status 是否仍含该 hostid(含 listen=- 的 ghost)。
func ClusterStatusContainsHostID(statusOut, hostID string) bool {
	rows := dbsteps.ParseClusterStatusTable(statusOut)
	_, _, found := FindClusterIdentityForHostID(rows, hostID)
	return found
}

// YasagentStatusContainsHostID 粗粒度判断 yasagent status 表是否仍含 hostid。
func YasagentStatusContainsHostID(agentOut, hostID string) bool {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		return false
	}
	for _, line := range strings.Split(agentOut, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		hid := strings.TrimSpace(parts[1])
		if strings.EqualFold(hid, "hostid") {
			continue
		}
		if hid == hostID {
			return true
		}
	}
	return false
}

// NodeRemoveLooksSuccessful 根据 yasboot node remove 输出判断 NodeRemove 任务是否成功。
func NodeRemoveLooksSuccessful(output string) bool {
	low := strings.ToLower(output)
	if strings.Contains(low, "task completed, status: success") {
		return true
	}
	// 表格行：NodeRemove ... SUCCESS
	for _, line := range strings.Split(output, "\n") {
		lowLine := strings.ToLower(line)
		if strings.Contains(lowLine, "noderemove") && strings.Contains(lowLine, "success") &&
			!strings.Contains(lowLine, "noderemovebefore") && !strings.Contains(lowLine, "noderemoveafter") {
			return true
		}
	}
	return false
}
