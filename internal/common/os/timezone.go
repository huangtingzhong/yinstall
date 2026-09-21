package os

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// DefaultOSTimezone Linux IANA 默认时区（--os-timezone 为空时使用）。
const DefaultOSTimezone = "Asia/Shanghai"

var (
	reYashanTimeZoneOffset    = regexp.MustCompile(`^([+-])(0[0-9]|1[0-5]):([0-5][0-9])$`)
	reTimedatectlTimeZoneLine = regexp.MustCompile(`(?i)time\s*zone:\s*(\S+)`)
)

// ResolveOSTimezone 解析 OS 时区 CLI；空则 DefaultOSTimezone。
func ResolveOSTimezone(raw string) string {
	tz := strings.TrimSpace(raw)
	if tz == "" {
		return DefaultOSTimezone
	}
	return tz
}

// IsYashanTimeZoneOffset 判断是否为 YashanDB TIME_ZONE 偏移格式 [-15:59, +15:59]。
func IsYashanTimeZoneOffset(s string) bool {
	return reYashanTimeZoneOffset.MatchString(strings.TrimSpace(s))
}

// ParseDBTimeZoneInput 解析 --db-timezone：偏移量原样校验；否则按 IANA 转为偏移。
func ParseDBTimeZoneInput(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("database timezone is empty")
	}
	if IsYashanTimeZoneOffset(raw) {
		return raw, nil
	}
	return IANAToYashanTimeZone(raw)
}

// IANAToYashanTimeZone 将 IANA 时区转为 YashanDB TIME_ZONE 偏移（如 +08:00）。
func IANAToYashanTimeZone(iana string) (string, error) {
	iana = strings.TrimSpace(iana)
	if iana == "" {
		return "", fmt.Errorf("IANA timezone is empty")
	}
	loc, err := time.LoadLocation(iana)
	if err != nil {
		return "", fmt.Errorf("invalid IANA timezone %q: %w", iana, err)
	}
	_, offsetSec := time.Now().In(loc).Zone()
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	mins := (offsetSec % 3600) / 60
	if hours > 15 || (hours == 15 && mins > 59) {
		return "", fmt.Errorf("timezone offset +15:59 exceeded for %q; specify --db-timezone explicitly", iana)
	}
	return fmt.Sprintf("%s%02d:%02d", sign, hours, mins), nil
}

// ParseTimedatectlTimezoneOutput 从 timedatectl status 行解析 IANA 时区（EL7/EL8 通用 "Time zone: ..."）。
func ParseTimedatectlTimezoneOutput(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("timedatectl timezone output is empty")
	}
	if m := reTimedatectlTimeZoneLine.FindStringSubmatch(raw); len(m) == 2 {
		tz := strings.TrimSuffix(strings.TrimSpace(m[1]), "(")
		tz = strings.TrimSpace(tz)
		if tz == "" {
			return "", fmt.Errorf("timedatectl timezone name is empty")
		}
		return tz, nil
	}
	// 兼容仅输出裸 IANA 的情况
	if !strings.Contains(raw, " ") && !strings.Contains(raw, ":") {
		return raw, nil
	}
	return "", fmt.Errorf("cannot parse timedatectl timezone from %q", raw)
}

// ReadHostIANATimezone 读取目标机 IANA 时区（统一用 timedatectl status 行；兼容 EL7 systemd 219，无版本分支）。
// 失败时由调用方要求用户设置 --db-timezone。
func ReadHostIANATimezone(ctx *runner.StepContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("step context is nil")
	}
	// EL7/EL8 均输出 "Time zone: <IANA> (...)"；不使用需 systemd>=239 的 show --value。
	result, err := ctx.Execute("timedatectl 2>/dev/null | grep -F 'Time zone'", false)
	if err != nil {
		return "", fmt.Errorf("read host timezone: %w", err)
	}
	if result == nil || result.GetExitCode() != 0 {
		return "", fmt.Errorf("timedatectl timezone query failed")
	}
	return ParseTimedatectlTimezoneOutput(result.GetStdout())
}
