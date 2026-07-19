// om_ensure_secondary.go - 在指定节点确保已同步的 secondary yasom (迁主/P2 共用)
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// EnsureSecondaryYasom 在 ctx.Executor 所在机拉起/确认 secondary，并与 primary max_seq 对齐。
// ip 为本机对外 IP (写入 listen 与 status 匹配)。
func EnsureSecondaryYasom(ctx *runner.StepContext, ip string) error {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return fmt.Errorf("secondary OM host IP is required")
	}
	listen, err := YasomListenAddr(ip, omBeginPort(ctx))
	if err != nil {
		return err
	}
	ctx.Results["om_secondary_listen"] = listen

	rows, _, err := YasomStatus(ctx)
	if err == nil && SecondarySynced(rows, ip, listen) == nil {
		ctx.Logger.Info("Host %s already synced secondary yasom; skip recover", ip)
		ctx.Results["om_secondary_synced"] = true
		return nil
	}

	if r := FindRowByIP(rows, ip); r != nil {
		if strings.EqualFold(r.Role, "primary") {
			return fmt.Errorf("host %s is primary yasom; cannot recover as secondary", ip)
		}
		if !IsPIDRunning(r.PID) || strings.EqualFold(r.Role, "secondary") {
			_ = CleanYasom(ctx, true)
		}
	}

	if err := RecoverYasom(ctx, "secondary", listen, true); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			_ = CleanYasom(ctx, true)
			if err2 := RecoverYasom(ctx, "secondary", listen, true); err2 != nil {
				return err2
			}
		} else {
			return err
		}
	}
	if err := WaitSecondarySynced(ctx, ip, listen, DefaultSyncWaitTimeout, DefaultSyncWaitInterval); err != nil {
		return err
	}
	ctx.Results["om_secondary_synced"] = true
	return nil
}
