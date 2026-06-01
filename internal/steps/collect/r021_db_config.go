// r021_db_config.go - 数据库配置文件采集
// 复制 yashandb.toml、集群配置文件及 yasboot confdir 到 db/config/ 目录（控制端归档）。
package collect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepR021DBConfigFiles 返回 R-021 步骤：采集数据库配置文件。
func StepR021DBConfigFiles() *runner.Step {
	return &runner.Step{
		ID:       "R-021",
		Name:     "Collect DB config files",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if getCollectEnvFile(ctx) == "" {
				return fmt.Errorf("env_file not available, skipping R-021")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)
			clusterName := getCollectClusterName(ctx)
			dir := filepath.Join(collectHostDir(ctx), "db", "config")

			get := func(cmd string) string {
				r, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile, cmd, collectCmdTimeout(ctx))
				if r != nil {
					return strings.TrimSpace(r.GetStdout())
				}
				return ""
			}

			// yasdb.toml / yashandb.toml 路径
			yasdbHome := get("echo $YASDB_HOME")
			if yasdbHome != "" {
				tomlPath := yasdbHome + "/conf/yashandb.toml"
				_ = runAndSaveAsUser(ctx, osUser, envFile,
					fmt.Sprintf("cat %s 2>/dev/null || true", tomlPath),
					filepath.Join(dir, "yashandb.toml"), collectCmdTimeout(ctx))
			}

			// yasboot 集群配置
			if clusterName != "" {
				homeDir := get("echo $HOME")
				yasbootConf := fmt.Sprintf("%s/.yasboot/%s_yasdb_home/conf/%s.toml", homeDir, clusterName, clusterName)
				_ = runAndSaveAsUser(ctx, osUser, envFile,
					fmt.Sprintf("cat %s 2>/dev/null || true", yasbootConf),
					filepath.Join(dir, clusterName+".toml"), collectCmdTimeout(ctx))
			}

			// 列出 yasboot confdir
			_ = runAndSaveAsUser(ctx, osUser, envFile,
				"ls -la ~/.yasboot/ 2>/dev/null || true",
				filepath.Join(dir, "yasboot-dir.txt"), collectCmdTimeout(ctx))

			_ = runAndSaveAsUser(ctx, osUser, envFile,
				"yasboot cluster list 2>/dev/null || true",
				filepath.Join(dir, "cluster-list.txt"), collectCmdTimeout(ctx))

			ctx.Logger.Info("[R-021] DB config files collected to %s", dir)
			return nil
		},
	}
}
