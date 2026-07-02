package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
)

// ExecResultForC001 执行结果接口，供 C-001 预检查调用；internal/ssh.ExecResult 通过 GetStdout/GetExitCode 实现
type ExecResultForC001 interface {
	GetStdout() string
	GetExitCode() int
}

// ExecutorForC001 执行器接口，供 C-001 预检查调用；由 cli 层用 ssh.Executor 适配实现
type ExecutorForC001 interface {
	Execute(cmd string, sudo bool) (ExecResultForC001, error)
	Host() string
}

// HostExec 供 C-001 预检查使用的单节点信息（避免 db 包依赖 cli 包）
type HostExec struct {
	Host     string
	Executor ExecutorForC001
}

// StepC001Check DB 安装第一步：检查网络可用性；单机时检查产品用户存在；YAC 下检查所有节点 UID/GID/用户一致及共享盘可用
// 实际检查逻辑由 RunConnectivityAndYACPrecheck 执行（需在 db 命令中传入所有节点），本步骤 Action 仅在单机时打日志
func StepC001Check() *runner.Step {
	return &runner.Step{
		ID:          "C-001",
		Name:        "Check Connectivity and YAC Prerequisites",
		Description: "Verify connectivity; YAC: network CIDR, product user password, optional udev disk discovery, UID/GID and shared disks",
		Tags:        []string{"db", "connectivity", "yac", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-001: Check Connectivity and YAC Prerequisites")
			ctx.Logger.Info("Connectivity and prerequisites check completed (standalone mode)")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// HostExecsFromStepContext 从多节点 StepContext 构建 HostExec 列表（供 C-009/C-010/C-013 等步骤调用）。
func HostExecsFromStepContext(ctx *runner.StepContext) []HostExec {
	if ctx == nil {
		return nil
	}
	hosts := ctx.HostsToRun()
	out := make([]HostExec, 0, len(hosts))
	for _, th := range hosts {
		hctx := ctx.ForHost(th)
		out = append(out, HostExec{
			Host:     th.Host,
			Executor: &stepCtxC001Executor{hctx: hctx},
		})
	}
	return out
}

type stepCtxC001Executor struct {
	hctx *runner.StepContext
}

func (a *stepCtxC001Executor) Execute(cmd string, sudo bool) (ExecResultForC001, error) {
	if a == nil || a.hctx == nil {
		return nil, fmt.Errorf("nil step context")
	}
	r, err := a.hctx.Execute(cmd, sudo)
	if err != nil {
		return nil, err
	}
	return &stepCtxC001Result{r: r}, nil
}

func (a *stepCtxC001Executor) Host() string {
	if a == nil || a.hctx == nil || a.hctx.Executor == nil {
		return ""
	}
	return a.hctx.Executor.Host()
}

type stepCtxC001Result struct {
	r runner.ExecResult
}

func (a *stepCtxC001Result) GetStdout() string {
	if a == nil || a.r == nil {
		return ""
	}
	return a.r.GetStdout()
}

func (a *stepCtxC001Result) GetExitCode() int {
	if a == nil || a.r == nil {
		return -1
	}
	return a.r.GetExitCode()
}

// RunC001FullPrecheck 执行 C-001 全部逻辑：连通性/YAC 前置、网段校验、产品用户密码、skip-os 磁盘发现。
func RunC001FullPrecheck(hosts []HostExec, params map[string]interface{}, logger *logging.Logger, isYACMode, skipOS, precheckMode, dryRun bool) error {
	if err := RunConnectivityAndYACPrecheck(hosts, params, logger, isYACMode); err != nil {
		return err
	}
	if !isYACMode {
		return nil
	}
	if err := RunNetworkValidation(hosts, params, logger); err != nil {
		return err
	}
	if !dryRun {
		if err := RunYACProductUserPasswordEnsure(hosts, params, logger, precheckMode); err != nil {
			return err
		}
	}
	if skipOS {
		if err := RunYACUdevDiskDiscovery(hosts, params, logger); err != nil {
			return err
		}
	}
	return nil
}

// RunConnectivityAndYACPrecheck 执行 C-001 连通性与 YAC 身份/共享盘检查。
func RunConnectivityAndYACPrecheck(hosts []HostExec, params map[string]interface{}, logger *logging.Logger, isYACMode bool) error {
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts to check")
	}

	firstHost := hosts[0].Host
	logger.ConsoleWithType("C-001", "Check Connectivity and YAC Prerequisites", firstHost, "start", "", "", 0)
	logger.Info("Running connectivity and YAC prerequisites check...")

	// 1. 网络：在各主机上做快速连通性检查
	for _, h := range hosts {
		result, err := c001Exec(h, logger, "echo 'connection_ok'", false)
		if err != nil {
			return fmt.Errorf("network check failed for %s: %w", h.Host, err)
		}
		if result == nil || result.GetExitCode() != 0 || !strings.Contains(result.GetStdout(), "connection_ok") {
			return fmt.Errorf("network check failed for %s: unexpected response", h.Host)
		}
	}
	logger.Info("Network connectivity: OK on all %d node(s)", len(hosts))

	user := getParamString(params, "os_user", "yashan")

	if !isYACMode {
		// 单机：若 --skip-os，产品用户须已存在；若包含 OS 基线（未 skip），用户由 B-003 创建，
		// 全局预检在 Phase 2 开头执行时早于 B-003 的 apply，故不在此强制要求用户已存在（与 B-010/C-006 预检策略一致）。
		skipOS := getParamBool(params, "db_skip_os", false)
		h := hosts[0]
		if skipOS {
			ru, _ := c001Exec(h, logger, fmt.Sprintf("id -u %s 2>/dev/null", user), false)
			uid := strings.TrimSpace(execStdout(ru))
			if uid == "" {
				return fmt.Errorf("user %s does not exist on node %s; with --skip-os create the user first or run OS preparation", user, h.Host)
			}
			logger.Info("Standalone (--skip-os): product user %s exists on %s (UID=%s)", user, h.Host, uid)
		} else {
			logger.Info("Standalone with OS baseline: deferring product user existence to step order (B-003 creates user before DB steps that need it)")
		}
		logger.ConsoleWithType("C-001", "Check Connectivity and YAC Prerequisites", firstHost, "success", "", "", time.Duration(0))
		return nil
	}

	// 2. YAC: collect UID, GID, group name from each node and verify consistency
	group := getParamString(params, "os_group", "yashan")

	type nodeIdentity struct {
		host      string
		uid       string
		gid       string
		groupName string
		groupGID  string
	}

	var identities []nodeIdentity
	for _, h := range hosts {
		ru, _ := c001Exec(h, logger, fmt.Sprintf("id -u %s 2>/dev/null", user), false)
		rg, _ := c001Exec(h, logger, fmt.Sprintf("id -g %s 2>/dev/null", user), false)
		rgn, _ := c001Exec(h, logger, fmt.Sprintf("id -gn %s 2>/dev/null", user), false)
		rgg, _ := c001Exec(h, logger, fmt.Sprintf("getent group %s 2>/dev/null | cut -d: -f3", group), false)

		uid := strings.TrimSpace(execStdout(ru))
		gid := strings.TrimSpace(execStdout(rg))
		groupName := strings.TrimSpace(execStdout(rgn))
		groupGID := strings.TrimSpace(execStdout(rgg))

		if uid == "" || gid == "" {
			return fmt.Errorf("user %s not found on node %s", user, h.Host)
		}
		if groupName == "" {
			groupName = group
		}
		identities = append(identities, nodeIdentity{h.Host, uid, gid, groupName, groupGID})
	}

	ref := identities[0]
	for _, id := range identities {
		if id.uid != ref.uid {
			return fmt.Errorf("UID mismatch: node %s has UID %s, node %s has UID %s (user %s); all nodes must have the same UID",
				ref.host, ref.uid, id.host, id.uid, user)
		}
		if id.gid != ref.gid {
			return fmt.Errorf("GID mismatch: node %s has GID %s, node %s has GID %s (user %s); all nodes must have the same GID",
				ref.host, ref.gid, id.host, id.gid, user)
		}
		if id.groupName != ref.groupName {
			return fmt.Errorf("group name mismatch: node %s has group %s, node %s has group %s; all nodes must have the same group name",
				ref.host, ref.groupName, id.host, id.groupName)
		}
	}
	logger.Info("YAC node identity: UID=%s, GID=%s, group=%s (consistent on all %d nodes)", ref.uid, ref.gid, ref.groupName, len(hosts))

	// 3. YAC: collect shared disk list and verify each disk is available on every node
	systemdgStr := getParamString(params, "yac_systemdg", "")
	datadgStr := getParamString(params, "yac_datadg", "")
	archdgStr := getParamString(params, "yac_archdg", "")

	var allDisks []string
	for _, dgStr := range []string{systemdgStr, datadgStr, archdgStr} {
		if dgStr == "" {
			continue
		}
		dg, err := ossteps.ParseDiskGroupConfig(dgStr)
		if err != nil || dg == nil {
			continue
		}
		for _, d := range dg.Disks {
			d = strings.TrimSpace(d)
			if d != "" {
				allDisks = append(allDisks, d)
			}
		}
	}

	if len(allDisks) > 0 {
		for _, h := range hosts {
			for _, disk := range allDisks {
				result, _ := c001Exec(h, logger, fmt.Sprintf("test -b %s && echo ok", disk), false)
				if result == nil || result.GetExitCode() != 0 || !strings.Contains(result.GetStdout(), "ok") {
					return fmt.Errorf("shared disk %s is not available on node %s", disk, h.Host)
				}
			}
		}
		logger.Info("Shared disks: all %d disk(s) available on all %d node(s)", len(allDisks), len(hosts))
	}

	logger.ConsoleWithType("C-001", "Check Connectivity and YAC Prerequisites", firstHost, "success", "", "", time.Duration(0))
	return nil
}

func getParamString(params map[string]interface{}, key, def string) string {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	s, _ := v.(string)
	if s == "" {
		return def
	}
	return s
}

func getParamBool(params map[string]interface{}, key string, def bool) bool {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func execStdout(result ExecResultForC001) string {
	if result == nil {
		return ""
	}
	return result.GetStdout()
}

// c001Exec 在 C-001 独立函数中执行命令并记录 debug 日志
func c001Exec(h HostExec, logger *logging.Logger, cmd string, sudo bool) (ExecResultForC001, error) {
	logger.LogCommandStart(h.Host, "C-001", cmd)
	start := time.Now()
	result, err := h.Executor.Execute(cmd, sudo)
	dur := time.Since(start)
	stdout := ""
	exitCode := -1
	if result != nil {
		stdout = result.GetStdout()
		exitCode = result.GetExitCode()
	}
	if err != nil {
		logger.LogCommandResult(h.Host, "C-001", stdout, err.Error(), exitCode, dur)
	} else {
		logger.LogCommandResult(h.Host, "C-001", stdout, "", exitCode, dur)
	}
	return result, err
}
