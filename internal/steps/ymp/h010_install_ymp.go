// h010_install_ymp.go - 执行 YMP 安装初始化
// H-010: 执行 ymp.sh install --db <db_pkg> --path <instantclient_dir>

package ymp

import (
	"fmt"
	"path"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepH010InstallYMP 执行 YMP 安装
func StepH010InstallYMP() *runner.Step {
	return &runner.Step{
		ID:          "H-010",
		Name:        "Run YMP Install",
		Description: "Execute ymp.sh install to initialize YMP with embedded database",
		Tags:        []string{"ymp", "install"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympSh := path.Join(installDir, "yashan-migrate-platform", "bin", "ymp.sh")

			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", ympSh), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("ymp.sh not found at %s, extract YMP first (H-006)", ympSh)
			}

			dbPackage := ctx.GetParamString("ymp_db_package", "")
			if dbPackage == "" {
				ctx.Logger.Info("ymp_db_package not specified, searching for latest YashanDB package...")
				remoteDir := ctx.RemoteSoftwareDir
				if remoteDir == "" {
					remoteDir = "/data/yashan/soft"
				}
				latestPkg, err := commonfile.FindLatestDBPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return fmt.Errorf("ymp_db_package not specified and auto-search failed: %w", err)
				}
				ctx.Logger.Info("Found latest YashanDB package for YMP: %s", latestPkg)
				ctx.Params["ymp_db_package"] = latestPkg
			}

			// 判断当前安装目录是否已完成安装（以 db 子目录存在为标志）
			// 注意：ymp.env 是用户级文件，多实例（不同端口）共用同一用户时不能仅靠它判断
			dbDir := path.Join(installDir, "yashan-migrate-platform", "db")
			result, _ = ctx.Execute(fmt.Sprintf("test -d %s", dbDir), false)
			if result != nil && result.GetExitCode() == 0 {
				if !ctx.IsForceStep() {
					ctx.Logger.Warn("YMP already installed in %s (db directory exists)", installDir)
					ctx.Logger.Warn("To reinstall, use: --force H-010")
					return fmt.Errorf("skip: YMP already installed in %s, use --force H-010 to reinstall", installDir)
				}
				// Force 模式：清理旧安装留下的 env 文件
				ympUser := ctx.GetParamString("ymp_user", "ymp")
				ympEnvFile := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
				result, _ = ctx.Execute(fmt.Sprintf("test -f %s", ympEnvFile), false)
				if result != nil && result.GetExitCode() == 0 {
					ctx.Logger.Info("Force reinstall detected, removing existing ymp.env: %s", ympEnvFile)
					if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("rm -f %s", ympEnvFile), true); err != nil {
						return fmt.Errorf("failed to remove existing ymp.env: %w", err)
					}
					ctx.Logger.Info("✓ Existing ymp.env removed")
				}
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")
			dbPackage := ctx.GetParamString("ymp_db_package", "")

			// 校验数据库模式
			dbMode := ctx.GetParamString("ymp_db_mode", "yashandb")
			if dbMode != "yashandb" && dbMode != "mysql" {
				return fmt.Errorf("invalid ymp_db_mode: '%s' (case-sensitive). Valid values are: 'yashandb' or 'mysql'", dbMode)
			}

			// 查找 DB 安装包
			ctx.Logger.Info("Looking for DB package: %s", dbPackage)
			dbPath, err := commonfile.FindAndDistribute(
				ctx,
				dbPackage,
				ctx.LocalSoftwareDirs,
				ctx.RemoteSoftwareDir,
			)
			if err != nil {
				return fmt.Errorf("DB package not found: %w", err)
			}

			// 设置包属主和权限，确保 ymp 用户可以读取
			// 如果文件在 /root 目录下，需要先复制到可访问的位置
			if strings.HasPrefix(dbPath, "/root/") {
				// 将文件复制到 /tmp 目录，ymp 用户可以访问
				tmpPath := fmt.Sprintf("/tmp/%s", path.Base(dbPath))
				ctx.Logger.Info("DB package is in /root, copying to %s for ymp user access", tmpPath)
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("cp %s %s", dbPath, tmpPath), true); err != nil {
					return fmt.Errorf("failed to copy DB package to /tmp: %w", err)
				}
				dbPath = tmpPath
			}
			// 设置包属主和权限（需要 root 权限，所以直接执行）
			if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("chown %s:%s %s && chmod 644 %s", ympUser, ympUser, dbPath, dbPath), true); err != nil {
				return fmt.Errorf("failed to set ownership and permissions on DB package: %w", err)
			}

			// 获取 instantclient 目录
			result, _ := ctx.Execute(fmt.Sprintf("ls -d %s/instantclient_* 2>/dev/null | head -1", installDir), false)
			icDir := ""
			if result != nil {
				icDir = strings.TrimSpace(result.GetStdout())
			}
			if icDir == "" {
				return fmt.Errorf("instantclient directory not found in %s, extract it first (H-007)", installDir)
			}

			// 配置 YMP 端口（如果指定了自定义端口，需要在安装前配置）
			// 只需要指定 YMP Web 服务端口，其他端口按照规则自动计算
			port := ctx.GetParamInt("ymp_port", 8090)
			dbPort := port + 1       // 数据库端口 = YMP端口 + 1
			yasomPort := port + 3    // yasom端口 = YMP端口 + 3
			yasagentPort := port + 4 // yasagent端口 = YMP端口 + 4

			appPropsFile := path.Join(installDir, "yashan-migrate-platform", "conf", "application.properties")
			dbPropsFile := path.Join(installDir, "yashan-migrate-platform", "conf", "db.properties")
			// profile.toml 可能在 db 目录下，安装后会在 db/conf/profile.toml
			profileTomlFile := path.Join(installDir, "yashan-migrate-platform", "db", "conf", "profile.toml")

			// ── 安装前配置：端口 + DB 模式（必须在 ymp.sh install 之前完成）──
			if port != 8090 {
				ctx.Logger.Info("Configuring ports before installation: Web=%d, DB=%d, yasom=%d, yasagent=%d", port, dbPort, yasomPort, yasagentPort)

				// 修改 application.properties 中的 server.port
				result, _ = ctx.Execute(fmt.Sprintf("test -f %s", appPropsFile), false)
				if result != nil && result.GetExitCode() == 0 {
					sedCmd := fmt.Sprintf(
						"grep -q '^server\\.port=' %s && sed -i 's/^server\\.port=.*/server.port=%d/' %s || echo 'server.port=%d' >> %s",
						appPropsFile, port, appPropsFile, port, appPropsFile,
					)
					if _, err := ctx.ExecuteWithCheck(sedCmd, true); err != nil {
						ctx.Logger.Warn("Failed to set server.port in application.properties: %v", err)
					} else {
						ctx.Logger.Info("✓ server.port=%d in %s", port, appPropsFile)
					}
				}

				// 修改 db.properties 中的 YASDB_PORT 和 YASDB_YASOM_PORT / YASDB_YASAGENT_PORT
				result, _ = ctx.Execute(fmt.Sprintf("test -f %s", dbPropsFile), false)
				if result != nil && result.GetExitCode() == 0 {
					for _, kv := range [][2]string{
						{"YASDB_PORT", fmt.Sprintf("%d", dbPort)},
						{"YASDB_YASOM_PORT", fmt.Sprintf("%d", yasomPort)},
						{"YASDB_YASAGENT_PORT", fmt.Sprintf("%d", yasagentPort)},
					} {
						sedCmd := fmt.Sprintf(
							"grep -q '^%s=' %s && sed -i 's/^%s=.*/%s=%s/' %s || echo '%s=%s' >> %s",
							kv[0], dbPropsFile, kv[0], kv[0], kv[1], dbPropsFile, kv[0], kv[1], dbPropsFile,
						)
						ctx.Execute(sedCmd, true)
					}
					ctx.Logger.Info("✓ DB ports configured in %s (YASDB_PORT=%d)", dbPropsFile, dbPort)
				}
			}

			// 若 db 模式为 mysql，在安装前修改 conf/db.properties 中的 YASDB_MODE
			if dbMode == "mysql" {
				ctx.Logger.Info("DB mode is mysql, modifying YASDB_MODE in %s", dbPropsFile)
				result, _ := ctx.Execute(fmt.Sprintf("test -f %s", dbPropsFile), false)
				if result != nil && result.GetExitCode() == 0 {
					setModeCmd := fmt.Sprintf(
						"grep -q '^YASDB_MODE=' %s && sed -i 's/^YASDB_MODE=.*/YASDB_MODE=mysql/' %s || echo 'YASDB_MODE=mysql' >> %s",
						dbPropsFile, dbPropsFile, dbPropsFile,
					)
					if _, err := ctx.ExecuteWithCheck(setModeCmd, true); err != nil {
						return fmt.Errorf("failed to set YASDB_MODE=mysql in db.properties: %w", err)
					}
					ctx.Logger.Info("✓ YASDB_MODE set to mysql in %s", dbPropsFile)
				} else {
					return fmt.Errorf("db.properties not found at %s, cannot set YASDB_MODE", dbPropsFile)
				}
			} else {
				ctx.Logger.Info("DB mode: yashandb (default)")
			}

			// ── 执行安装 ──
			ympSh := path.Join(installDir, "yashan-migrate-platform", "bin", "ymp.sh")
			cmd := fmt.Sprintf("sh %s install --db %s --path %s", ympSh, dbPath, icDir)

			ctx.Logger.Info("Running: ymp.sh install --db %s --path %s", dbPath, icDir)

			result, err = commonos.ExecuteAsUser(ctx, ympUser, cmd, false)
			if err != nil {
				return fmt.Errorf("ymp.sh install failed: %w", err)
			}

			// 检查执行结果
			if result.GetExitCode() != 0 {
				errMsg := result.GetStderr()
				if errMsg == "" {
					errMsg = result.GetStdout()
				}
				if errMsg == "" {
					errMsg = fmt.Sprintf("exit code %d", result.GetExitCode())
				}
				// 输出详细的错误信息到终端
				if ctx.Logger != nil {
					ctx.Logger.LogErrorExit(
						ctx.Executor.Host(),
						ctx.CurrentStepID,
						"",
						fmt.Sprintf("su - %s -c 'sh %s install --db %s --path %s'", ympUser, ympSh, dbPath, icDir),
						result.GetStdout(),
						result.GetStderr(),
						result.GetExitCode(),
						errMsg,
					)
				}
				return fmt.Errorf("ymp.sh install failed with exit code %d: %s", result.GetExitCode(), strings.TrimSpace(errMsg))
			}

			// 安装完成后，立即配置端口（在服务完全启动之前）
			// 如果指定了自定义端口，需要修改配置文件并重启服务
			if port != 8090 {
				ctx.Logger.Info("Configuring YMP ports after installation: Web=%d, DB=%d, yasom=%d, yasagent=%d", port, dbPort, yasomPort, yasagentPort)

				// 先完全停止服务（包括数据库），确保所有进程都停止
				ctx.Logger.Info("Stopping YMP service and database...")
				stopCmd := fmt.Sprintf("cd %s && sh bin/ymp.sh stop 2>&1", path.Join(installDir, "yashan-migrate-platform"))
				commonos.ExecuteAsUser(ctx, ympUser, stopCmd, false)

				// 等待进程完全停止
				ctx.Logger.Info("Waiting for processes to stop...")
				waitCmd := "sleep 3"
				ctx.Execute(waitCmd, false)

				// 配置 YMP Web 服务端口
				ctx.Logger.Info("Configuring YMP Web service port to %d in %s", port, appPropsFile)
				result, _ := ctx.Execute(fmt.Sprintf("test -f %s", appPropsFile), false)
				if result != nil && result.GetExitCode() == 0 {
					configurePortCmd := fmt.Sprintf(
						"sed -i 's/^server\\.port=.*/server.port=%d/' %s && grep -q '^server\\.port=' %s || echo 'server.port=%d' >> %s",
						port, appPropsFile, appPropsFile, port, appPropsFile,
					)
					if _, err := ctx.ExecuteWithCheck(configurePortCmd, true); err != nil {
						ctx.Logger.Warn("Failed to configure YMP Web service port in application.properties: %v", err)
					} else {
						ctx.Logger.Info("✓ YMP Web service port configured to %d", port)
					}
				} else {
					ctx.Logger.Warn("application.properties not found at %s, port configuration skipped", appPropsFile)
				}

				// 配置嵌入式数据库端口
				ctx.Logger.Info("Configuring YMP embedded database port to %d", dbPort)

				// 1. 更新 db.properties 中的 YASDB_PORT
				result, _ = ctx.Execute(fmt.Sprintf("test -f %s", dbPropsFile), false)
				if result != nil && result.GetExitCode() == 0 {
					configureDBPortCmd := fmt.Sprintf(
						"sed -i 's/^YASDB_PORT=.*/YASDB_PORT=%d/' %s && grep -q '^YASDB_PORT=' %s || echo 'YASDB_PORT=%d' >> %s",
						dbPort, dbPropsFile, dbPropsFile, dbPort, dbPropsFile,
					)
					if _, err := ctx.ExecuteWithCheck(configureDBPortCmd, true); err != nil {
						ctx.Logger.Warn("Failed to configure database port in db.properties: %v", err)
					} else {
						ctx.Logger.Info("✓ Database port configured to %d in db.properties", dbPort)
					}
				} else {
					ctx.Logger.Warn("db.properties not found at %s, port configuration skipped", dbPropsFile)
				}

				// 2. 更新 application.properties 中的 spring.datasource.url
				result, _ = ctx.Execute(fmt.Sprintf("test -f %s", appPropsFile), false)
				if result != nil && result.GetExitCode() == 0 {
					// 替换 jdbc:yasdb://127.0.0.1:8091/yashan 中的端口号
					configureURLCmd := fmt.Sprintf(
						"sed -i 's|jdbc:yasdb://127\\.0\\.0\\.1:[0-9]\\+/yashan|jdbc:yasdb://127.0.0.1:%d/yashan|g' %s",
						dbPort, appPropsFile,
					)
					if _, err := ctx.ExecuteWithCheck(configureURLCmd, true); err != nil {
						ctx.Logger.Warn("Failed to configure database URL in application.properties: %v", err)
					} else {
						ctx.Logger.Info("✓ Database URL configured to port %d in application.properties", dbPort)
					}
				}

				// 配置 yasom 和 yasagent 端口
				ctx.Logger.Info("Configuring yasom and yasagent ports (yasom: %d, yasagent: %d)", yasomPort, yasagentPort)

				// 尝试查找 profile.toml 文件
				result, _ = ctx.Execute(fmt.Sprintf("find %s -name 'profile.toml' -type f 2>/dev/null | head -1", installDir), false)
				profileFile := profileTomlFile
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					profileFile = strings.TrimSpace(result.GetStdout())
				}

				// 检查文件是否存在
				result, _ = ctx.Execute(fmt.Sprintf("test -f %s", profileFile), false)
				if result != nil && result.GetExitCode() == 0 {
					// 使用 sed 修改 yasom 和 yasagent 端口配置
					if yasomPort != 8093 {
						configureYasomCmd := fmt.Sprintf(
							"sed -i -E 's/(yasom[_-]?port\\s*[=:]\\s*)[0-9]+/\\1%d/g' %s",
							yasomPort, profileFile,
						)
						if _, err := ctx.ExecuteWithCheck(configureYasomCmd, true); err != nil {
							ctx.Logger.Warn("Failed to configure yasom port in profile.toml: %v", err)
						} else {
							ctx.Logger.Info("✓ yasom port configured to %d in profile.toml", yasomPort)
						}
					}

					if yasagentPort != 8094 {
						configureYasagentCmd := fmt.Sprintf(
							"sed -i -E 's/(yasagent[_-]?port\\s*[=:]\\s*)[0-9]+/\\1%d/g' %s",
							yasagentPort, profileFile,
						)
						if _, err := ctx.ExecuteWithCheck(configureYasagentCmd, true); err != nil {
							ctx.Logger.Warn("Failed to configure yasagent port in profile.toml: %v", err)
						} else {
							ctx.Logger.Info("✓ yasagent port configured to %d in profile.toml", yasagentPort)
						}
					}
				} else {
					ctx.Logger.Warn("profile.toml not found at %s, yasom/yasagent port configuration skipped", profileFile)
				}

				// 重新启动服务以使用新端口
				// 注意：数据库端口配置在集群配置中，需要通过 yasboot 或重新初始化数据库
				// 但 YMP 的嵌入式数据库端口是通过 db.properties 中的 YASDB_PORT 配置的
				// 如果数据库已经启动，需要停止并重新启动数据库集群
				ctx.Logger.Info("Restarting YMP service to apply new port configuration...")

				// 尝试通过 yasboot 停止数据库集群（如果存在）
				ympEnvFile := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
				stopDBCmd := fmt.Sprintf("source %s 2>/dev/null && yasboot process stop -c ymp 2>&1 || true", ympEnvFile)
				commonos.ExecuteAsUserWithEnvCtx(ctx, ympUser, ympEnvFile, stopDBCmd, false)

				// 等待数据库完全停止
				ctx.Execute("sleep 2", false)

				// 重新启动 YMP 服务（这会重新启动数据库）
				startCmd := fmt.Sprintf("cd %s && sh bin/ymp.sh start 2>&1", path.Join(installDir, "yashan-migrate-platform"))
				_, startErr := commonos.ExecuteAsUserWithCheck(ctx, ympUser, startCmd, true)
				if startErr != nil {
					ctx.Logger.Warn("Failed to restart YMP service: %v", err)
					ctx.Logger.Info("Please manually restart YMP service to apply new port configuration")
					ctx.Logger.Info("Note: Database port may need to be reconfigured in cluster profile.toml")
				} else {
					ctx.Logger.Info("✓ YMP service restarted with new port configuration")
				}
			}

			ctx.Logger.Info("YMP installation completed successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			// 检查 ymp.env 是否生成
			envFile := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", envFile), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("ymp.env not found at %s (may use alternative path)", envFile)
			} else {
				ctx.Logger.Info("✓ ymp.env found: %s", envFile)
			}

			// 检查 YMP 进程
			result, _ = ctx.Execute(fmt.Sprintf("ps -ef | grep -v grep | grep %s | grep ymp", ympUser), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("✓ YMP process detected")
			}

			return nil
		},
	}
}
