package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/ssh"
)

// ExecResult 命令执行结果接口，由 internal/ssh.ExecResult 实现，统一使用 ssh/executor.go 的封装
type ExecResult interface {
	GetStdout() string
	GetStderr() string
	GetExitCode() int
	GetDuration() time.Duration
}

// Executor 命令执行器接口，由 internal/ssh.Executor 实现，统一使用 ssh/executor.go 的封装
type Executor interface {
	Execute(cmd string, sudo bool) (ExecResult, error)
	Host() string
	Close() error
	Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error
}

// Step 步骤定义
type Step struct {
	ID          string   // 步骤 ID，如 B-002
	Name        string   // 步骤名称
	Description string   // 步骤描述
	Tags        []string // 标签，如 os, db, yac
	Dangerous   bool     // 是否危险操作
	Optional    bool     // 是否可选
	Global      bool     // 跨节点全局步骤，需要 TargetHosts 上下文（如自动磁盘发现）

	// 执行函数
	PreCheck  func(ctx *StepContext) error // 前置检查
	Action    func(ctx *StepContext) error // 执行动作
	PostCheck func(ctx *StepContext) error // 结果校验
}

// OSInfo 操作系统信息
type OSInfo struct {
	Name       string // 操作系统名称，如 "Oracle Linux Server", "Red Hat Enterprise Linux", "Kylin"
	Version    string // 版本号，如 "8.8", "7.9", "V10"
	VersionID  string // 版本 ID，如 "8.8", "7.9"
	ID         string // OS ID，如 "ol", "rhel", "kylin"
	Kernel     string // 内核版本
	Arch       string // CPU 架构，如 "x86_64", "aarch64"
	IsRHEL7    bool   // 是否为 RHEL7 系列（包括 CentOS 7, OL 7）
	IsRHEL8    bool   // 是否为 RHEL8 系列（包括 CentOS 8, OL 8, Rocky 8）
	IsKylin    bool   // 是否为麒麟系统
	IsUOS      bool   // 是否为统信 UOS
	PkgManager string // 包管理器: yum, dnf, apt
}

// TargetHost 表示一个目标节点，用于 YAC 等多节点场景下步骤自行决定在哪些节点执行
type TargetHost struct {
	Host     string
	Executor Executor
}

// StepContext 步骤执行上下文
type StepContext struct {
	Executor          Executor
	Logger            *logging.Logger
	Params            map[string]interface{}
	DryRun            bool
	Precheck          bool
	Results           map[string]interface{} // 存储步骤产出
	OSInfo            *OSInfo                // 操作系统信息（由 B-001 填充）
	LocalSoftwareDirs []string               // 本地软件目录
	RemoteSoftwareDir string                 // 远程软件目录
	ForceAll          bool                   // 强制执行所有步骤（-F / --force）
	ForceSteps        []string               // 强制执行的步骤（-f / --force-steps）
	ForceDeleteUser   bool                   // 允许强制模式删除/重建用户和组
	CurrentStepID     string                 // 当前步骤 ID
	StepIndex         int                    // 当前步骤序号（从 0 开始）
	TotalSteps        int                    // 总步骤数
	Progress          *StepProgress          // 非 nil 时由 RunStep 分配序号/总数（排除 Optional 跳过与未执行的连通步）
	// TargetHosts 所有目标节点（YAC 时为多节点）；步骤内部可遍历在需要的节点上执行
	TargetHosts []TargetHost
	// TargetPlatform linux|darwin|windows（M-001 填充）
	TargetPlatform string
}

// ForHost 返回一个仅针对指定节点的子上下文，用于在“所有节点执行”的步骤中逐节点执行
func (ctx *StepContext) ForHost(th TargetHost) *StepContext {
	c := *ctx
	c.Executor = th.Executor
	return &c
}

// HostsToRun 返回本步骤应在哪些节点执行：若 TargetHosts 非空则返回所有节点，否则返回仅当前 Executor（单节点）
func (ctx *StepContext) HostsToRun() []TargetHost {
	if len(ctx.TargetHosts) > 0 {
		return ctx.TargetHosts
	}
	if ctx.Executor != nil {
		return []TargetHost{{Host: ctx.Executor.Host(), Executor: ctx.Executor}}
	}
	return nil
}

// IsForceStep 判断当前步骤是否为强制执行（-F/--force 全局强制 或 -f/--force-steps 指定了当前步骤）
func (ctx *StepContext) IsForceStep() bool {
	if ctx.ForceAll {
		return true
	}
	for _, id := range ctx.ForceSteps {
		if id == ctx.CurrentStepID {
			return true
		}
	}
	return false
}

// IsForceDeleteUser 判断是否允许强制删除/重建用户和组
// 即使 IsForceStep() 为 true，用户/组删除也需要显式 --force-delete-user 才执行
func (ctx *StepContext) IsForceDeleteUser() bool {
	return ctx.IsForceStep() && ctx.ForceDeleteUser
}

// UploadContext 构造文件上传日志上下文（进度写 debug，起止写 Info）。
func (ctx *StepContext) UploadContext() *ssh.UploadContext {
	if ctx == nil || ctx.Logger == nil {
		return nil
	}
	host := ""
	if ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	return &ssh.UploadContext{
		Logger: ctx.Logger,
		StepID: ctx.CurrentStepID,
		Host:   host,
	}
}

// StepResult 步骤执行结果
type StepResult struct {
	StepID    string
	StepName  string
	Host      string
	Success   bool
	Skipped   bool
	Error     error
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
	Artifacts map[string]string
}

// precheckStructuredIssueCoversFailure is true when PreCheck already called ReportPrecheckIssue with the
// same message as err (e.g. PC.DB.DIR.ALREADY_EXISTS). Then we skip emitting duplicate PC.PRECHECK.FAILED lines.
func precheckStructuredIssueCoversFailure(ctx *StepContext, stepID string, err error) bool {
	if ctx == nil || err == nil {
		return false
	}
	msg := strings.TrimSpace(err.Error())
	for _, iss := range ctx.GetPrecheckIssues() {
		if iss.StepID != stepID {
			continue
		}
		if iss.Code == "" || iss.Code == "PC.PRECHECK.FAILED" {
			continue
		}
		if strings.TrimSpace(iss.Message) == msg {
			return true
		}
	}
	return false
}

func (ctx *StepContext) applyProgressIndex(index, total int) {
	ctx.StepIndex = index
	ctx.TotalSteps = total
}

func (ctx *StepContext) claimProgressBeforeRun(step *Step, optionalWillRun bool) {
	if ctx.Progress == nil {
		return
	}
	var idx, total int
	if optionalWillRun {
		idx, total = ctx.Progress.includeOptionalRunning()
	} else {
		idx, total = ctx.Progress.assignProgress()
	}
	ctx.applyProgressIndex(idx, total)
}

func (ctx *StepContext) logProgressStart(step *Step) {
	ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "start", 0)
}

func (ctx *StepContext) logProgressSkip(step *Step, dur time.Duration) {
	ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "skip", dur)
}

func (ctx *StepContext) logProgressSkippedNotCounted(step *Step) {
	ctx.Logger.ConsoleStepSkipped(step.ID, step.Name)
}

// RunStep 执行单个步骤
func RunStep(step *Step, ctx *StepContext) *StepResult {
	host := ctx.Executor.Host()
	ctx.CurrentStepID = step.ID // 设置当前步骤 ID

	result := &StepResult{
		StepID:    step.ID,
		StepName:  step.Name,
		Host:      host,
		StartTime: time.Now(),
		Artifacts: make(map[string]string),
	}

	ctx.Logger.LogStepStart(host, step.ID, step.Name)

	// Optional：先 PreCheck，跳过则不占进度序号/总数
	if step.Optional && step.PreCheck != nil {
		ctx.Logger.Debug(logging.LogEntry{
			Host: host, StepID: step.ID, Level: "debug", Message: "Running pre-check",
		})
		if err := step.PreCheck(ctx); err != nil {
			result.Success = true
			result.Skipped = true
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			if !ctx.Precheck {
				if ctx.Progress != nil {
					ctx.logProgressSkippedNotCounted(step)
				} else {
					ctx.logProgressSkip(step, result.Duration)
				}
			}
			ctx.Logger.LogStepEnd(host, step.ID, step.Name, true, result.Duration, "skipped: "+err.Error())
			return result
		}
	}
	if step.Optional {
		ctx.claimProgressBeforeRun(step, true)
		ctx.logProgressStart(step)
	} else {
		ctx.claimProgressBeforeRun(step, false)
		ctx.logProgressStart(step)
	}

	// 1. Pre-check（非 Optional 或 Optional 无 PreCheck 已在上面处理）
	if step.PreCheck != nil && !step.Optional {
		ctx.Logger.Debug(logging.LogEntry{
			Host:    host,
			StepID:  step.ID,
			Level:   "debug",
			Message: "Running pre-check",
		})
		if err := step.PreCheck(ctx); err != nil {
			// 显式跳过（如无 root/sudo）：终端显示 skipped，不算失败
			if IsStepSkipped(err) {
				result.Success = true
				result.Skipped = true
				result.EndTime = time.Now()
				result.Duration = result.EndTime.Sub(result.StartTime)
				if !ctx.Precheck {
					ctx.logProgressSkip(step, result.Duration)
				}
				ctx.Logger.LogStepEnd(host, step.ID, step.Name, true, result.Duration, "skipped: "+err.Error())
				return result
			}
			// In precheck mode we want to continue execution in the outer loop.
			// Here we mark the step as failed, and emit a structured issue (unless PreCheck already reported the same message).
			if ctx.Precheck && !precheckStructuredIssueCoversFailure(ctx, step.ID, err) {
				ctx.ReportPrecheckIssue(PrecheckIssue{
					StepID:      step.ID,
					StepName:    step.Name,
					Host:        host,
					Severity:    PrecheckSeverityError,
					Code:        "PC.PRECHECK.FAILED",
					Message:     err.Error(),
					Evidence:    "",
					Remediation: "",
				})
			}

			result.Error = fmt.Errorf("pre-check failed: %w", err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			ctx.Logger.LogErrorExit(host, step.ID, step.Name, "", "", "", -1, result.Error.Error())
			ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "fail", result.Duration)
			ctx.Logger.LogStepEnd(host, step.ID, step.Name, false, result.Duration, result.Error.Error())
			return result
		}
	}

	// Precheck mode only
	if ctx.Precheck {
		result.Success = true
		result.Skipped = true // keep legacy console semantics (skip action/postcheck)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		// For --precheck: do not print "passed" noise.
		ctx.Logger.LogStepEnd(host, step.ID, step.Name, true, result.Duration, "precheck passed")
		return result
	}

	// Dry-run mode
	if ctx.DryRun {
		ctx.Logger.Debug(logging.LogEntry{
			Host:    host,
			StepID:  step.ID,
			Level:   "info",
			Message: "Dry-run mode, skipping action",
		})
		result.Success = true
		result.Skipped = true
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "skip", result.Duration)
		ctx.Logger.LogStepEnd(host, step.ID, step.Name, true, result.Duration, "dry-run")
		return result
	}

	// 2. Execute action
	if step.Action != nil {
		ctx.Logger.Debug(logging.LogEntry{
			Host:    host,
			StepID:  step.ID,
			Level:   "debug",
			Message: "Running action",
		})
		if err := step.Action(ctx); err != nil {
			result.Error = fmt.Errorf("action failed: %w", err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			// 若错误来自 ExecuteWithCheck / ExecuteAsUser*WithCheck，已在该处输出 LogErrorExit，此处不再重复。
			if !CommandExitLogged(err) {
				ctx.Logger.LogErrorExit(host, step.ID, step.Name, "", "", "", -1, result.Error.Error())
			}
			ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "fail", result.Duration)
			ctx.Logger.LogStepEnd(host, step.ID, step.Name, false, result.Duration, result.Error.Error())
			return result
		}
	}

	// 3. Post-check
	if step.PostCheck != nil {
		ctx.Logger.Debug(logging.LogEntry{
			Host:    host,
			StepID:  step.ID,
			Level:   "debug",
			Message: "Running post-check",
		})
		if err := step.PostCheck(ctx); err != nil {
			result.Error = fmt.Errorf("post-check failed: %w", err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			ctx.Logger.LogErrorExit(host, step.ID, step.Name, "", "", "", -1, result.Error.Error())
			ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "fail", result.Duration)
			ctx.Logger.LogStepEnd(host, step.ID, step.Name, false, result.Duration, result.Error.Error())
			return result
		}
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	ctx.Logger.ConsoleStep(step.ID, step.Name, ctx.StepIndex, ctx.TotalSteps, "success", result.Duration)
	ctx.Logger.LogStepEnd(host, step.ID, step.Name, true, result.Duration, "")
	return result
}

// TruncateForLog 截断过长字符串用于 phase 摘要（默认 120 字符）。
func TruncateForLog(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 120
	}
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// LogPhase 写入结构化 debug 里程碑（仅 debug 文件，不进终端）。
func (ctx *StepContext) LogPhase(phase, msg string) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	host := ""
	if ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	ctx.Logger.Debug(logging.LogEntry{
		Host:    host,
		StepID:  ctx.CurrentStepID,
		Level:   "debug",
		Phase:   phase,
		Message: msg,
	})
}

// LogScriptPreview 在执行 shell/SQL 脚本正文前写入 debug 日志（见 logging.LogScriptPreview）。
func (ctx *StepContext) LogScriptPreview(scriptKind, label, body string) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	host := ""
	if ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	ctx.Logger.LogScriptPreview(host, ctx.CurrentStepID, scriptKind, label, body)
}

// Execute 在上下文中执行命令并记录日志
func (ctx *StepContext) Execute(cmd string, sudo bool) (ExecResult, error) {
	host := ctx.Executor.Host()
	stepID := ctx.CurrentStepID

	ctx.Logger.LogCommandStart(host, stepID, cmd)

	result, err := ctx.Executor.Execute(cmd, sudo)
	if result != nil {
		ctx.Logger.LogCommandResult(host, stepID,
			result.GetStdout(), result.GetStderr(),
			result.GetExitCode(), result.GetDuration())
	} else if err != nil {
		ctx.Logger.LogCommandResult(host, stepID, "", err.Error(), -1, 0)
	}
	return result, err
}

// ExecuteWithCheck 执行命令并检查返回码；失败时通过 Logger.LogErrorExit 将命令与完整输出输出到终端
func (ctx *StepContext) ExecuteWithCheck(cmd string, sudo bool) (ExecResult, error) {
	result, err := ctx.Execute(cmd, sudo)
	if err != nil {
		return result, err
	}
	if result != nil && result.GetExitCode() != 0 {
		errMsg := result.GetStderr()
		if errMsg == "" {
			errMsg = result.GetStdout()
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("exit code %d", result.GetExitCode())
		}
		ctx.Logger.LogErrorExit(
			ctx.Executor.Host(),
			ctx.CurrentStepID,
			"",
			cmd,
			result.GetStdout(),
			result.GetStderr(),
			result.GetExitCode(),
			errMsg,
		)
		return result, NewCommandExitError(result.GetExitCode(), errMsg, true)
	}
	return result, nil
}

// GetTargetPlatform returns linux|darwin|windows for the current host context.
func (ctx *StepContext) GetTargetPlatform() string {
	if ctx.TargetPlatform != "" {
		return ctx.TargetPlatform
	}
	host := ""
	if ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	if host != "" {
		if v, ok := ctx.Results[host+"_target_platform"].(string); ok && v != "" {
			return v
		}
	}
	if v, ok := ctx.Results["target_platform"].(string); ok && v != "" {
		return v
	}
	return PlatformLinuxDefault
}

// PlatformLinuxDefault is the default target platform when unset.
const PlatformLinuxDefault = "linux"

// GetParam 获取参数
func (ctx *StepContext) GetParam(key string) interface{} {
	if ctx.Params == nil {
		return nil
	}
	return ctx.Params[key]
}

// GetParamString 获取字符串参数
func (ctx *StepContext) GetParamString(key string, defaultVal string) string {
	v := ctx.GetParam(key)
	if v == nil {
		return defaultVal
	}
	if s, ok := v.(string); ok {
		return s
	}
	return defaultVal
}

// GetParamInt 获取整数参数
func (ctx *StepContext) GetParamInt(key string, defaultVal int) int {
	v := ctx.GetParam(key)
	if v == nil {
		return defaultVal
	}
	if i, ok := v.(int); ok {
		return i
	}
	return defaultVal
}

// YasbootRemoteSSHPort 返回 yasboot 访问远端节点时使用的 SSH 端口（对应 yasboot 的 --port）。
// 优先使用 params["yasboot_ssh_port"]；未设置或为 0 时回退到 params["ssh_port"]，再回退到 defaultVal。
func (ctx *StepContext) YasbootRemoteSSHPort(defaultVal int) int {
	if ctx == nil {
		return defaultVal
	}
	p := ctx.GetParamInt("yasboot_ssh_port", 0)
	if p > 0 {
		return p
	}
	return ctx.GetParamInt("ssh_port", defaultVal)
}

// GetParamBool 获取布尔参数
func (ctx *StepContext) GetParamBool(key string, defaultVal bool) bool {
	v := ctx.GetParam(key)
	if v == nil {
		return defaultVal
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return defaultVal
}

// GetParamStringSlice 获取字符串切片参数
func (ctx *StepContext) GetParamStringSlice(key string) []string {
	v := ctx.GetParam(key)
	if v == nil {
		return nil
	}
	if s, ok := v.([]string); ok {
		return s
	}
	return nil
}

// SetResult 设置步骤结果
func (ctx *StepContext) SetResult(key string, value interface{}) {
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}
	ctx.Results[key] = value
}
