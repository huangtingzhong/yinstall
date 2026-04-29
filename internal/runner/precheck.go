package runner

import (
	"time"
)

// PrecheckSeverity 表示一条预检发现的严重程度。
type PrecheckSeverity string

const (
	PrecheckSeverityError PrecheckSeverity = "error"
	PrecheckSeverityWarn  PrecheckSeverity = "warn"
	PrecheckSeverityInfo  PrecheckSeverity = "info"
)

// PrecheckIssue 是结构化、可机器读取的预检发现。
// 设计目标：通过 Logger 写入 JSONL，同时在内存中保留用于汇总展示。
type PrecheckIssue struct {
	Timestamp   string           `json:"timestamp"`
	StepID      string           `json:"step_id"`
	StepName    string           `json:"step_name,omitempty"`
	Host        string           `json:"host,omitempty"`
	Severity    PrecheckSeverity `json:"severity"`
	Code        string           `json:"code,omitempty"`
	Message     string           `json:"message"`
	Evidence    string           `json:"evidence,omitempty"`
	Remediation string           `json:"remediation,omitempty"`
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

// ReportPrecheckIssue 记录一条预检 issue 到 ctx.Results（内存）；仅在 CLI 使用 --precheck 时
// 才将 warn/error 以人类可读行写入终端与 session（见 ConsolePrecheckIssue）。正式安装时也可调用以积累 issue，但不会向终端输出。
func (ctx *StepContext) ReportPrecheckIssue(issue PrecheckIssue) {
	if issue.Timestamp == "" {
		issue.Timestamp = nowRFC3339()
	}
	if issue.StepID == "" {
		issue.StepID = ctx.CurrentStepID
	}
	if issue.Host == "" && ctx.Executor != nil {
		issue.Host = ctx.Executor.Host()
	}

	// 写入内存
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}
	const key = "__precheck_issues"
	if v, ok := ctx.Results[key]; ok {
		if arr, ok := v.([]PrecheckIssue); ok {
			ctx.Results[key] = append(arr, issue)
		} else {
			ctx.Results[key] = []PrecheckIssue{issue}
		}
	} else {
		ctx.Results[key] = []PrecheckIssue{issue}
	}

	// 人类可读行仅用于 CLI --precheck：正式安装（apply）期间绝不写入终端或 session 的 [precheck-*] 行，
	// 避免与正常步骤输出混淆；apply 时 issue 仍保留在内存 ctx.Results 供步骤逻辑使用。
	if ctx.Logger == nil || !ctx.Precheck {
		return
	}
	// --precheck 下 info 过噪，仅输出 warn / error
	if issue.Severity == PrecheckSeverityInfo {
		return
	}
	ctx.Logger.ConsolePrecheckIssue(issue.StepID, issue.StepName, issue.Host, string(issue.Severity), issue.Code, issue.Message)
}

// GetPrecheckIssues 返回记录在 ctx.Results 中的 issues。
func (ctx *StepContext) GetPrecheckIssues() []PrecheckIssue {
	if ctx == nil || ctx.Results == nil {
		return nil
	}
	const key = "__precheck_issues"
	if v, ok := ctx.Results[key]; ok {
		if arr, ok := v.([]PrecheckIssue); ok {
			return arr
		}
	}
	return nil
}
