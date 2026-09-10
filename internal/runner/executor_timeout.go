package runner

import "time"

// ExecuteTimeoutSetter allows extending remote command timeout when the transport supports it.
// Pass 0 to restore the default.
type ExecuteTimeoutSetter interface {
	SetExecuteTimeout(d time.Duration)
}

// SetExecuteTimeout adjusts remote execute timeout when supported by the transport.
func (ctx *StepContext) SetExecuteTimeout(d time.Duration) {
	if ctx == nil || ctx.Executor == nil {
		return
	}
	if s, ok := ctx.Executor.(ExecuteTimeoutSetter); ok {
		s.SetExecuteTimeout(d)
	}
}
