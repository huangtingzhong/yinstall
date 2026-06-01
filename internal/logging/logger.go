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

// redactSensitive 为 true 时对日志中的密码/密钥等做 ***REDACTED*** 脱敏；默认 false（明文，便于排障）。
var redactSensitive bool

// SetRedactSensitive 设置是否在 session/debug 日志中脱敏密码等敏感字段。
func SetRedactSensitive(enabled bool) {
	redactSensitive = enabled
}

// RedactSensitive 返回当前是否启用日志脱敏。
func RedactSensitive() bool {
	return redactSensitive
}

// Logger 日志管理器
type Logger struct {
	runID          string
	logDir         string
	sessionFile    *os.File // session 日志：与终端输出保持一致（人类可读）
	debugFile      *os.File // debug 日志：记录更详细的信息（含命令输出等）
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
	Phase     string `json:"phase,omitempty"` // start/success/fail/skip
	Message   string `json:"message"`
	Command   string `json:"command,omitempty"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// LogTimestampFormat 为日志文件名中的紧凑时间戳（与 runID 后缀一致）。
const LogTimestampFormat = "20060102150405"

var (
	runIDTSCompactSuffix = regexp.MustCompile(`-(\d{14})$`)
	runIDTSHyphenSuffix  = regexp.MustCompile(`-(\d{8})-(\d{6})$`)
	runIDStripSuffix     = regexp.MustCompile(`-(?:\d{14}|\d{8}-\d{6})$`)
)

// SessionAndDebugLogPaths 生成 session / debug 日志绝对路径：
// yinstall_<type>_<timestamp>.log 与 yinstall_<type>_debug_<timestamp>.log
func SessionAndDebugLogPaths(logDir, runID string, now time.Time) (sessionPath, debugPath string, err error) {
	absLogDir, err := filepath.Abs(logDir)
	if err != nil {
		absLogDir = logDir
	}
	logType, ts := LogTypeAndTimestamp(runID, now)
	sessionPath = filepath.Join(absLogDir, "yinstall_"+logType+"_"+ts+".log")
	debugPath = filepath.Join(absLogDir, "yinstall_"+logType+"_debug_"+ts+".log")
	return sessionPath, debugPath, nil
}

// LogTypeAndTimestamp 从 runID 解析类型与时间戳；无法解析时间戳时用 now。
func LogTypeAndTimestamp(runID string, now time.Time) (logType, compactTS string) {
	compactTS = compactTimestampFromRunID(runID)
	if compactTS == "" {
		compactTS = now.Format(LogTimestampFormat)
	}
	logType = sanitizeLogType(logTypeFromRunID(runID))
	return logType, compactTS
}

func logTypeFromRunID(runID string) string {
	if runID == "" {
		return "run"
	}
	typ := runIDStripSuffix.ReplaceAllString(runID, "")
	if typ == "" {
		return runID
	}
	return typ
}

func compactTimestampFromRunID(runID string) string {
	if m := runIDTSHyphenSuffix.FindStringSubmatch(runID); len(m) == 3 {
		return m[1] + m[2]
	}
	if m := runIDTSCompactSuffix.FindStringSubmatch(runID); len(m) == 2 {
		return m[1]
	}
	return ""
}

func sanitizeLogType(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "run"
	}
	return out
}

// NewLogger 创建日志管理器，打印 banner 到终端和 session 日志
func NewLogger(runID, logDir, version, author, contact string) (*Logger, error) {
	// 检查并创建日志目录
	if err := ensureDirectory(logDir); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// 转换为绝对路径，确保 banner 中展示全路径（兼容 Windows/macOS/Linux）
	absLogDir, err := filepath.Abs(logDir)
	if err != nil {
		absLogDir = logDir // 转换失败时回退原值
	}

	now := time.Now()
	sessionPath, debugPath, err := SessionAndDebugLogPaths(absLogDir, runID, now)
	if err != nil {
		return nil, fmt.Errorf("failed to build log paths: %w", err)
	}

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

	// 将 banner 输出到终端 + session 日志
	banner := fmt.Sprintf("Version: %s\nAuthor: %s\nContact: %s\n\nThe log of current session can be found at:\n  %s\nDebug log can be found at:\n  %s\n",
		version, author, contact, sessionPath, debugPath)
	fmt.Print(banner)
	sessionFile.WriteString(banner)

	// 同时写入 debug 日志
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
	if l.sessionFile != nil {
		l.sessionFile.WriteString(line)
	}
	l.mu.Unlock()

	l.debugWrite("STEP", line)
}

// ConsoleStepSkipped 输出不计入进度总数的跳过（Optional 条件不满足等）。
func (l *Logger) ConsoleStepSkipped(stepID, stepName string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s %s: skipped (not in progress total): '%s'\n", timestamp, stepID, stepName)
	l.mu.Lock()
	fmt.Print(line)
	if l.sessionFile != nil {
		l.sessionFile.WriteString(line)
	}
	l.mu.Unlock()
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

// debugWrite 写入 debug 日志文件；time.Now() 在锁内取值保证时间戳有序
func (l *Logger) debugWrite(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.debugFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s [%s] %s\n", timestamp, level, strings.TrimRight(msg, "\n"))
	l.debugFile.WriteString(line)
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
	command = redact(command)
	stdout = redact(stdout)
	stderr = redact(stderr)
	errMsg = redact(errMsg)

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
	if l.sessionFile != nil {
		l.sessionFile.WriteString(block)
	}
	l.mu.Unlock()

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

// maxScriptPreviewLines 限制单次脚本预览的最大行数，避免超大 SQL/脚本撑爆 debug 日志。
const maxScriptPreviewLines = 256

// LogScriptPreview 在执行 shell/SQL 脚本正文前写入 debug（多行；--log-redact 时脱敏）；超长截断。
// scriptKind 示例：shell、sql；label 可为空或远端路径等附注。
func (l *Logger) LogScriptPreview(host, stepID, scriptKind, label, body string) {
	if l == nil {
		return
	}
	scriptKind = strings.TrimSpace(scriptKind)
	if scriptKind == "" {
		scriptKind = "script"
	}
	prefix := fmt.Sprintf("host=%s step=%s script=%s", host, stepID, scriptKind)
	if label = strings.TrimSpace(label); label != "" {
		prefix += " label=" + label
	}
	l.debugWrite("DEBUG", prefix+" >>> body (before execute):")
	body = strings.TrimRight(redact(body), "\n")
	if body == "" {
		l.debugWrite("DEBUG", prefix+" body| (empty)")
		return
	}
	lines := strings.Split(body, "\n")
	omitted := 0
	if len(lines) > maxScriptPreviewLines {
		omitted = len(lines) - maxScriptPreviewLines
		lines = lines[:maxScriptPreviewLines]
	}
	for _, line := range lines {
		l.debugWrite("DEBUG", fmt.Sprintf("%s body| %s", prefix, line))
	}
	if omitted > 0 {
		l.debugWrite("DEBUG", fmt.Sprintf("%s body| ... (%d lines omitted)", prefix, omitted))
	}
}

// LogCommandStart 在命令执行前记录到 debug 日志
func (l *Logger) LogCommandStart(host, stepID, command string) {
	command = redact(command)
	l.debugWrite("DEBUG", fmt.Sprintf("host=%s step=%s >>> %s", host, stepID, command))
}

// logCommandStream 将单路输出逐行写入 debug；无内容时写一行 (empty)，便于区分「无输出」与「未记录」。
func (l *Logger) logCommandStream(prefix, label, s string) {
	if s == "" {
		l.debugWrite("DEBUG", fmt.Sprintf("%s %s| (empty)", prefix, label))
		return
	}
	for _, line := range strings.Split(s, "\n") {
		l.debugWrite("DEBUG", fmt.Sprintf("%s %s| %s", prefix, label, line))
	}
}

// LogCommandResult 在命令执行后记录结果到 debug 日志（每个字段独立一行）
func (l *Logger) LogCommandResult(host, stepID string, stdout, stderr string, exitCode int, duration time.Duration) {
	stdout = redact(strings.TrimRight(stdout, "\n"))
	stderr = redact(strings.TrimRight(stderr, "\n"))
	prefix := fmt.Sprintf("host=%s step=%s", host, stepID)

	l.debugWrite("DEBUG", fmt.Sprintf("%s exit_code=%d duration=%s", prefix, exitCode, duration))
	l.logCommandStream(prefix, "stdout", stdout)
	l.logCommandStream(prefix, "stderr", stderr)
}

// LogCommand 兼容旧接口（合并 start + result）
func (l *Logger) LogCommand(host, stepID, command string, stdout, stderr string, exitCode int, duration time.Duration) {
	l.LogCommandStart(host, stepID, command)
	l.LogCommandResult(host, stepID, stdout, stderr, exitCode, duration)
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

// Close 关闭所有日志文件；先 Sync 刷盘再关闭，关闭后置 nil 防止后续写入 panic
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.sessionFile != nil {
		_ = l.sessionFile.Sync()
		_ = l.sessionFile.Close()
		l.sessionFile = nil
	}
	if l.debugFile != nil {
		_ = l.debugFile.Sync()
		_ = l.debugFile.Close()
		l.debugFile = nil
	}
}

// ConsoleNotice 向终端与 session 输出一行说明（用于步骤内子阶段跳过等）。
func (l *Logger) ConsoleNotice(stepID, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s %s: %s\n", timestamp, stepID, strings.TrimSpace(message))
	l.mu.Lock()
	fmt.Print(line)
	if l.sessionFile != nil {
		_, _ = l.sessionFile.WriteString(line)
	}
	l.mu.Unlock()
	l.debugWrite("NOTICE", line)
}

// ConsolePrecheckIssue prints a single precheck issue line to terminal + session log.
// This is for --precheck readability (no JSON output).
func (l *Logger) ConsolePrecheckIssue(stepID, stepName, host, severity, code, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	codePart := ""
	if strings.TrimSpace(code) != "" {
		codePart = " " + strings.TrimSpace(code)
	}
	line := fmt.Sprintf("%s %s: [precheck-%s]%s (%s) %s - %s\n",
		timestamp, stepID, strings.ToLower(strings.TrimSpace(severity)), codePart, host, stepName, strings.TrimSpace(message))
	l.mu.Lock()
	fmt.Print(line)
	if l.sessionFile != nil {
		_, _ = l.sessionFile.WriteString(line)
	}
	l.mu.Unlock()
	l.debugWrite("PRECHECK", line)
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

// 敏感信息脱敏正则
// 1. key=value / key:value 格式（password、passwd、pwd、secret、token、api_key 等）
// 2. echo ... | passwd 格式
// 3. --password value / --passwd value 命令行参数格式
// 4. yasboot 风格 -p 'secret'（非 --password）
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|secret[_-]?key|private[_-]?key)[\s]*[=:]\s*['"]?([^'";\s]+)`),
	regexp.MustCompile(`(?i)echo\s+['"]?[^'"]+['"]?\s*\|\s*passwd`),
	regexp.MustCompile(`(?i)--(?:password|passwd|pwd|secret|token)\s+['"]?([^'";\s]+)['"]?`),
	regexp.MustCompile(`(?i)(?:^|\s)-p\s+(?:'[^']*'|"[^"]*"|\S+)`),
}

func redact(s string) string {
	if !redactSensitive || s == "" {
		return s
	}
	result := s
	for i, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			switch i {
			case 0:
				if idx := strings.IndexAny(match, "=:"); idx >= 0 {
					return match[:idx+1] + "***REDACTED***"
				}
			case 1:
				return "echo '***REDACTED***'|passwd"
			case 2:
				parts := strings.Fields(match)
				if len(parts) >= 2 {
					return parts[0] + " ***REDACTED***"
				}
			case 3:
				if idx := strings.Index(strings.ToLower(match), "-p"); idx >= 0 {
					return match[:idx+2] + " ***REDACTED***"
				}
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
