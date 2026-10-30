// config_file.go - CLI 参数 YAML 配置文件的加载、灌参与回写
// 扁平键对齐 cobra long flag; CLI Changed 优先; 结束时原子覆盖写回

package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/yinstall/internal/logging"
	"gopkg.in/yaml.v3"
)

// ConfigWarnFunc 未知键等警告回调 (通常接到 stderr 或 logger.Warn).
type ConfigWarnFunc func(format string, args ...interface{})

// configProcessLog 记录参数文件全过程 (读/灌参/跳过/写回), 供写入 debug 日志.
var (
	configProcessMu    sync.Mutex
	configProcessLog   []string
	configDebugLogPath string // 最近一次 session debug 路径, 供写回阶段追加
)

func clearConfigProcessLog() {
	configProcessMu.Lock()
	configProcessLog = nil
	configProcessMu.Unlock()
}

// ClearConfigProcessLog 清空过程日志 (测试用).
func ClearConfigProcessLog() {
	clearConfigProcessLog()
}

func logConfigProcess(format string, args ...interface{}) {
	configProcessMu.Lock()
	configProcessLog = append(configProcessLog, fmt.Sprintf(format, args...))
	configProcessMu.Unlock()
}

// ConfigProcessLogLines 返回参数文件过程日志副本 (测试用).
func ConfigProcessLogLines() []string {
	configProcessMu.Lock()
	defer configProcessMu.Unlock()
	out := make([]string, len(configProcessLog))
	copy(out, configProcessLog)
	return out
}

// flushConfigProcessLogToLogger 将已缓冲的过程日志写入 debug (session logger 创建后调用).
func flushConfigProcessLogToLogger(logger *logging.Logger) {
	if logger == nil {
		return
	}
	configDebugLogPath = logger.DebugLogPath()
	configProcessMu.Lock()
	lines := append([]string(nil), configProcessLog...)
	configProcessMu.Unlock()
	if len(lines) == 0 {
		return
	}
	logger.Info("config process| ----- begin (%d lines) -----", len(lines))
	for _, line := range lines {
		logger.Info("%s", line)
	}
	logger.Info("config process| ----- end (buffered) -----")
}

// appendConfigProcessLogToDebugFile 在 logger 已关闭后追加写回阶段日志到 debug 文件.
func appendConfigProcessLogToDebugFile(fromIndex int) {
	path := strings.TrimSpace(configDebugLogPath)
	configProcessMu.Lock()
	if path == "" || fromIndex >= len(configProcessLog) {
		configProcessMu.Unlock()
		return
	}
	lines := append([]string(nil), configProcessLog[fromIndex:]...)
	configProcessMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05")
	for _, line := range lines {
		_, _ = fmt.Fprintf(f, "%s [INFO] %s\n", ts, line)
	}
}

func formatConfigValue(v interface{}) string {
	s, err := YAMLValueToFlagString(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func sortedConfigKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// LoadConfigFileMap 读取扁平 YAML 为 map; 根节点必须是 mapping.
func LoadConfigFileMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	var root interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}
	m, ok := root.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("config file %s: root must be a mapping", path)
	}
	return m, nil
}

// YAMLValueToFlagString 将 YAML 标量/列表转为 pflag.Set 可用的字符串.
func YAMLValueToFlagString(v interface{}) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", fmt.Errorf("nil value")
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	case float64:
		// yaml 有时把整数解成 float64
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case []interface{}:
		parts := make([]string, 0, len(x))
		for i, e := range x {
			s, err := YAMLValueToFlagString(e)
			if err != nil {
				return "", fmt.Errorf("list index %d: %w", i, err)
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("unsupported config value type %T", v)
	}
}

// ApplyConfigMapToCommand 将扁平配置灌入 cmd 的 flags; 已 Changed 的跳过; 未知键 warn.
// 每个键的处理结果写入 configProcessLog (debug).
func ApplyConfigMapToCommand(cmd *cobra.Command, m map[string]interface{}, warn ConfigWarnFunc) error {
	if warn == nil {
		warn = func(string, ...interface{}) {}
	}
	for _, k := range sortedConfigKeys(m) {
		v := m[k]
		name := strings.TrimSpace(k)
		if name == "" {
			continue
		}
		valStr := formatConfigValue(v)
		if name == "config" || name == "c" {
			warn("config key %q is reserved and ignored", name)
			logConfigProcess("config apply| key=%s action=ignore_reserved value=%s", name, valStr)
			continue
		}
		f := cmd.Flag(name)
		if f == nil {
			warn("unknown config key %q (ignored)", name)
			logConfigProcess("config apply| key=%s action=ignore_unknown value=%s", name, valStr)
			continue
		}
		if f.Changed {
			logConfigProcess("config apply| key=%s action=skip_cli_override file=%s cli=%s", name, valStr, f.Value.String())
			continue
		}
		str, err := YAMLValueToFlagString(v)
		if err != nil {
			return fmt.Errorf("config key %q: %w", name, err)
		}
		if err := f.Value.Set(str); err != nil {
			return fmt.Errorf("config key %q: %w", name, err)
		}
		f.Changed = true
		logConfigProcess("config apply| key=%s action=applied value=%s", name, str)
	}
	return nil
}

// CollectCommandFlagValues 收集供回写的 flag 值.
// 仅包含 Changed 的键, 以及 includeKeys 中出现的键 (通常为原配置文件已有键),
// 避免把未使用的默认值 (含默认口令) 全部灌进文件.
func CollectCommandFlagValues(cmd *cobra.Command, includeKeys map[string]bool) map[string]interface{} {
	out := make(map[string]interface{})
	seen := make(map[string]bool)
	add := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(f *pflag.Flag) {
			if f == nil || seen[f.Name] {
				return
			}
			seen[f.Name] = true
			switch f.Name {
			case "config", "c", "help", "version":
				return
			}
			if !f.Changed && (includeKeys == nil || !includeKeys[f.Name]) {
				return
			}
			v, ok := flagValueForYAML(f)
			if !ok {
				return
			}
			out[f.Name] = v
		})
	}
	add(cmd.LocalFlags())
	add(cmd.InheritedFlags())
	return out
}

// flagValueForYAML 将 pflag 值转为可写入 YAML 的类型; 空字符串跳过.
func flagValueForYAML(f *pflag.Flag) (interface{}, bool) {
	if f == nil || f.Value == nil {
		return nil, false
	}
	if sv, ok := f.Value.(interface{ GetSlice() []string }); ok {
		sl := sv.GetSlice()
		if len(sl) == 0 {
			return nil, false
		}
		arr := make([]interface{}, len(sl))
		for i, s := range sl {
			arr[i] = s
		}
		return arr, true
	}
	switch f.Value.Type() {
	case "bool":
		b, err := strconv.ParseBool(f.Value.String())
		if err != nil {
			return nil, false
		}
		return b, true
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count":
		n, err := strconv.Atoi(f.Value.String())
		if err != nil {
			return f.Value.String(), true
		}
		return n, true
	default:
		s := f.Value.String()
		if s == "" {
			return nil, false
		}
		return s, true
	}
}

// MergeConfigForWrite 以 original 为底, 用 flagVals 覆盖同名键; 保留原文件中他键.
func MergeConfigForWrite(original map[string]interface{}, flagVals map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(original)+len(flagVals))
	for k, v := range original {
		if k == "config" || k == "c" {
			continue
		}
		out[k] = v
	}
	for k, v := range flagVals {
		if k == "config" || k == "c" {
			continue
		}
		out[k] = v
	}
	return out
}

// WriteConfigFileAtomic 将 map 以 YAML 原子写到 path (先临时文件再 rename).
func WriteConfigFileAtomic(path string, m map[string]interface{}) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := path + ".yinstall.tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp config %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config to %s: %w", path, err)
	}
	return nil
}
