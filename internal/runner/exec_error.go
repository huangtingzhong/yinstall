package runner

import (
	"errors"
	"fmt"
)

// CommandExitError 表示远端命令以非零退出码结束；Logged 为 true 时终端已输出 LogErrorExit 块。
type CommandExitError struct {
	ExitCode int
	Message  string
	Logged   bool
}

func (e *CommandExitError) Error() string {
	return fmt.Sprintf("command failed with exit code %d: %s", e.ExitCode, e.Message)
}

// NewCommandExitError 构造命令退出错误。
func NewCommandExitError(exitCode int, message string, logged bool) error {
	return &CommandExitError{
		ExitCode: exitCode,
		Message:  message,
		Logged:   logged,
	}
}

// CommandExitLogged 判断 err（含 wrap 链）是否已记录 LogErrorExit。
func CommandExitLogged(err error) bool {
	var ce *CommandExitError
	if errors.As(err, &ce) {
		return ce.Logged
	}
	return false
}
