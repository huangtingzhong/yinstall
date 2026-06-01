package runner

// StepProgress 维护「有意义」的安装进度：仅对实际计入进度的步骤递增序号；
// Optional 步骤在 PreCheck 判定跳过时既不占序号也不占总数，若实际执行则动态扩大 Total。
type StepProgress struct {
	total int
	next  int
}

// NewStepProgress 创建进度计数器；plannedTotal 通常为非 Optional 步骤数（安装 + 挂钩 collect 等）。
func NewStepProgress(plannedTotal int) *StepProgress {
	if plannedTotal < 0 {
		plannedTotal = 0
	}
	return &StepProgress{total: plannedTotal}
}

// Total 返回当前分母（含运行中动态纳入的 Optional 步骤）。
func (p *StepProgress) Total() int {
	if p == nil {
		return 0
	}
	return p.total
}

// Next 返回下一条将分配的序号（已分配步数），用于挂钩 collect 的起始 offset。
func (p *StepProgress) Next() int {
	if p == nil {
		return 0
	}
	return p.next
}

// assignProgress 为即将执行的步骤分配 (index, total)。
func (p *StepProgress) assignProgress() (index, total int) {
	if p == nil {
		return 0, 0
	}
	index = p.next
	total = p.total
	p.next++
	return index, total
}

// includeOptionalRunning 在 Optional 步骤确定执行（PreCheck 通过）时扩大 Total 并分配序号。
func (p *StepProgress) includeOptionalRunning() (index, total int) {
	if p == nil {
		return 0, 0
	}
	p.total++
	return p.assignProgress()
}

// CountNonOptionalSteps 统计步骤列表中非 Optional 的条目数（用于进度分母初值）。
func CountNonOptionalSteps(steps []*Step) int {
	n := 0
	for _, s := range steps {
		if s != nil && !s.Optional {
			n++
		}
	}
	return n
}

// CountCollectProgressSteps 统计 collect 管线进度分母；skipConnectivity 为 true 时不计 R-001（安装挂钩复用连接）。
func CountCollectProgressSteps(steps []*Step, skipConnectivity bool) int {
	n := 0
	for _, s := range steps {
		if s == nil {
			continue
		}
		if skipConnectivity && s.ID == "R-001" {
			continue
		}
		if !s.Optional {
			n++
		}
	}
	return n
}
