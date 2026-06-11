package cli

import (
	"strings"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/ssh"
)

func isLocalHost(host string) bool {
	h := strings.TrimSpace(strings.ToLower(host))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// PrimarySSHConfig holds SSH settings for connecting to a primary host.
type PrimarySSHConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	KeyPath  string
	Auth     string
	Local    bool
}

// createPrimaryExecutor connects to the primary database host.
func createPrimaryExecutor(cfg PrimarySSHConfig, logger *logging.Logger, stepID string) (ssh.Executor, error) {
	sshCfg := ssh.Config{
		Host:       cfg.Host,
		Port:       cfg.Port,
		User:       cfg.User,
		AuthMethod: cfg.Auth,
		Password:   cfg.Password,
		KeyPath:    cfg.KeyPath,
		Logger:     logger,
		StepID:     stepID,
	}
	if cfg.Local {
		sshCfg.AuthMethod = "local"
	}
	return connectSSHWithRetry(sshCfg, cfg.Password != "", logger)
}
