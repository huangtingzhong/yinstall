// primary_stage.go - 主库 yasboot stage 目录默认值与补全

package standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// DefaultPrimaryStageDir 未指定 --db-stage-dir 时使用：/home/<primaryOSUser>/install_<port>（port 为主库监听端口，与 db_begin_port 一致）。
func DefaultPrimaryStageDir(primaryOSUser string, port int) string {
	u := strings.TrimSpace(primaryOSUser)
	if u == "" {
		u = "yashan"
	}
	return fmt.Sprintf("/home/%s/install_%d", u, port)
}

// EnsurePrimaryStageDirParam 若 ctx.Params["db_stage_dir"] 为空，则按主库 OS 用户与 db_begin_port 写入默认路径。
func EnsurePrimaryStageDirParam(ctx *runner.StepContext) {
	if ctx == nil || ctx.Params == nil {
		return
	}
	if strings.TrimSpace(ctx.GetParamString("db_stage_dir", "")) != "" {
		return
	}
	port := ctx.GetParamInt("db_begin_port", 1688)
	ctx.Params["db_stage_dir"] = DefaultPrimaryStageDir(GetPrimaryOSUser(ctx), port)
}

// DefaultExpansionInstallPath 未指定 --db-home-path 时：/data/<user>/yasdb_home_<port>（与多实例主库目录惯例一致）。
func DefaultExpansionInstallPath(osUser string, port int) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	return fmt.Sprintf("/data/%s/yasdb_home_%d", u, port)
}

// DefaultExpansionDataPath 未指定 --db-data-path 时：/data/<user>/yasdb_data_<port>。
func DefaultExpansionDataPath(osUser string, port int) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	return fmt.Sprintf("/data/%s/yasdb_data_%d", u, port)
}

// DefaultExpansionLogPath 未指定 --db-log-path 时：/data/<user>/log_<port>。
func DefaultExpansionLogPath(osUser string, port int) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		u = "yashan"
	}
	return fmt.Sprintf("/data/%s/log_%d", u, port)
}

// EnsureExpansionPathParams 若 install/data/log 路径为空，则按 primary_os_user 与 db_begin_port 写入默认路径（供单独 -s 某步等兜底）。
func EnsureExpansionPathParams(ctx *runner.StepContext) {
	if ctx == nil || ctx.Params == nil {
		return
	}
	u := GetPrimaryOSUser(ctx)
	port := ctx.GetParamInt("db_begin_port", 1688)
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
