package runner

import "time"

// ExecuteTimeoutSetter allows extending WinRM command timeout (default 30m).
// Pass 0 to restore the default.
type ExecuteTimeoutSetter interface {
	SetExecuteTimeout(d time.Duration)
}

// SetExecuteTimeout adjusts remote execute timeout when supported (WinRM).
func (ctx *StepContext) SetExecuteTimeout(d time.Duration) {
	if ctx == nil || ctx.Executor == nil {
		return
	}
	if s, ok := ctx.Executor.(ExecuteTimeoutSetter); ok {
		s.SetExecuteTimeout(d)
	}
}
