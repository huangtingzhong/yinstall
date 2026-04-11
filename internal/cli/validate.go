package cli

import "fmt"

// validatePort 校验端口号合法性（1-65535）
func validatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid %s: %d (must be between 1 and 65535)", name, port)
	}
	return nil
}

// validatePorts 批量校验端口号
func validatePorts(ports map[string]int) error {
	for name, port := range ports {
		if err := validatePort(name, port); err != nil {
			return err
		}
	}
	return nil
}

// validateMemoryPercent 校验内存百分比（1-100）
func validateMemoryPercent(name string, pct int) error {
	if pct < 1 || pct > 100 {
		return fmt.Errorf("invalid %s: %d (must be between 1 and 100)", name, pct)
	}
	return nil
}
