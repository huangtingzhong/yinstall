package runner

import "errors"

// StepSkippedError 表示步骤因前置条件（如无 root/sudo）被主动跳过，非失败。
type StepSkippedError struct {
	Reason string
}

func (e *StepSkippedError) Error() string {
	if e == nil || e.Reason == "" {
		return "step skipped"
	}
	return e.Reason
}

// NewStepSkippedError 构造步骤跳过错误（RunStep 会在终端输出 skipped）。
func NewStepSkippedError(reason string) error {
	return &StepSkippedError{Reason: reason}
}

// IsStepSkipped 判断 err 是否为 StepSkippedError。
func IsStepSkipped(err error) bool {
	var se *StepSkippedError
	return errors.As(err, &se)
}
