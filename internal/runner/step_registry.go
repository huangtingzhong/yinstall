package runner

import (
	"fmt"
	"strings"
)

// StepEntry 描述 registry 中一项步骤工厂；FixedID 非空时跳过自动编号。
type StepEntry struct {
	New     func() *Step
	FixedID string
}

// StepSpec registry 规格：Prefix + 有序 Entries → 连续 ID。
type StepSpec struct {
	Prefix  string
	Width   int
	Entries []StepEntry
}

// BuildSteps 按 Entries 顺序构建步骤并赋值 ID。
// 自动编号：{Prefix}-{seq:0Width}；FixedID 条目使用固定 ID 且不计入 seq。
func BuildSteps(spec StepSpec) []*Step {
	width := spec.Width
	if width <= 0 {
		width = 3
	}
	prefix := strings.TrimSpace(spec.Prefix)
	seq := 0
	out := make([]*Step, 0, len(spec.Entries))
	for _, e := range spec.Entries {
		if e.New == nil {
			continue
		}
		s := e.New()
		if s == nil {
			continue
		}
		fixed := strings.TrimSpace(e.FixedID)
		if fixed != "" {
			s.ID = fixed
		} else {
			seq++
			s.ID = fmt.Sprintf("%s-%0*d", prefix, width, seq)
		}
		out = append(out, s)
	}
	return out
}

// FirstStepID returns the ID of the first step, or prefix-001 if empty.
func FirstStepID(steps []*Step, prefix string) string {
	if len(steps) > 0 && steps[0] != nil && steps[0].ID != "" {
		return steps[0].ID
	}
	width := 3
	if prefix == "" {
		return ""
	}
	return fmt.Sprintf("%s-%0*d", prefix, width, 1)
}

// StepMsg prefixes text with ctx.CurrentStepID, stripping a legacy hardcoded step prefix if present.
func StepMsg(ctx *StepContext, text string) string {
	if ctx == nil || ctx.CurrentStepID == "" {
		return text
	}
	if idx := strings.Index(text, ": "); idx > 0 {
		if looksLikeStepID(text[:idx]) {
			text = text[idx+2:]
		}
	}
	return ctx.CurrentStepID + ": " + text
}

func looksLikeStepID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	i := strings.LastIndex(s, "-")
	if i <= 0 || i >= len(s)-1 {
		return false
	}
	for _, c := range s[i+1:] {
		if c < '0' || c > '9' {
			if c >= 'a' && c <= 'z' {
				continue
			}
			return false
		}
	}
	return true
}

// StepByName returns the first step with an exact Name match.
func StepByName(steps []*Step, name string) *Step {
	for _, s := range steps {
		if s != nil && s.Name == name {
			return s
		}
	}
	return nil
}

// StepIDByName returns the ID of the step with the given Name, or "" if not found.
func StepIDByName(steps []*Step, name string) string {
	if s := StepByName(steps, name); s != nil {
		return s.ID
	}
	return ""
}

// StepByTag returns the first step containing tag in Tags.
func StepByTag(steps []*Step, tag string) *Step {
	for _, s := range steps {
		if s == nil {
			continue
		}
		for _, t := range s.Tags {
			if t == tag {
				return s
			}
		}
	}
	return nil
}

// CloneStep 浅拷贝 Step（含 Tags 切片）；用于 collect/stress 包装 os 步骤。
func CloneStep(s *Step) *Step {
	if s == nil {
		return nil
	}
	cp := *s
	if len(s.Tags) > 0 {
		cp.Tags = append([]string(nil), s.Tags...)
	}
	return &cp
}
