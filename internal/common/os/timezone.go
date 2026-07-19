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

var reYashanTimeZoneOffset = regexp.MustCompile(`^([+-])(0[0-9]|1[0-5]):([0-5][0-9])$`)

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

// ReadHostIANATimezone 读取目标机 timedatectl 报告的 IANA 时区（无 fallback；失败时由调用方要求用户设置 --db-timezone）。
func ReadHostIANATimezone(ctx *runner.StepContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("step context is nil")
	}
	result, err := ctx.Execute("timedatectl show --property=Timezone --value 2>/dev/null", false)
	if err != nil {
		return "", fmt.Errorf("read host timezone: %w", err)
	}
	if result == nil || result.GetExitCode() != 0 {
		return "", fmt.Errorf("timedatectl timezone query failed")
	}
	tz := strings.TrimSpace(result.GetStdout())
	if tz == "" {
		return "", fmt.Errorf("timedatectl returned empty timezone")
	}
	return tz, nil
}
