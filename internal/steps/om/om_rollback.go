// om_rollback.go - 迁主失败尽力回滚 (不 demote；已升主则不强制降级)
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// MigrateRollbackHint 根据已成功的最后一步给出回滚策略说明（英文）。
func MigrateRollbackHint(lastOKStep string) string {
	switch lastOKStep {
	case "", "OM Migrate Gate", "OM Host Prepare", "OM Host Add":
		return "No primary OM stop yet; check yasom status. Optional: clean secondary on --om-new if recover left residue."
	case "OM Recover Secondary":
		return "Secondary on --om-new may exist; attempting clean on new host. Primary on --om-current should still be up."
	case "OM Stop Primary":
		return "Primary was stopped; attempting start on --om-current (metadata still primary)."
	case "OM Recover Primary", "OM Sync", "OM Update Hosts TOML", "OM Recover Old Secondary":
		return "New host may already be primary; will not demote. Attempt sync and print status; fix hosts.toml manually if needed."
	default:
		return "Check yasboot process yasom status on both hosts; do not use demote."
	}
}

// AttemptMigrateRollback 尽力回滚。curCtx 连 CUR，newCtx 连 NEW；共享 Results 可选。
// 返回 rollback 过程中的错误（可多段拼接）；调用方应在迁主失败后仍返回原错误为主。
func AttemptMigrateRollback(curCtx, newCtx *runner.StepContext, curIP, newIP, lastOKStep string) error {
	curIP = strings.TrimSpace(curIP)
	newIP = strings.TrimSpace(newIP)
	var errs []string
	hint := MigrateRollbackHint(lastOKStep)
	if curCtx != nil && curCtx.Logger != nil {
		curCtx.Logger.Warn("OM migrate rollback: lastOK=%s; %s", lastOKStep, hint)
	}

	switch lastOKStep {
	case "OM Recover Secondary":
		if newCtx != nil {
			if err := CleanYasom(newCtx, true); err != nil {
				errs = append(errs, "clean new secondary: "+err.Error())
			}
		}
	case "OM Stop Primary":
		// stop 后角色仍是 primary; recover 会报 already exist 且不拉起进程, 必须 start。
		if curCtx != nil {
			if err := StartYasom(curCtx); err != nil {
				errs = append(errs, "start primary on current: "+err.Error())
			} else if err := SyncYasom(curCtx, true); err != nil {
				errs = append(errs, "sync after start current: "+err.Error())
			}
		}
	case "OM Recover Primary", "OM Sync", "OM Update Hosts TOML", "OM Recover Old Secondary":
		if newCtx != nil {
			if err := SyncYasom(newCtx, true); err != nil {
				errs = append(errs, "sync on new primary: "+err.Error())
			}
			if rows, out, err := YasomStatus(newCtx); err != nil {
				errs = append(errs, "status after failed migrate: "+err.Error())
				if newCtx.Logger != nil {
					newCtx.Logger.Warn("yasom status output:\n%s", out)
				}
			} else if newCtx.Logger != nil {
				pri := FindPrimaryRow(rows)
				who := ""
				if pri != nil {
					who = pri.IPAddr
				}
				newCtx.Logger.Warn("After failed migrate, primary yasom appears to be: %s (expected maybe %s)", who, newIP)
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("rollback issues: %s", strings.Join(errs, "; "))
}
