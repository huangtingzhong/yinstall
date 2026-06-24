package win_os

import "strings"

var linuxToWindowsTZ = map[string]string{
	"Asia/Shanghai":    "China Standard Time",
	"Asia/Chongqing":   "China Standard Time",
	"Asia/Harbin":      "China Standard Time",
	"Asia/Urumqi":      "China Standard Time",
	"UTC":              "UTC",
	"Etc/UTC":          "UTC",
	"Europe/London":    "GMT Standard Time",
	"America/New_York": "Eastern Standard Time",
}

// NormalizeWindowsTimezone maps Linux IANA ids to Windows timezone ids when known.
func NormalizeWindowsTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "China Standard Time"
	}
	if v, ok := linuxToWindowsTZ[tz]; ok {
		return v
	}
	return tz
}
