// r022_db_filesystem.go - 数据库文件系统布局采集
// 采集安装目录、数据目录的磁盘占用和文件列表，写入 db/filesystem/ 目录。
package collect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepR022DBFilesystemLayout 返回 R-022 步骤：采集数据库文件系统布局。
func StepR022DBFilesystemLayout() *runner.Step {
	return &runner.Step{
		ID:       "R-022",
		Name:     "Collect DB filesystem layout",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if getCollectEnvFile(ctx) == "" {
				return fmt.Errorf("env_file not available, skipping R-022")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)
			dir := filepath.Join(collectHostDir(ctx), "db", "filesystem")

			get := func(cmd string) string {
				r, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile, cmd, collectCmdTimeout(ctx))
				if r != nil {
					return strings.TrimSpace(r.GetStdout())
				}
				return ""
			}

			yasdbHome := get("echo $YASDB_HOME")
			yasdbData := get("echo $YASDB_DATA")

			// 目录磁盘占用
			if yasdbHome != "" {
				_ = runAndSaveAsUser(ctx, osUser, envFile,
					fmt.Sprintf("du -sh %s 2>/dev/null || true", yasdbHome),
					filepath.Join(dir, "du-home.txt"), collectCmdTimeout(ctx))
				_ = runAndSaveAsUser(ctx, osUser, envFile,
					fmt.Sprintf("ls -la %s/ 2>/dev/null || true", yasdbHome),
					filepath.Join(dir, "ls-home.txt"), collectCmdTimeout(ctx))
			}
			if yasdbData != "" {
				_ = runAndSaveAsUser(ctx, osUser, envFile,
					fmt.Sprintf("du -sh %s 2>/dev/null || true", yasdbData),
					filepath.Join(dir, "du-data.txt"), collectCmdTimeout(ctx))
				_ = runAndSaveAsUser(ctx, osUser, envFile,
					fmt.Sprintf("ls -la %s/ 2>/dev/null || true", yasdbData),
					filepath.Join(dir, "ls-data.txt"), collectCmdTimeout(ctx))
			}

			// df 文件系统类型
			_ = runAndSave(ctx, "df -hT 2>/dev/null || df -h 2>/dev/null || true", filepath.Join(dir, "df.txt"))

			ctx.Logger.Info("[R-022] DB filesystem layout collected to %s", dir)
			return nil
		},
	}
}
