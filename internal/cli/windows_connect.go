package cli

import (
	"fmt"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/ssh"
)

func createWindowsExecutor(target string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	if flags.Local {
		cfg := ssh.Config{
			Host:       "localhost",
			AuthMethod: "local",
			Logger:     logger,
			StepID:     stepID,
			TargetOS:   ssh.TargetOSWindows,
		}
		return ssh.NewExecutor(cfg)
	}
	return connectWindowsRemote(target, flags, logger, stepID)
}

func connectWindowsRemote(target string, flags GlobalFlags, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	if ex, err := trySSHWindows(target, flags, logger, stepID); err == nil {
		if logger != nil {
			logger.DebugWrite("DEBUG", fmt.Sprintf("step=%s windows connect via ssh %s:%d", stepID, target, flags.SSHPort))
		}
		return ex, nil
	} else if logger != nil {
		logger.DebugWrite("DEBUG", fmt.Sprintf("step=%s ssh %s:%d failed, trying winrm %s:%d: %v",
			stepID, target, flags.SSHPort, target, defaultWinRMPort, err))
	}

	winrmEx, err := createWinRMExecutor(target, flags, logger, stepID)
	if err != nil {
		return nil, err
	}
	if logger != nil {
		logger.DebugWrite("DEBUG", fmt.Sprintf("step=%s windows connect via winrm %s:%d", stepID, target, defaultWinRMPort))
	}
	return winrmEx, nil
}

func createWindowsPrimaryExecutor(cfg PrimarySSHConfig, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	if cfg.Local {
		c := ssh.Config{
			Host:       "localhost",
			AuthMethod: "local",
			Logger:     logger,
			StepID:     stepID,
			TargetOS:   ssh.TargetOSWindows,
		}
		return ssh.NewExecutor(c)
	}
	flags := GlobalFlags{
		SSHPort:     cfg.Port,
		SSHUser:     cfg.User,
		SSHPassword: cfg.Password,
		SSHKeyPath:  cfg.KeyPath,
		SSHAuth:     cfg.Auth,
	}
	return connectWindowsRemote(cfg.Host, flags, logger, stepID)
}
