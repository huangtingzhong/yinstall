package mssql

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/yinstall/internal/runner"
)

// psEscape escapes a string for embedding inside a PowerShell single-quoted
// literal. PS single-quotes escape ' as ”. Backticks and $ are NOT special
// inside single quotes, so they need no escaping.
func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// psSingleQuote wraps s as a PS single-quoted literal: 'foo”bar' -> 'foo”bar'.
func psSingleQuote(s string) string {
	return "'" + psEscape(s) + "'"
}

// PSSingleQuote is the exported form of psSingleQuote for callers outside
// internal/common/mssql (e.g., step packages that need to build single-line
// PS commands).
func PSSingleQuote(s string) string {
	return psSingleQuote(s)
}

// wrapPSCommandArgument wraps a single-line PS command in outer double quotes
// for cmd.exe/SSH shell passing. The cmd must NOT use PS double-quoted strings
// with embedded " (use PS single-quoted strings instead). Caller MUST ensure
// cmd is single-line; multi-line scripts break this wrapping (closing brace
// gets dropped by SSH/cmd.exe transport).
func wrapPSCommandArgument(cmd string) string {
	out := strings.ReplaceAll(cmd, `"`, `\"`)
	return `powershell -NoProfile -Command "` + out + `"`
}

// isSingleLinePSCommand reports whether cmd can be safely executed via
// `powershell -Command "<cmd>"`. Multi-line scripts cannot (SSH/cmd.exe
// transport drops trailing braces; `;`-joining breaks PS arrays and
// multi-line function bodies). They must use -EncodedCommand (base64).
func isSingleLinePSCommand(cmd string) bool {
	return !strings.ContainsRune(cmd, '\n')
}

// encodePSEncodedCommand base64-encodes a PS script as UTF-16LE for
// `powershell -EncodedCommand <base64>`. This is the safe transport for
// multi-line scripts (function definitions, arrays spanning lines,
// here-strings, param() blocks).
func encodePSEncodedCommand(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, r := range u {
		b[i*2] = byte(r)
		b[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// runPSCommand executes a PowerShell command. Single-line commands use
// `-Command "..."` (debuggable); multi-line scripts auto-fall-back to
// `-EncodedCommand <base64>` to avoid shell-quoting issues with newlines,
// arrays, function definitions, and param() blocks.
func runPSCommand(ctx *runner.StepContext, label, cmd string) error {
	if ctx == nil {
		return nil
	}
	ctx.LogScriptPreview("powershell", label, cmd)
	if ctx.DryRun {
		return nil
	}
	var shellCmd string
	if isSingleLinePSCommand(cmd) {
		shellCmd = wrapPSCommandArgument(cmd)
	} else {
		shellCmd = `powershell -NoProfile -EncodedCommand ` + encodePSEncodedCommand(cmd)
	}
	_, err := ctx.ExecuteWithCheck(shellCmd, false)
	return err
}

// runPSCommandScalar runs a PowerShell command and returns stdout.
// See runPSCommand for single-line vs multi-line transport selection.
func runPSCommandScalar(ctx *runner.StepContext, label, cmd string) (string, error) {
	if ctx == nil {
		return "", nil
	}
	ctx.LogScriptPreview("powershell", label, cmd)
	if ctx.DryRun {
		return "", nil
	}
	var shellCmd string
	if isSingleLinePSCommand(cmd) {
		shellCmd = wrapPSCommandArgument(cmd)
	} else {
		shellCmd = `powershell -NoProfile -EncodedCommand ` + encodePSEncodedCommand(cmd)
	}
	res, err := ctx.ExecuteWithCheck(shellCmd, false)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", fmt.Errorf("%s: empty powershell result", label)
	}
	return res.GetStdout(), nil
}

// RunHAPowerShellScript executes a PowerShell command or multi-line script.
// Single-line inputs use -Command; multi-line scripts auto-use -EncodedCommand.
func RunHAPowerShellScript(ctx *runner.StepContext, label, cmd string) error {
	return runPSCommand(ctx, label, cmd)
}

// RunHAPowerShellScalar runs a PowerShell command or multi-line script and
// returns stdout. Single-line inputs use -Command; multi-line scripts
// auto-use -EncodedCommand.
func RunHAPowerShellScalar(ctx *runner.StepContext, label, cmd string) (string, error) {
	return runPSCommandScalar(ctx, label, cmd)
}

// EncodePowerShellCommand is retained for backward compat with existing
// callers (runWSFCPowerShellScalar). It now returns the original script
// unchanged so the transport-selection logic (isSingleLinePSCommand) can
// pick -Command vs -EncodedCommand automatically.
//
// Deprecated: callers should pass scripts directly to RunHAPowerShellScalar
// which auto-selects the appropriate transport.
func EncodePowerShellCommand(script string) string {
	return script
}
