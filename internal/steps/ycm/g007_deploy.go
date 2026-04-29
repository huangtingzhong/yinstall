// g007_deploy.go - 执行 YCM 初始化部署
// G-007: 运行 ycm-init deploy 命令，支持 sqlite3 和 yashandb 两种模式

package ycm

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepG007Deploy 执行 YCM 初始化部署
func StepG007Deploy() *runner.Step {
	return &runner.Step{
		ID:          "G-007",
		Name:        "Deploy YCM",
		Description: "Execute YCM initialization deployment (sqlite3 or yashandb backend)",
		Tags:        []string{"ycm", "install"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			driver := ctx.GetParamString("ycm_db_driver", "sqlite3")

			// 检查 ycm-init 可执行文件，支持多种可能的路径
			// - /opt/ycm-init（部分版本）
			// - /opt/ycm/ycm-init（常见版本）
			// - /opt/yashan-migrate-platform/ycm-init（旧版本）
			var ycmInit string
			possiblePaths := []string{
				fmt.Sprintf("%s/ycm/ycm-init", installDir),
				fmt.Sprintf("%s/ycm-init", installDir),
				fmt.Sprintf("%s/yashan-migrate-platform/ycm-init", installDir),
			}

			for _, path := range possiblePaths {
				result, _ := ctx.Execute(fmt.Sprintf("test -f %s", path), false)
				if result != nil && result.GetExitCode() == 0 {
					ycmInit = path
					ctx.Logger.Info("Found ycm-init at: %s", path)
					break
				}
			}

			if ycmInit == "" {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("ycm-init not found in any of: %v", possiblePaths))
			}

			// 将找到的路径保存到参数中，供 Action 使用
			ctx.Params["ycm_init_path"] = ycmInit

			// 检查 deploy.yml
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", deployFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("deploy config not found: %s", deployFile))
			}

			// 如果使用 yashandb 模式，检查必要参数
			if driver == "yashandb" {
				dbURL := ctx.GetParamString("ycm_db_url", "")
				if dbURL == "" {
					return fmt.Errorf("ycm_db_url is required when ycm_db_driver=yashandb")
				}
				dbPassword := ctx.GetParamString("ycm_db_admin_password", "")
				if dbPassword == "" {
					return fmt.Errorf("ycm_db_admin_password is required when ycm_db_driver=yashandb")
				}
			}

			ctx.Logger.Info("YCM deploy pre-check passed (driver: %s)", driver)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			driver := ctx.GetParamString("ycm_db_driver", "sqlite3")
			ycmInit := ctx.GetParamString("ycm_init_path", "")

			if ycmInit == "" {
				return fmt.Errorf("ycm_init_path not set in parameters")
			}

			ctx.Logger.Info("Deploying YCM with driver: %s", driver)
			ctx.Logger.Info("Deploy config: %s", deployFile)

			// 如果使用 yashandb，先修改 deploy.yml 中的 dbconfig
			if driver == "yashandb" {
				dbURL := ctx.GetParamString("ycm_db_url", "")
				dbLibPath := ctx.GetParamString("ycm_db_lib_path", "")

				ctx.Logger.Info("Configuring YashanDB backend in deploy.yml")

				// 设置 driver
				cmd := fmt.Sprintf("sed -i 's/driver:.*/driver: \"yashandb\"/' %s", deployFile)
				ctx.Execute(cmd, true)

				// 设置 url
				// 需要对 url 中的特殊字符进行转义
				escapedURL := strings.ReplaceAll(dbURL, "/", "\\/")
				cmd = fmt.Sprintf("sed -i 's/url:.*/url: \"%s\"/' %s", escapedURL, deployFile)
				ctx.Execute(cmd, true)

				// 设置 libPath（如果提供）
				if dbLibPath != "" {
					escapedLibPath := strings.ReplaceAll(dbLibPath, "/", "\\/")
					cmd = fmt.Sprintf("sed -i 's/libPath:.*/libPath: \"%s\"/' %s", escapedLibPath, deployFile)
					ctx.Execute(cmd, true)
				}

				ctx.Logger.Info("YashanDB backend configured in deploy.yml")
			}

			// 构建部署命令
			var cmd string
			if driver == "yashandb" {
				dbAdminUser := ctx.GetParamString("ycm_db_admin_user", "yasman")
				dbAdminPassword := ctx.GetParamString("ycm_db_admin_password", "")
				cmd = fmt.Sprintf("%s deploy --conf %s --username %s --password %s",
					ycmInit, deployFile, dbAdminUser, commonos.ShellSingleQuote(dbAdminPassword))
			} else {
				// sqlite3 模式
				cmd = fmt.Sprintf("%s deploy --conf %s", ycmInit, deployFile)
			}

			ctx.Logger.Info("Executing YCM deploy command...")
			result, err := ctx.Execute(cmd, true)
			if err != nil {
				return fmt.Errorf("YCM deploy failed: %w", err)
			}
			if result != nil {
				if result.GetExitCode() != 0 {
					ctx.Logger.Error("YCM deploy stderr: %s", result.GetStderr())
					return fmt.Errorf("YCM deploy failed with exit code %d", result.GetExitCode())
				}
				ctx.Logger.Info("YCM deploy output: %s", result.GetStdout())
			}

			ctx.Logger.Info("YCM deployment completed successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// 等待进程启动
			ctx.Logger.Info("Waiting for YCM processes to start...")
			result, _ := ctx.Execute("sleep 3", false)
			_ = result

			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			ycmDir := fmt.Sprintf("%s/ycm", installDir)

			// 在路径后添加 / 以避免误匹配（如 /opt/ycm 不会匹配到 /opt/ycm2）
			ycmDirPattern := ycmDir
			if !strings.HasSuffix(ycmDirPattern, "/") {
				ycmDirPattern = ycmDirPattern + "/"
			}
			// 简单检查进程是否存在
			result, _ = ctx.Execute(fmt.Sprintf("ps -ef | grep '%s' | grep -v grep | head -3", ycmDirPattern), false)
			if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
				ctx.Logger.Info("OK: YCM processes detected after deployment")
			} else {
				ctx.Logger.Warn("No YCM processes detected yet (may still be starting)")
			}
			return nil
		},
	}
}
