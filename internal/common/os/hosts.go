package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	hostsBeginMarker = "# BEGIN yinstall managed hosts"
	hostsEndMarker   = "# END yinstall managed hosts"
)

// UpdateManagedHostsBlock replaces the yinstall managed block in /etc/hosts
// with the given entries. If no block exists, it appends one.
// entries example: ["10.10.10.125  yashandb01", "10.10.10.126  yashandb02"]
func UpdateManagedHostsBlock(ctx *runner.StepContext, entries []string) error {
	if len(entries) == 0 {
		return nil
	}

	block := hostsBeginMarker + "\\n"
	for _, e := range entries {
		block += e + "\\n"
	}
	block += hostsEndMarker

	removeCmd := fmt.Sprintf(
		`sed -i '/%s/,/%s/d' /etc/hosts`,
		escapeForSed(hostsBeginMarker),
		escapeForSed(hostsEndMarker),
	)

	appendCmd := fmt.Sprintf(`printf '%s\n' >> /etc/hosts`, block)

	fullCmd := removeCmd + " && " + appendCmd
	result, err := ctx.Execute(fullCmd, true)
	if err != nil {
		return fmt.Errorf("failed to update /etc/hosts managed block: %w", err)
	}
	if result != nil && result.GetExitCode() != 0 {
		return fmt.Errorf("failed to update /etc/hosts: %s", result.GetStderr())
	}
	return nil
}

// ReadManagedHostsEntries reads the current entries from the yinstall managed
// block in /etc/hosts. Returns empty slice if no block exists.
func ReadManagedHostsEntries(ctx *runner.StepContext) []string {
	cmd := fmt.Sprintf(
		`sed -n '/%s/,/%s/{/%s/d;/%s/d;p}' /etc/hosts`,
		escapeForSed(hostsBeginMarker),
		escapeForSed(hostsEndMarker),
		escapeForSed(hostsBeginMarker),
		escapeForSed(hostsEndMarker),
	)
	result, _ := ctx.Execute(cmd, true)
	if result == nil || result.GetStdout() == "" {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

func escapeForSed(s string) string {
	r := strings.NewReplacer(
		"/", "\\/",
		"#", "\\#",
	)
	return r.Replace(s)
}
