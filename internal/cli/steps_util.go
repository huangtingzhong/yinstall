package cli

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// filterSteps 根据 include/exclude 的 step 参数过滤要执行的 steps。
//
// 顺序与优先级：
//  1. --include-steps (-s)：将集合收窄到指定 step IDs（支持范围）；未指定表示全部 steps。
//  2. --exclude-steps (-e)：从当前集合移除指定 IDs；若 include 与 exclude 同时包含同一 ID，则以 exclude 为准（不执行）。
//     解析时使用与步骤目录相同的 catalog。
func filterSteps(allSteps []*runner.Step, flags GlobalFlags) []*runner.Step {
	var stepsToRun []*runner.Step

	// 1) 处理 include-steps（支持范围）
	if len(flags.IncludeSteps) > 0 {
		included, err := parseStepRanges(allSteps, flags.IncludeSteps)
		if err != nil {
			// 若解析失败：这里无法返回 error（函数签名历史原因），因此打印错误并返回 nil（停止执行）。
			fmt.Printf("Error parsing include-steps: %v\n", err)
			return nil
		}
		stepsToRun = included
	} else {
		// 默认：全部 steps
		stepsToRun = make([]*runner.Step, len(allSteps))
		copy(stepsToRun, allSteps)
	}

	// 2) 处理 exclude-steps（支持范围）。在 include-steps 之后执行：同一 ID 同时出现 → 以 exclude 为准（不执行）。
	if len(flags.ExcludeSteps) > 0 {
		excluded, err := parseStepRanges(allSteps, flags.ExcludeSteps)
		if err != nil {
			fmt.Printf("Error parsing exclude-steps: %v\n", err)
			return nil
		}
		excludedID := make(map[string]struct{}, len(excluded))
		for _, ex := range excluded {
			if ex != nil {
				excludedID[ex.ID] = struct{}{}
			}
		}
		var retained []*runner.Step
		for _, step := range stepsToRun {
			if step == nil {
				continue
			}
			if _, drop := excludedID[step.ID]; !drop {
				retained = append(retained, step)
			}
		}
		stepsToRun = retained
	}

	return stepsToRun
}

// parseStepRanges 解析一组范围描述（例如 "c001"、"c001-c003"、"c005-"），并从 allSteps 中返回匹配的 steps。
// 假设 allSteps 已按执行顺序排序。
func parseStepRanges(allSteps []*runner.Step, specs []string) ([]*runner.Step, error) {
	// 用 map 去重
	selectedMap := make(map[string]bool)
	var selectedSteps []*runner.Step

	// 构建 ID→index 的映射，用于范围计算
	idToIndex := make(map[string]int)
	// 同时构建大小写不敏感映射
	idMapLower := make(map[string]string)
	for i, step := range allSteps {
		idToIndex[step.ID] = i
		idMapLower[strings.ToLower(step.ID)] = step.ID
	}

	// 辅助函数：解析 ID（大小写不敏感）
	resolveID := func(input string) (string, bool) {
		// 1) 精确匹配
		if _, ok := idToIndex[input]; ok {
			return input, true
		}
		// 2) 大小写不敏感匹配
		if realID, ok := idMapLower[strings.ToLower(input)]; ok {
			return realID, true
		}
		// 3) 规范化分隔符/格式（如 c005 -> C-005）
		// 简单启发：字母后跟数字时，在首字母后插入 '-'
		normalized := normalizeStepID(input)
		if realID, ok := idMapLower[strings.ToLower(normalized)]; ok {
			return realID, true
		}

		return "", false
	}

	for _, spec := range specs {
		// 兼容：用户可能把 "c001,c002" 作为单个字符串传入（某些场景下 cobra 的 StringSlice 会出现）
		parts := strings.Split(spec, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			// 范围语法：
			// 1) "start-end"
			// 2) "start-"
			// 3) "-end"
			// 4) "single"

			if strings.Contains(part, "-") {
				// 注意：step ID 本身可能包含 '-'（例如 C-006），需要区分 ID 内部连字符与范围分隔符。
				// 策略：先判断是否能解析为单个合法 ID；若不是，再按范围解析。

				// 1) 若是单个合法 ID（精确/宽松），直接加入
				if id, ok := resolveID(part); ok {
					// 单个 step
					addStep(allSteps, idToIndex, selectedMap, &selectedSteps, id)
					continue
				}

				// 2) 按范围解析：遍历 '-' 位置尝试切分，若左右两边都能解析为合法 ID（或为空），则认定为范围。

				isRange := false
				for i := 0; i < len(part); i++ {
					if part[i] == '-' {
						prefix := part[:i]
						suffix := part[i+1:]

						// 检查 "start-"
						if suffix == "" {
							if startID, ok := resolveID(prefix); ok {
								addRange(allSteps, idToIndex, selectedMap, &selectedSteps, idToIndex[startID], len(allSteps)-1)
								isRange = true
								break
							}
						}

						// 检查 "-end"
						if prefix == "" {
							if endID, ok := resolveID(suffix); ok {
								addRange(allSteps, idToIndex, selectedMap, &selectedSteps, 0, idToIndex[endID])
								isRange = true
								break
							}
						}

						// 检查 "start-end"
						startID, ok1 := resolveID(prefix)
						endID, ok2 := resolveID(suffix)
						if ok1 && ok2 {
							addRange(allSteps, idToIndex, selectedMap, &selectedSteps, idToIndex[startID], idToIndex[endID])
							isRange = true
							break
						}
					}
				}

				if isRange {
					continue
				}

				return nil, fmt.Errorf("invalid step or range: %s", part)
			} else {
				// 不含连字符：只能是单个 step
				if id, ok := resolveID(part); ok {
					addStep(allSteps, idToIndex, selectedMap, &selectedSteps, id)
				} else {
					return nil, fmt.Errorf("unknown step: %s", part)
				}
			}
		}
	}

	// 重新按 index 排序，保持执行顺序
	//（直接 append 可能会打乱顺序，例如用户传入 "c005,c001"）
	// 安装场景下顺序很关键，应当按 allSteps 中的出现顺序返回。
	// 做法：遍历 allSteps，并根据 selectedMap 过滤。
	var finalSteps []*runner.Step
	for _, step := range allSteps {
		if selectedMap[step.ID] {
			finalSteps = append(finalSteps, step)
		}
	}

	return finalSteps, nil
}

func addStep(allSteps []*runner.Step, idToIndex map[string]int, selectedMap map[string]bool, selectedSteps *[]*runner.Step, id string) {
	if _, exists := selectedMap[id]; !exists {
		selectedMap[id] = true
		*selectedSteps = append(*selectedSteps, allSteps[idToIndex[id]])
	}
}

func addRange(allSteps []*runner.Step, idToIndex map[string]int, selectedMap map[string]bool, selectedSteps *[]*runner.Step, startIdx, endIdx int) {
	if startIdx > endIdx {
		return // Empty range
	}
	for i := startIdx; i <= endIdx; i++ {
		step := allSteps[i]
		if _, exists := selectedMap[step.ID]; !exists {
			selectedMap[step.ID] = true
			*selectedSteps = append(*selectedSteps, step)
		}
	}
}

// normalizeStepID 尝试将 "c005" 规范化为 "C-005"
func normalizeStepID(input string) string {
	// 若形如 [letter][digit][digit][digit]，则添加连字符
	if len(input) == 4 {
		return fmt.Sprintf("%s-%s", strings.ToUpper(input[:1]), input[1:])
	}
	return input
}
