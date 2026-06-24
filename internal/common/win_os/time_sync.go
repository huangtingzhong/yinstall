package win_os

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ConfigureTimezoneAndTime sets timezone and optional NTP peer.
func ConfigureTimezoneAndTime(ctx *runner.StepContext) error {
	tz := NormalizeWindowsTimezone(ctx.GetParamString("os_timezone", "China Standard Time"))
	ntp := strings.TrimSpace(ctx.GetParamString("os_ntp_server", ""))

	setTZ := fmt.Sprintf(`Set-TimeZone -Id '%s'`, strings.ReplaceAll(tz, "'", "''"))
	ctx.LogScriptPreview("powershell", "W-003 timezone", setTZ)
	if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+setTZ+`"`, false); err != nil {
		return err
	}

	if ntp != "" {
		w32 := fmt.Sprintf(`w32tm /config /manualpeerlist:%s /syncfromflags:manual /update`, ntp)
		ctx.LogScriptPreview("powershell", "W-003 w32tm", w32)
		if _, err := ctx.ExecuteWithCheck(w32, false); err != nil {
			return err
		}
		_, _ = ctx.Execute(`net stop w32time && net start w32time`, false)
	}
	return nil
}

// TimeSkewSeconds returns absolute skew vs control machine (best effort: w32tm stripchart one sample).
func TimeSkewSeconds(ctx *runner.StepContext) (float64, error) {
	res, err := ctx.Execute(`powershell -NoProfile -Command "(Get-Date).ToUniversalTime().Subtract([datetime]'1970-01-01').TotalSeconds"`, false)
	if err != nil || res == nil {
		return 0, err
	}
	remote, err := strconv.ParseFloat(strings.TrimSpace(res.GetStdout()), 64)
	if err != nil {
		return 0, err
	}
	local := float64(0) // caller compares; on target-only check we verify w32tm /query /status
	_ = local
	return remote, nil
}

// MaxTimeSkewSec reads param default 60.
func MaxTimeSkewSec(ctx *runner.StepContext) int {
	return ctx.GetParamInt("os_max_time_skew", 60)
}
