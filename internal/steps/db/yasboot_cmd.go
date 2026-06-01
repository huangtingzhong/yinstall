package db

import (
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// ParamYasbootGenExtraArgs / ParamYasbootDeployExtraArgs 与 CLI --yasboot-gen-extra-args、--yasboot-deploy-extra-args 对应。
const (
	ParamYasbootGenExtraArgs    = "yasboot_gen_extra_args"
	ParamYasbootDeployExtraArgs = "yasboot_deploy_extra_args"
)

// BuildClusterDeployInner 组装 yasboot cluster deploy 子命令（不含 cd stageDir）。
func BuildClusterDeployInner(yasbootPath, configPath, password string, isYAC bool, deployExtra string) string {
	inner := yasbootPath + " cluster deploy -t " + configPath + " -p " + commonos.ShellSingleQuote(password)
	if isYAC {
		inner += " --yfs-force-create"
	}
	return commonos.YasbootAppendExtraArgs(inner, deployExtra, false)
}

// AppendYasbootGenExtraArgs 将 gen 阶段附加参数拼到 package se/ce gen 命令末尾。
func AppendYasbootGenExtraArgs(genCmd, genExtra string) string {
	return commonos.YasbootAppendExtraArgs(genCmd, genExtra, false)
}

// YasbootCommandHasEnableBranch 在整条 yasboot 命令或附加参数字符串中检测 enable-branch 关键字。
// 不限于 --yasboot-gen-extra-args；未来其它 Param 拼入的命令片段同样适用。
func YasbootCommandHasEnableBranch(cmdOrArgs string) bool {
	cmdOrArgs = strings.TrimSpace(cmdOrArgs)
	if cmdOrArgs == "" {
		return false
	}
	return strings.Contains(strings.ToLower(cmdOrArgs), "enable-branch")
}

// StepContextHasEnableBranch 从步骤 Params 中聚合所有可能携带 yasboot 标志的字符串并检测 enable-branch。
func StepContextHasEnableBranch(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	seen := make(map[string]struct{})
	for _, blob := range yasbootEnableBranchParamBlobs(ctx) {
		if blob == "" {
			continue
		}
		if _, ok := seen[blob]; ok {
			continue
		}
		seen[blob] = struct{}{}
		if YasbootCommandHasEnableBranch(blob) {
			return true
		}
	}
	return false
}

// yasbootEnableBranchParamBlobs 收集可能与 yasboot gen/deploy 相关的参数字符串。
func yasbootEnableBranchParamBlobs(ctx *runner.StepContext) []string {
	var out []string
	appendIf := func(s string) {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	appendIf(ctx.GetParamString(ParamYasbootGenExtraArgs, ""))
	appendIf(ctx.GetParamString(ParamYasbootDeployExtraArgs, ""))
	for k, v := range ctx.Params {
		if !strings.Contains(strings.ToLower(k), "yasboot") {
			continue
		}
		if s, ok := v.(string); ok {
			appendIf(s)
		}
	}
	return out
}

// YasbootGenExtraHasEnableBranch 保留旧名，委托 YasbootCommandHasEnableBranch。
//
// Deprecated: 对单段参数字符串请用 YasbootCommandHasEnableBranch；对步骤上下文请用 StepContextHasEnableBranch。
func YasbootGenExtraHasEnableBranch(genExtra string) bool {
	return YasbootCommandHasEnableBranch(genExtra)
}
