// primary_stage.go - 主库 yasboot stage 目录默认值与补全

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// dbDefaultBeginPort 与 internal/cli/db 默认 --db-port（1688）一致；非该端口时路径规则与 buildDBParams 中加 _<port> 后缀的策略对齐。
const dbDefaultBeginPort = 1688

// DefaultPrimaryStageDir CLI 占位；主库步骤内应调用 EnsurePrimaryStageDirParam 经 getent 解析。
func DefaultPrimaryStageDir(primaryOSUser string, port int) string {
	return commonos.ConventionStageDir(primaryOSUser, port)
}

// EnsurePrimaryStageDirParam 若 db_stage_dir 为空或为 /home/<user> 占位，经 getent 写回真实路径。
func EnsurePrimaryStageDirParam(ctx *runner.StepContext) error {
	if ctx == nil {
		return nil
	}
	port := ctx.GetParamInt("db_begin_port", dbDefaultBeginPort)
	return commonos.ResolvePrimaryStageDirParam(ctx, GetPrimaryOSUser(ctx), port)
}

// DefaultExpansionInstallPath 未指定 --db-home-path 时与 yinstall db 默认 db-home-path / buildDBParams 一致：
// dbDefaultBeginPort 为 /data/<user>/yasdb_home；否则 /data/<user>/yasdb_home_<port>。
func DefaultExpansionInstallPath(osUser string, port int) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/data/%s/yasdb_home", u)
	}
	return fmt.Sprintf("/data/%s/yasdb_home_%d", u, port)
}

// DefaultExpansionDataPath 未指定 --db-data-path 时与 yinstall db 默认一致：
// dbDefaultBeginPort 为 /data/<user>/yasdb_data；否则 /data/<user>/yasdb_data_<port>。
func DefaultExpansionDataPath(osUser string, port int) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/data/%s/yasdb_data", u)
	}
	return fmt.Sprintf("/data/%s/yasdb_data_%d", u, port)
}

// DefaultExpansionLogPath 未指定 --db-log-path 时与 yinstall db 默认一致：
// dbDefaultBeginPort 为 /data/<user>/log；否则 /data/<user>/log_<port>。
func DefaultExpansionLogPath(osUser string, port int) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/data/%s/log", u)
	}
	return fmt.Sprintf("/data/%s/log_%d", u, port)
}

// EnsureExpansionPathParams 若 install/data/log 路径为空，则按 primary_os_user 与 db_begin_port 写入默认路径（供单独 -s 某步等兜底）。
func EnsureExpansionPathParams(ctx *runner.StepContext) {
	if ctx == nil || ctx.Params == nil {
		return
	}
	u := GetPrimaryOSUser(ctx)
	port := ctx.GetParamInt("db_begin_port", dbDefaultBeginPort)
	if strings.TrimSpace(ctx.GetParamString("db_install_path", "")) == "" {
		ctx.Params["db_install_path"] = DefaultExpansionInstallPath(u, port)
	}
	if strings.TrimSpace(ctx.GetParamString("db_data_path", "")) == "" {
		ctx.Params["db_data_path"] = DefaultExpansionDataPath(u, port)
	}
	if strings.TrimSpace(ctx.GetParamString("db_log_path", "")) == "" {
		ctx.Params["db_log_path"] = DefaultExpansionLogPath(u, port)
	}
}
