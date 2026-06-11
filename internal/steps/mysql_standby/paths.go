package mysql_standby

import (
	"fmt"
	"path/filepath"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

func primaryLayout(ctx *runner.StepContext) commonmysql.Layout {
	base := ctx.GetParamString("mysql_base", commonmysql.DefaultBase(ctx.GetTargetPlatform()))
	return commonmysql.LayoutFromParams(ctx.GetTargetPlatform(), base, primaryPort(ctx), "")
}

func replicaLayout(ctx *runner.StepContext) (commonmysql.Layout, error) {
	base := ctx.GetParamString("mysql_base", commonmysql.DefaultBase(ctx.GetTargetPlatform()))
	ver := strings.TrimSpace(ctx.GetParamString("mysql_version", ""))
	if ver == "" {
		if v, ok := ctx.Results["replica_mysql_version"].(string); ok {
			ver = layoutVersionFromServerVersion(v)
		}
	}
	if ver == "" {
		return commonmysql.Layout{}, fmt.Errorf("replica mysql_version not resolved (run MR-007 or pass --mysql-version)")
	}
	return commonmysql.LayoutFromParams(ctx.GetTargetPlatform(), base, replicaPort(ctx), ver), nil
}

func layoutVersionFromServerVersion(v string) string {
	parts := strings.Split(strings.TrimSpace(v), "-")
	return strings.TrimSpace(parts[0])
}

func cnfPathForLayout(layout commonmysql.Layout, platform string) string {
	return layout.Other + "/" + commonmysql.ConfigFileName(platform)
}

func replicaPlatform(ctx *runner.StepContext) string {
	if p := strings.TrimSpace(ctx.GetParamString("replica_platform", "")); p != "" {
		return p
	}
	if v, ok := ctx.Results["replica_platform"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := ctx.Results["target_platform"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if p := strings.TrimSpace(ctx.GetParamString("target_platform", "")); p != "" {
		return p
	}
	return commonmysql.PlatformLinux
}

func replicaSoftDir(ctx *runner.StepContext) string {
	if d := strings.TrimSpace(ctx.GetParamString("replica_soft_dir", "")); d != "" {
		return d
	}
	if d := strings.TrimSpace(ctx.RemoteSoftwareDir); d != "" {
		return d
	}
	return commonmysql.DefaultRemoteSoftwareDir(replicaPlatform(ctx))
}

func resolveDumpFilePath(ctx *runner.StepContext, primaryPort int) string {
	if p := strings.TrimSpace(ctx.GetParamString("dump_file", "")); p != "" {
		return p
	}
	name := fmt.Sprintf("yinstall_mysql_dump_%d.sql", primaryPort)
	return joinRemotePath(replicaSoftDir(ctx), name, replicaPlatform(ctx))
}

func joinRemotePath(dir, name, platform string) string {
	dir = strings.TrimRight(strings.ReplaceAll(dir, `\`, `/`), "/")
	if platform == commonmysql.PlatformWindows {
		return dir + `/` + name
	}
	return filepath.Join(dir, name)
}

func ensureRemoteDir(ctx *runner.StepContext, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty remote directory")
	}
	if ctx.GetTargetPlatform() == commonmysql.PlatformWindows {
		winDir := strings.ReplaceAll(dir, `\`, `/`)
		return commonfile.RemoteEnsureDir(ctx, winDir, false)
	}
	useSudo := ctx.GetParamBool("sudo", false) && !ctx.GetParamBool("local_mode", false)
	cmd := fmt.Sprintf("mkdir -p %s", shellQuote(dir))
	_, err := ctx.ExecuteWithCheck(cmd, useSudo)
	return err
}
