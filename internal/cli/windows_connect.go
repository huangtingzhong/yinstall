package cli

import (
	"fmt"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/ssh"
)

// createWindowsExecutor rejects Windows targets on yinstall (hetero installers live in install CLI).
func createWindowsExecutor(target string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	_ = flags
	_ = logger
	_ = stepID
	return nil, fmt.Errorf("Windows targets are not supported by yinstall; use the install CLI for MySQL/SQL Server (target %s)", target)
}

// createWindowsPrimaryExecutor rejects Windows primary hosts on yinstall.
func createWindowsPrimaryExecutor(cfg PrimarySSHConfig, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	_ = logger
	_ = stepID
	return nil, fmt.Errorf("Windows targets are not supported by yinstall; use the install CLI for MySQL/SQL Server (target %s)", cfg.Host)
}
