package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Logger 日志管理器
type Logger struct {
	runID          string
	logDir         string
	sessionFile    *os.File // session log = mirrors terminal output
	debugFile      *os.File // debug log = all detailed logs
	sessionLogPath string
	debugLogPath   string
	mu             sync.Mutex
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	RunID     string `json:"run_id"`
	Host      string `json:"host,omitempty"`
	StepID    string `json:"step_id,omitempty"`
	Level     string `json:"level"`
	Phase     string `json:"phase,omitempty"` // start, success, fail, skip
	Message   string `json:"message"`
	Command   string `json:"command,omitempty"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// NewLogger 创建日志管理器，打印 banner 到终端和 session 日志
func NewLogger(runID, logDir, version, author, contact string) (*Logger, error) {
	// 检查并创建日志目录
	if err := ensureDirectory(logDir); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	sessionPath := filepath.Join(logDir, fmt.Sprintf("yinstall_%s_%s.log", timestamp, runID))
	debugPath := filepath.Join(logDir, fmt.Sprintf("yinstall_%s_%s_debug.log", timestamp, runID))

	sessionFile, err := os.Create(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create session log: %w", err)
	}

	debugFile, err := os.Create(debugPath)
	if err != nil {
		sessionFile.Close()
		return nil, fmt.Errorf("failed to create debug log: %w", err)
	}

	l := &Logger{
		runID:          runID,
		logDir:         logDir,
		sessionFile:    sessionFile,
		debugFile:      debugFile,
		sessionLogPath: sessionPath,
		debugLogPath:   debugPath,
	}

	// Print banner to terminal + session log
	banner := fmt.Sprintf("Version: %s\nAuthor: %s\nContact: %s\n\nThe log of current session can be found at:\n  %s\nDebug log can be found at:\n  %s\n",
		version, author, contact, sessionPath, debugPath)
	fmt.Print(banner)
	sessionFile.WriteString(banner)

	// Also write banner to debug log
	debugFile.WriteString(banner)

	return l, nil
}

// SessionLogPath 返回 session 日志路径
func (l *Logger) SessionLogPath() string {
	return l.sessionLogPath
}

// DebugLogPath 返回 debug 日志路径
func (l *Logger) DebugLogPath() string {
	return l.debugLogPath
}

// LogDir 返回日志目录
func (l *Logger) LogDir() string {
	return l.logDir
}

// ConsoleStep 输出步骤进度到终端和 session 日志
// phase: start, success, fail, skip
func (l *Logger) ConsoleStep(stepID, stepName string, stepIndex, totalSteps int, phase string, duration time.Duration) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	var line string
	switch phase {
	case "start":
		line = fmt.Sprintf("%s %s: Executing installation step %d of %d: '%s'\n",
			timestamp, stepID, stepIndex, totalSteps, stepName)
	case "success":
		line = fmt.Sprintf("%s %s: Step %d completed successfully: '%s' (%.2fs)\n",
			timestamp, stepID, stepIndex, stepName, duration.Seconds())
	case "fail":
		line = fmt.Sprintf("%s %s: Step %d failed: '%s' (%.2fs)\n",
			timestamp, stepID, stepIndex, stepName, duration.Seconds())
	case "skip":
		line = fmt.Sprintf("%s %s: Step %d skipped: '%s'\n",
			timestamp, stepID, stepIndex, stepName)
	default:
		line = fmt.Sprintf("%s %s: Step %d [%s]: '%s'\n",
			timestamp, stepID, stepIndex, phase, stepName)
	}

	l.mu.Lock()
	fmt.Print(line)
	l.sessionFile.WriteString(line)
	l.mu.Unlock()

	// Also write to debug log
	l.debugWrite("STEP", line)
}

// Info 写入 debug 日志（不输出到终端）
func (l *Logger) Info(format string, args ...interface{}) {
	l.debugWrite("INFO", fmt.Sprintf(format, args...))
}

// Error 写入 debug 日志（不输出到终端）
func (l *Logger) Error(format string, args ...interface{}) {
	l.debugWrite("ERROR", fmt.Sprintf(format, args...))
}

// Warn 写入 debug 日志（不输出到终端）
func (l *Logger) Warn(format string, args ...interface{}) {
	l.debugWrite("WARN", fmt.Sprintf(format, args...))
}

// debugWrite 写入 debug 日志文件
func (l *Logger) debugWrite(level, msg string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s [%s] %s\n", timestamp, level, strings.TrimRight(msg, "\n"))
	l.mu.Lock()
	l.debugFile.WriteString(line)
	l.mu.Unlock()
}

// LogErrorExit 统一报错退出：将执行的命令、stdout、stderr、退出码、错误信息输出到终端和日志
func (l *Logger) LogErrorExit(host, stepID, stepName, command, stdout, stderr string, exitCode int, errMsg string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	lines := []string{
		"",
		fmt.Sprintf("%s ========== Error Exit ==========", timestamp),
		fmt.Sprintf("  Host: %s", host),
		fmt.Sprintf("  Step: %s %s", stepID, stepName),
	}
	if command != "" {
		lines = append(lines, "  --- Command ---", indentBlock(command), "")
	}
	if exitCode >= 0 {
		lines = append(lines, fmt.Sprintf("  Exit Code: %d", exitCode))
	}
	if stdout != "" {
		lines = append(lines, "  --- Stdout ---", indentBlock(stdout), "")
	}
	if stderr != "" {
		lines = append(lines, "  --- Stderr ---", indentBlock(stderr), "")
	}
	lines = append(lines, "  --- Error ---", indentBlock(errMsg), "================================", "")

	block := strings.Join(lines, "\n")
	l.mu.Lock()
	fmt.Print(block)
	l.sessionFile.WriteString(block)
	l.mu.Unlock()

	// Also write to debug log
	l.debugWrite("ERROR", block)
}

func indentBlock(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i := range lines {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// Debug 写入 debug 日志
func (l *Logger) Debug(entry LogEntry) {
	entry.Timestamp = time.Now().Format(time.RFC3339)
	entry.RunID = l.runID

	// 脱敏处理
	entry.Command = redact(entry.Command)
	entry.Stdout = redact(entry.Stdout)
	entry.Stderr = redact(entry.Stderr)
	entry.Message = redact(entry.Message)

	var parts []string
	parts = append(parts, fmt.Sprintf("host=%s step=%s level=%s", entry.Host, entry.StepID, entry.Level))
	if entry.Phase != "" {
		parts = append(parts, fmt.Sprintf("phase=%s", entry.Phase))
	}
	if entry.Message != "" {
		parts = append(parts, fmt.Sprintf("msg=%s", entry.Message))
	}
	if entry.Command != "" {
		parts = append(parts, fmt.Sprintf("cmd=%s", entry.Command))
	}
	if entry.Stdout != "" {
		parts = append(parts, fmt.Sprintf("stdout=%s", entry.Stdout))
	}
	if entry.Stderr != "" {
		parts = append(parts, fmt.Sprintf("stderr=%s", entry.Stderr))
	}
	if entry.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit_code=%d", entry.ExitCode))
	}
	if entry.Duration != "" {
		parts = append(parts, fmt.Sprintf("duration=%s", entry.Duration))
	}

	l.debugWrite(strings.ToUpper(entry.Level), strings.Join(parts, " "))
}

// LogCommand 记录命令执行到 debug 日志
func (l *Logger) LogCommand(host, stepID, command string, stdout, stderr string, exitCode int, duration time.Duration) {
	l.Debug(LogEntry{
		Host:     host,
		StepID:   stepID,
		Level:    "debug",
		Command:  command,
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Duration: duration.String(),
	})
}

// LogStepStart 记录步骤开始到 debug 日志
func (l *Logger) LogStepStart(host, stepID, stepName string) {
	l.Debug(LogEntry{
		Host:    host,
		StepID:  stepID,
		Level:   "info",
		Phase:   "start",
		Message: stepName,
	})
}

// LogStepEnd 记录步骤结束到 debug 日志
func (l *Logger) LogStepEnd(host, stepID, stepName string, success bool, duration time.Duration, errMsg string) {
	phase := "success"
	if !success {
		phase = "fail"
	}
	l.Debug(LogEntry{
		Host:     host,
		StepID:   stepID,
		Level:    "info",
		Phase:    phase,
		Message:  stepName + ": " + errMsg,
		Duration: duration.String(),
	})
}

// Close 关闭所有日志文件
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.sessionFile != nil {
		l.sessionFile.Close()
	}
	if l.debugFile != nil {
		l.debugFile.Close()
	}
}

// ---- Legacy compatibility methods (delegate to new methods) ----

// Console 兼容旧接口，输出步骤到终端
func (l *Logger) Console(stepID, stepName, host, phase string, msg string, duration time.Duration) {
	// Legacy: just write to debug log (callers should use ConsoleStep now)
	l.debugWrite("CONSOLE", fmt.Sprintf("[%s] %s host=%s phase=%s msg=%s duration=%s", stepID, stepName, host, phase, msg, duration))
}

// ConsoleWithType 兼容旧接口
func (l *Logger) ConsoleWithType(stepID, stepName, host, phase, execType string, msg string, duration time.Duration) {
	// Legacy: just write to debug log (callers should use ConsoleStep now)
	l.debugWrite("CONSOLE", fmt.Sprintf("[%s] %s host=%s phase=%s type=%s msg=%s duration=%s", stepID, stepName, host, phase, execType, msg, duration))
}

// 敏感信息脱敏
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|key)[\s]*[=:]\s*['"]?([^'";\s]+)`),
	regexp.MustCompile(`(?i)echo\s+['"]?[^'"]+['"]?\s*\|\s*passwd`),
}

func redact(s string) string {
	result := s
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := strings.SplitN(match, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=***REDACTED***"
			}
			parts = strings.SplitN(match, ":", 2)
			if len(parts) == 2 {
				return parts[0] + ":***REDACTED***"
			}
			if strings.Contains(strings.ToLower(match), "passwd") {
				return "echo '***REDACTED***'|passwd"
			}
			return "***REDACTED***"
		})
	}
	return result
}

// ensureDirectory 确保目录存在，如果存在则不创建，如果存在同名文件则删除并创建目录
// 跨平台兼容：Windows 和 Unix/Linux
// 递归检查父路径，确保父路径都是目录而不是文件
func ensureDirectory(dir string) error {
	// 首先尝试直接创建目录（如果已存在且是目录，会返回 nil）
	var perm os.FileMode
	if runtime.GOOS == "windows" {
		perm = os.ModePerm
	} else {
		perm = 0700
	}

	// 尝试创建目录（包括所有必要的父目录）
	if err := os.MkdirAll(dir, perm); err != nil {
		// 如果创建失败，检查是否是因为同名文件存在
		if info, statErr := os.Stat(dir); statErr == nil {
			// 路径存在但创建失败，可能是因为是文件而不是目录
			if !info.IsDir() {
				// 存在同名文件，需要删除
				if err := os.Remove(dir); err != nil {
					if runtime.GOOS == "windows" {
						return fmt.Errorf("path %s exists but is a file, not a directory. Please close any programs using this file and try again, or manually delete it: %w", dir, err)
					}
					return fmt.Errorf("path %s exists but is not a directory, failed to remove: %w", dir, err)
				}
				// 文件删除成功，再次尝试创建目录
				return os.MkdirAll(dir, perm)
			}
			// 是目录，不应报错，返回原始错误
		}
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return nil
}
