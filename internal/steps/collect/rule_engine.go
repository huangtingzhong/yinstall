// rule_engine.go - 配置驱动的采集规则引擎
//
// 设计目标：只需编辑 embed/collect_rules.yaml（或通过 --rules-file 传入额外文件），
// 即可扩展采集内容，无需修改 Go 代码或重新编译。
//
// 规则类型：
//   - type: sql   → 以产品用户通过 yasql / as sysdba 执行，结果写入 <host>/db/<dest>
//   - type: shell → 以 SSH 登录用户或产品用户执行，结果写入 <host>/<category>/<dest>
//
// 脚本来源：
//   - source: inline → content 字段直接写 SQL 或 shell 命令
//   - source: file   → path 字段指向 embed/scripts/ 中的内嵌脚本文件
package collect

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yinstall/internal/runner"
)

// embedFS 包含 embed/ 目录下的全部文件（collect_rules.yaml + scripts/）。
//
//go:embed embed
var embedFS embed.FS

// CollectRule 描述一条采集规则（对应 collect_rules.yaml 中的单个条目）。
type CollectRule struct {
	// ID 唯一标识；--rules-file 中同 ID 条目将覆盖内置规则
	ID string `yaml:"id"`
	// Desc 人类可读描述（ASCII，不含中文，写入 debug 日志）
	Desc string `yaml:"desc"`
	// Category 决定输出子目录：db → <host>/db/<dest>；os → <host>/os/<dest>
	Category string `yaml:"category"`
	// Type 执行方式：sql（yasql sysdba）或 shell
	Type string `yaml:"type"`
	// Dest 相对于 category 子目录的输出文件路径，支持斜线分隔的子目录
	Dest string `yaml:"dest"`
	// Timeout 单条规则最大执行秒数；0 = 沿用全局 --collect-sql-timeout / --collect-cmd-timeout
	Timeout int `yaml:"timeout"`
	// Enabled 是否启用；false 条目不执行（用户可在 --rules-file 中改为 true）
	Enabled bool `yaml:"enabled"`
	// Source 脚本来源：inline（直接在 content 字段）或 file（path 指向内嵌脚本）
	Source string `yaml:"source"`
	// Path 内嵌脚本路径，相对于 embed/scripts/（Source 为 file 时有效）
	Path string `yaml:"path"`
	// Content 内联 SQL 或 shell 命令（Source 为 inline 时有效）
	Content string `yaml:"content"`
	// AsDBUser 仅 shell 类型有效：source DB env 后以产品 OS 用户执行
	AsDBUser bool `yaml:"as_db_user"`
	// Sudo 仅 shell 类型有效：以 sudo 执行（需配合全局 --sudo 开关）
	Sudo bool `yaml:"sudo"`
}

// CollectRulesConfig 对应 collect_rules.yaml 顶层结构。
type CollectRulesConfig struct {
	Version string        `yaml:"version"`
	Rules   []CollectRule `yaml:"rules"`
}

// LoadEmbeddedRules 加载二进制内嵌的默认规则配置。
func LoadEmbeddedRules() (*CollectRulesConfig, error) {
	data, err := embedFS.ReadFile("embed/collect_rules.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded collect_rules.yaml: %w", err)
	}
	return parseRulesYAML(data)
}

// LoadRulesFile 从本地文件系统加载外部规则配置（--rules-file）。
func LoadRulesFile(path string) (*CollectRulesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules file %s: %w", path, err)
	}
	return parseRulesYAML(data)
}

// MergeRules 将 extra 中的规则合并到 base：相同 ID 整条覆盖，新 ID 追加到末尾。
func MergeRules(base, extra []CollectRule) []CollectRule {
	index := make(map[string]int, len(base))
	result := make([]CollectRule, len(base))
	copy(result, base)
	for i, r := range result {
		index[r.ID] = i
	}
	for _, r := range extra {
		if i, ok := index[r.ID]; ok {
			result[i] = r
		} else {
			index[r.ID] = len(result)
			result = append(result, r)
		}
	}
	return result
}

// ResolveRuleContent 获取规则实际执行内容：
//   - source: inline → 直接返回 Content 字段
//   - source: file   → 从 embed/scripts/<Path> 读取
func ResolveRuleContent(rule *CollectRule) (string, error) {
	switch rule.Source {
	case "file":
		data, err := embedFS.ReadFile("embed/scripts/" + rule.Path)
		if err != nil {
			return "", fmt.Errorf("read embedded script scripts/%s: %w", rule.Path, err)
		}
		return string(data), nil
	default: // inline 或未指定
		return rule.Content, nil
	}
}

// ExecuteRule 执行单条规则并将输出写入归档目录。
// hostDir 为当前主机的归档根目录（如 ~/.yinstall/collect/<ts>/hosts/10.10.10.130）。
// 执行失败不返回 error，改为 appendWarning 记录（规则失败不中断后续规则）。
func ExecuteRule(ctx *runner.StepContext, rule *CollectRule, hostDir string) {
	content, err := ResolveRuleContent(rule)
	if err != nil {
		appendWarning(ctx, fmt.Sprintf("[%s] resolve content: %v", rule.ID, err))
		return
	}

	// 计算输出路径
	var baseDir string
	switch rule.Category {
	case "db":
		baseDir = filepath.Join(hostDir, "db")
	default: // os
		baseDir = filepath.Join(hostDir, "os")
	}
	destPath := filepath.Join(baseDir, filepath.FromSlash(rule.Dest))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		appendWarning(ctx, fmt.Sprintf("[%s] mkdir %s: %v", rule.ID, filepath.Dir(destPath), err))
		return
	}

	// 计算超时
	timeout := ruleTimeout(ctx, rule)
	dest := collectDestLabel(ctx, destPath)
	timeoutSec := int(timeout.Seconds())
	if rule.Timeout > 0 {
		timeoutSec = rule.Timeout
	}
	collectLogPhase(ctx, "rule-start",
		fmt.Sprintf("id=%s type=%s category=%s dest=%s timeout=%ds desc=%s",
			rule.ID, rule.Type, rule.Category, dest, timeoutSec, rule.Desc))

	// 按类型执行
	var output string
	var execErr error
	switch rule.Type {
	case "sql":
		output, execErr = executeRuleSQL(ctx, rule, content, timeout)
	case "shell":
		output, execErr = executeRuleShell(ctx, rule, content, timeout)
	default:
		appendWarning(ctx, fmt.Sprintf("[%s] unknown rule type: %s", rule.ID, rule.Type))
		collectLogPhase(ctx, "rule-fail", fmt.Sprintf("id=%s err=unknown type %s", rule.ID, rule.Type))
		return
	}

	stats := collectOutputStats(output)
	if execErr != nil {
		collectLogPhase(ctx, "rule-fail", fmt.Sprintf("id=%s %s err=%v", rule.ID, stats, execErr))
	} else {
		collectLogPhase(ctx, "rule-done", fmt.Sprintf("id=%s dest=%s %s", rule.ID, dest, stats))
	}

	if err := writeTextFile(destPath, output); err != nil {
		appendWarning(ctx, fmt.Sprintf("[%s] write %s: %v", rule.ID, destPath, err))
	}
}

// ─── 内部执行函数 ────────────────────────────────────────────────────────────

func executeRuleSQL(ctx *runner.StepContext, rule *CollectRule, sql string, timeout time.Duration) (string, error) {
	envFile := getCollectEnvFile(ctx)
	if envFile == "" {
		ctx.Logger.Info("[R-035] skip SQL rule %s: no DB env file (R-004 may be skipped)", rule.ID)
		return "", nil
	}
	osUser := getCollectOSUser(ctx)
	out, err := collectRunSQL(ctx, osUser, envFile, sql, timeout)
	if err != nil {
		appendWarning(ctx, fmt.Sprintf("[%s] SQL failed: %v", rule.ID, err))
	}
	return out, err
}

// executeRuleShell 以临时文件方式执行 shell 规则（与 collectRunSQL 对称）。
// 脚本内容先写入本地临时文件，上传后用 bash <file> 执行，避免多行脚本内嵌命令行时的引号/换行脆弱性。
//
// YAC 多节点：R-035 是 per-host 步骤（无 Global:true），RunPerHostStepsEx 对每个节点各调用一次，
// 因此 shell 规则自然在所有节点执行，无需额外循环。
func executeRuleShell(ctx *runner.StepContext, rule *CollectRule, script string, timeout time.Duration) (string, error) {
	var out string
	var err error

	if rule.AsDBUser {
		// 以产品 OS 用户身份执行（source DB env file 后 bash <file>）
		envFile := getCollectEnvFile(ctx)
		if envFile == "" {
			ctx.Logger.Info("[R-035] skip shell rule %s (as_db_user): no DB env file", rule.ID)
			return "", nil
		}
		out, err = collectRunShellAsUser(ctx, getCollectOSUser(ctx), envFile, script, timeout)
	} else {
		// 以 SSH 登录用户执行（可选 sudo）
		out, err = collectRunShell(ctx, script, rule.Sudo, timeout)
	}

	if err != nil {
		appendWarning(ctx, fmt.Sprintf("[%s] shell failed: %v", rule.ID, err))
	}
	return out, err
}

// ruleTimeout 计算规则有效超时：优先使用规则自身设置，0 则回落至全局 context 超时。
func ruleTimeout(ctx *runner.StepContext, rule *CollectRule) time.Duration {
	if rule.Timeout > 0 {
		return time.Duration(rule.Timeout) * time.Second
	}
	switch rule.Type {
	case "sql":
		return collectSQLTimeout(ctx)
	default:
		return collectCmdTimeout(ctx)
	}
}

// parseRulesYAML 解析 YAML 字节流为 CollectRulesConfig。
func parseRulesYAML(data []byte) (*CollectRulesConfig, error) {
	var cfg CollectRulesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse collect_rules.yaml: %w", err)
	}
	return &cfg, nil
}

// EmbedFS 暴露内嵌文件系统，供外部工具（如 --list-rules）枚举内嵌脚本。
func EmbedFS() embed.FS {
	return embedFS
}
