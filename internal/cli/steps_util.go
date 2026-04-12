package cli

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// filterSteps filters steps based on include/exclude flags and tags.
//
// Order and precedence:
//  1. --include-steps (-s): narrows to the listed step IDs (ranges supported). Omitted = all steps.
//  2. --exclude-steps: removes listed IDs from the current set. If both include and exclude list the same ID,
//     exclude wins (step is not run). Parsed against the same catalog as step 1.
//  3. --include-tags: narrows to steps that have at least one listed tag.
//  4. --exclude-tags: removes steps that have any listed tag.
func filterSteps(allSteps []*runner.Step, flags GlobalFlags) []*runner.Step {
	var stepsToRun []*runner.Step

	// 1. Handle include-steps (ranges supported)
	if len(flags.IncludeSteps) > 0 {
		included, err := parseStepRanges(allSteps, flags.IncludeSteps)
		if err != nil {
			// If parsing fails, fall back to exact match (backward compatibility or error)
			// For now, let's log error and return nil or panic?
			// Since we can't easily return error here without changing signature,
			// and existing code expects []*runner.Step.
			// Let's print error to stderr and return empty list to stop execution if possible,
			// or just log and ignore invalid ranges.
			// Better: parseStepRanges should return error, and we should handle it.
			// But the original filterSteps didn't return error.
			// We can panic or print error.
			fmt.Printf("Error parsing include-steps: %v\n", err)
			return nil
		}
		stepsToRun = included
	} else {
		// Default: all steps
		stepsToRun = make([]*runner.Step, len(allSteps))
		copy(stepsToRun, allSteps)
	}

	// 2. Handle exclude-steps (ranges supported). Runs after include-steps: same ID in both → excluded (not run).
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

	// 3. Handle include-tags
	if len(flags.IncludeTags) > 0 {
		var tagged []*runner.Step
		for _, step := range stepsToRun {
			matched := false
			for _, targetTag := range flags.IncludeTags {
				for _, stepTag := range step.Tags {
					if stepTag == targetTag {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if matched {
				tagged = append(tagged, step)
			}
		}
		stepsToRun = tagged
	}

	// 4. Handle exclude-tags
	if len(flags.ExcludeTags) > 0 {
		var retained []*runner.Step
		for _, step := range stepsToRun {
			excluded := false
			for _, targetTag := range flags.ExcludeTags {
				for _, stepTag := range step.Tags {
					if stepTag == targetTag {
						excluded = true
						break
					}
				}
				if excluded {
					break
				}
			}
			if !excluded {
				retained = append(retained, step)
			}
		}
		stepsToRun = retained
	}

	return stepsToRun
}

// parseStepRanges parses a list of range specs (e.g. "c001", "c001-c003", "c005-")
// and returns the matching steps from allSteps.
// It assumes allSteps is sorted in execution order.
func parseStepRanges(allSteps []*runner.Step, specs []string) ([]*runner.Step, error) {
	// Use a map to deduplicate steps
	selectedMap := make(map[string]bool)
	var selectedSteps []*runner.Step

	// Create a map of ID to index for range calculation
	idToIndex := make(map[string]int)
	// Also case-insensitive map
	idMapLower := make(map[string]string)
	for i, step := range allSteps {
		idToIndex[step.ID] = i
		idMapLower[strings.ToLower(step.ID)] = step.ID
	}

	// Helper to resolve ID (case-insensitive)
	resolveID := func(input string) (string, bool) {
		// 1. Try exact match
		if _, ok := idToIndex[input]; ok {
			return input, true
		}
		// 2. Try case-insensitive match
		if realID, ok := idMapLower[strings.ToLower(input)]; ok {
			return realID, true
		}
		// 3. Try normalizing separators/format if needed（如 c005 -> C-005）
		// Simple heuristic: insert hyphen after first letter if it's a letter followed by digits
		normalized := normalizeStepID(input)
		if realID, ok := idMapLower[strings.ToLower(normalized)]; ok {
			return realID, true
		}

		return "", false
	}

	for _, spec := range specs {
		// Split by comma in case user passed "c001,c002" as single string (cobra does this for StringSlice)
		// But specs is already []string from StringSliceVar.
		// Handling split just in case.
		parts := strings.Split(spec, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			// Check for range syntax matches
			// 1. "start-end"
			// 2. "start-"
			// 3. "-end"
			// 4. "single"

			if strings.Contains(part, "-") {
				// Potential range. But wait, step IDs themselves might contain hyphens (e.g. C-006).
				// We need to distinguish between ID hyphen and range separator.
				// However, if we follow the rule:
				// c005-c006 -> range
				// c005 -> single
				// c005- -> range
				// -c006 -> range
				//
				// Ambiguity: C-006 is a valid ID. "C-006" contains "-".
				// "C-006-C-007" -> is it "C-006" to "C-007"? Yes.
				// "C-006-" -> "C-006" to end? Yes.
				//
				// Logic: Split by "-" is risky if IDs have hyphens.
				// But we know standard IDs are like X-NNN.
				// Let's allow users to likely use concise IDs like c005.
				// If they use full IDs C-006, we have C-006-C-007. Two hyphens?
				//
				// Let's try to parse intelligently.
				// If the string starts with "-", it's "-end"
				// If ends with "-", it's "start-"
				// unique separator?
				//
				// Best approach: check if the string *is* a valid ID first.
				// "C-006" -> yes. Treat as single.
				// "C-006-C-007" -> not a valid ID. Treat as range check.
				// "c005" -> maybe valid (case insensitive). check first.

				// 1. Check if it's a single valid ID (exact or loose)
				if id, ok := resolveID(part); ok {
					// It's a single step
					addStep(allSteps, idToIndex, selectedMap, &selectedSteps, id)
					continue
				}

				// 2. Parse as range
				// Need to find the split point.
				// If we have "start-end", and start/end might contain hyphens.
				// Try to match longest possible valid IDs?
				// Or assume the range separator is the *last* hyphen? or *first*?
				// "C-006-C-007". Middle one.
				// "c005-c006". Middle one.
				//
				// Let's try checking prefix and suffix.
				// Iterate through all possible split points?
				// Or use a more specific separator if possible? No, user input.
				//
				// Heuristic for "start-end":
				// Attempt to split at every '-' index. If left and right are valid IDs (or empty), then it's a range.

				isRange := false
				for i := 0; i < len(part); i++ {
					if part[i] == '-' {
						prefix := part[:i]
						suffix := part[i+1:]

						// Check "start-"
						if suffix == "" {
							if startID, ok := resolveID(prefix); ok {
								addRange(allSteps, idToIndex, selectedMap, &selectedSteps, idToIndex[startID], len(allSteps)-1)
								isRange = true
								break
							}
						}

						// Check "-end"
						if prefix == "" {
							if endID, ok := resolveID(suffix); ok {
								addRange(allSteps, idToIndex, selectedMap, &selectedSteps, 0, idToIndex[endID])
								isRange = true
								break
							}
						}

						// Check "start-end"
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
				// No hyphen, must be single step
				if id, ok := resolveID(part); ok {
					addStep(allSteps, idToIndex, selectedMap, &selectedSteps, id)
				} else {
					return nil, fmt.Errorf("unknown step: %s", part)
				}
			}
		}
	}

	// Re-sort selected steps by index to maintain execution order
	// (Though simple append might have mixed order if user did "c005,c001")
	// For installation, order strictly matters. We should probably return them in the order they appear in allSteps.
	// Filter allSteps based on selectedMap.
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

// normalizeStepID attempts to format "c005" to "C-005"
func normalizeStepID(input string) string {
	// If it's like [letter][digit][digit][digit], add hyphen
	if len(input) == 4 {
		return fmt.Sprintf("%s-%s", strings.ToUpper(input[:1]), input[1:])
	}
	return input
}
