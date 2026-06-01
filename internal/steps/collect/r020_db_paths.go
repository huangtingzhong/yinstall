// r020_db_paths.go - 数据库路径与版本信息采集
// source env 文件后读取 YASDB_HOME、YASDB_DATA 环境变量，获取安装路径和版本号，
// 写入 db/paths.json。
package collect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepR020DBPathsVersion 返回 R-020 步骤：采集数据库安装路径与版本。
func StepR020DBPathsVersion() *runner.Step {
	return &runner.Step{
		ID:       "R-020",
		Name:     "Collect DB paths and version",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if getCollectEnvFile(ctx) == "" {
				return fmt.Errorf("env_file not discovered (R-004 skipped or failed), skipping R-020")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)
			dir := filepath.Join(collectHostDir(ctx), "db")

			get := func(cmd string) string {
				r, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile, cmd, collectCmdTimeout(ctx))
				if r != nil {
					return strings.TrimSpace(r.GetStdout())
				}
				return ""
			}

			yasdbHome := get("echo $YASDB_HOME")
			yasdbData := get("echo $YASDB_DATA")
			version := get("yasdb -V 2>/dev/null || echo unknown")

			paths := map[string]string{
				"yasdb_home":    yasdbHome,
				"yasdb_data":    yasdbData,
				"yasdb_version": version,
				"env_file":      envFile,
				"os_user":       osUser,
			}

			if err := writeJSON(filepath.Join(dir, "paths.json"), paths); err != nil {
				appendWarning(ctx, "R-020", err.Error())
			}

			// 将路径写入 Results 供后续步骤使用
			ctx.Results["yasdb_home"] = yasdbHome
			ctx.Results["yasdb_data"] = yasdbData

			ctx.Logger.Info("[R-020] DB paths: home=%s data=%s version=%s", yasdbHome, yasdbData, version)
			return nil
		},
	}
}
