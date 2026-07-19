// om_update_hosts_toml.go - 更新 stage hosts.toml [om] 段
package om

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

func stepUpdateHostsTOML() *runner.Step {
	return &runner.Step{
		Name:        "OM Update Hosts TOML",
		Description: "Patch hosts.toml [om] hostid and LISTEN_ADDR",
		Tags:        []string{"om", "migrate"},

		PreCheck: func(ctx *runner.StepContext) error {
			stage := omStageDir(ctx)
			path := stage + "/hosts.toml"
			// 经产品用户访问 stage (非 root SSH 时走 sudo -n -u)
			res, _ := commonos.ExecuteAsUser(ctx, omProductUser(ctx), fmt.Sprintf("test -f %s", path), false)
			if res == nil || res.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx,
					fmt.Errorf("hosts.toml not found: %s", path))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Update Hosts TOML")
			stage := omStageDir(ctx)
			path := stage + "/hosts.toml"
			user := omProductUser(ctx)
			res, err := commonos.ExecuteAsUser(ctx, user, fmt.Sprintf("cat %s", path), false)
			if err != nil || res == nil || res.GetExitCode() != 0 {
				return fmt.Errorf("read hosts.toml failed: %v", err)
			}
			hostID, _ := ctx.Results["om_new_hostid"].(string)
			if hostID == "" {
				nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
				rows, _, sErr := YasomStatus(ctx)
				if sErr == nil {
					if r := FindRowByIP(rows, nw); r != nil {
						hostID = r.HostID
						ctx.Results["om_new_hostid"] = hostID
					}
				}
			}
			if hostID == "" {
				return fmt.Errorf("om_new_hostid unknown; cannot patch hosts.toml")
			}
			listen, _ := ctx.Results["om_new_listen"].(string)
			if listen == "" {
				nw := strings.TrimSpace(ctx.GetParamString("om_new", ""))
				var lErr error
				listen, lErr = YasomListenAddr(nw, omBeginPort(ctx))
				if lErr != nil {
					return lErr
				}
			}
			patched, err := PatchHostsTomlOM(res.GetStdout(), hostID, listen)
			if err != nil {
				return err
			}
			// 以产品用户写回, 保持文件属主
			b64hint := fmt.Sprintf("cat > %s <<'YINSTALL_OM_HOSTS_EOF'\n%s\nYINSTALL_OM_HOSTS_EOF", path, patched)
			ctx.LogScriptPreview("toml", "hosts.toml", patched)
			if _, werr := commonos.ExecuteAsUserWithCheck(ctx, user, b64hint, false); werr != nil {
				return fmt.Errorf("write hosts.toml failed: %w", werr)
			}
			omLogPhase(ctx, "hosts-toml-done", hostID+" "+listen)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stage := omStageDir(ctx)
			path := stage + "/hosts.toml"
			hostID, _ := ctx.Results["om_new_hostid"].(string)
			listen, _ := ctx.Results["om_new_listen"].(string)
			res, _ := commonos.ExecuteAsUser(ctx, omProductUser(ctx), fmt.Sprintf("cat %s", path), false)
			if res == nil {
				return fmt.Errorf("cannot re-read hosts.toml")
			}
			out := res.GetStdout()
			if !strings.Contains(out, hostID) || !strings.Contains(out, listen) {
				return fmt.Errorf("hosts.toml postcheck failed: missing %s or %s", hostID, listen)
			}
			return nil
		},
	}
}
