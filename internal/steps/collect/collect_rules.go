// r035_rules.go - 配置驱动的采集扩展步骤
//
// R-035 读取内嵌的 collect_rules.yaml（以及可选的 --rules-file 外部文件），
// 按顺序执行所有 enabled: true 的规则，将结果写入归档目录对应子目录。
//
// 扩展方式（无需改代码）：
//  1. 编辑外部规则文件，添加 SQL / shell 规则条目
//  2. 运行：yinstall collect -t <host> --rules-file /path/to/extra_rules.yaml
//
// 同 ID 规则：外部文件中的条目覆盖内置条目（整条替换）；新 ID 则追加到末尾。
package collect

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// stepRules 返回 R-035 步骤：执行配置驱动的采集规则。
func stepRules() *runner.Step {
	return &runner.Step{
		Name:     "Run collect rules",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			// 预检：尝试加载内嵌规则，确保 embed 正常；若完全无规则则跳过
			cfg, err := LoadEmbeddedRules()
			if err != nil {
				return fmt.Errorf("load embedded rules: %w", err)
			}

			// 与外部文件合并后统计 enabled 规则数
			rules := cfg.Rules
			if extra := ctx.GetParamString("collect_rules_file", ""); extra != "" {
				extraCfg, err := LoadRulesFile(extra)
				if err != nil {
					// --rules-file 加载失败：警告但不阻断（回落到内置规则）
					appendWarning(ctx, fmt.Sprintf("load --rules-file %s: %v (using built-in rules only)", extra, err))
				} else {
					rules = MergeRules(rules, extraCfg.Rules)
				}
			}

			count := 0
			for _, r := range rules {
				if r.Enabled {
					count++
				}
			}
			if count == 0 {
				return fmt.Errorf("no enabled rules found in collect_rules.yaml (step skipped)")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			hostDir := collectHostDir(ctx)

			// 1. 加载内嵌默认规则
			cfg, err := LoadEmbeddedRules()
			if err != nil {
				return fmt.Errorf("load embedded rules: %w", err)
			}
			rules := cfg.Rules

			// 2. 合并外部规则（若指定了 --rules-file）
			if extra := ctx.GetParamString("collect_rules_file", ""); extra != "" {
				extraCfg, err := LoadRulesFile(extra)
				if err != nil {
					appendWarning(ctx, fmt.Sprintf("load --rules-file %s: %v (using built-in rules only)", extra, err))
				} else {
					rules = MergeRules(rules, extraCfg.Rules)
					ctx.Logger.Info("[R-035] merged %d extra rules from %s", len(extraCfg.Rules), extra)
				}
			}

			// 3. 过滤 enabled 规则
			var active []CollectRule
			for _, r := range rules {
				if r.Enabled {
					active = append(active, r)
				}
			}
			ctx.Logger.Info("[R-035] executing %d enabled rules (total %d in config)", len(active), len(rules))
			collectLogPhase(ctx, "plan",
				fmt.Sprintf("enabled=%d total=%d host_dir=%s", len(active), len(rules), hostDir))

			// 4. 顺序执行每条规则
			for i := range active {
				r := &active[i]
				ctx.Logger.Info("[R-035] rule [%s] %s -> %s/%s", r.ID, r.Desc, r.Category, r.Dest)
				ExecuteRule(ctx, r, hostDir)
			}

			ctx.Logger.Info("[R-035] all rules completed, results in %s", hostDir)
			return nil
		},
	}
}
