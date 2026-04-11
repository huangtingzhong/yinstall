// h006_install_jdk.go - 安装 JDK
// H-006: 安装 JDK（可选步骤）
//
// 执行策略：
//  1. 检查 java 是否已安装 → 已安装则跳过
//  2. 指定了 --ymp-jdk-package → 上传 RPM/tar.gz 并安装
//  3. 未指定包 → 通过 yum/dnf/apt 自动安装（参考 libaio 安装策略）
//  4. 安装后验证 java 可用，失败则报错退出

package ymp

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// jdkPkgName 根据包管理器和期望版本返回 JDK 包名
func jdkPkgName(pkgManager, version string) string {
	if pkgManager == "apt" {
		return fmt.Sprintf("openjdk-%s-jdk", version)
	}
	// yum / dnf / zypper
	return fmt.Sprintf("java-%s-openjdk", version)
}

// StepH006InstallJDK 安装 JDK（已安装则跳过）
func StepH006InstallJDK() *runner.Step {
	return &runner.Step{
		ID:          "H-006",
		Name:        "Install JDK",
		Description: "Install JDK: skip if already installed; use --ymp-jdk-package if specified, otherwise fallback to yum/dnf/apt",
		Tags:        []string{"ymp", "jdk"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// Java 已安装 → 跳过本步骤
			result, _ := ctx.Execute("which java 2>/dev/null", false)
			if result != nil && result.GetExitCode() == 0 {
				vr, _ := ctx.Execute("java -version 2>&1 | head -1", false)
				ver := ""
				if vr != nil {
					ver = strings.TrimSpace(vr.GetStdout())
					if ver == "" {
						ver = strings.TrimSpace(vr.GetStderr())
					}
				}
				ctx.Logger.Info("JDK already installed: %s", ver)
				return fmt.Errorf("JDK already installed: %s", ver)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			jdkPackage := ctx.GetParamString("ymp_jdk_package", "")
			expectedVersion := ctx.GetParamString("ymp_jdk_version", "17")

			// 通过包管理器安装时，若 local-iso 模式需先确保 ISO 挂载和 repo 就绪
			if jdkPackage == "" {
				if err := commonos.EnsureLocalISORepo(ctx); err != nil {
					return fmt.Errorf("failed to prepare local ISO repo: %w", err)
				}
			}

			if jdkPackage != "" {
				// ── 策略 1：用指定软件包安装 ──────────────────────────────────
				ctx.Logger.Info("Installing JDK from specified package: %s", jdkPackage)
				fullPath, err := commonfile.FindAndDistribute(
					ctx,
					jdkPackage,
					ctx.LocalSoftwareDirs,
					ctx.RemoteSoftwareDir,
				)
				if err != nil {
					return fmt.Errorf("JDK package not found: %w", err)
				}
				ctx.Logger.Info("JDK package located at: %s", fullPath)

				var cmd string
				switch {
				case strings.HasSuffix(fullPath, ".rpm"):
					cmd = fmt.Sprintf("rpm -ivh %s", fullPath)
				case strings.HasSuffix(fullPath, ".tar.gz"), strings.HasSuffix(fullPath, ".tgz"):
					installDir := ctx.GetParamString("ymp_jdk_install_dir", "/usr/local")
					cmd = fmt.Sprintf("tar -zxf %s -C %s", fullPath, installDir)
				default:
					return fmt.Errorf("unsupported JDK package format: %s (expected .rpm or .tar.gz/.tgz)", fullPath)
				}

				if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf("failed to install JDK from package: %w", err)
				}
				ctx.Logger.Info("JDK installed from package successfully")
			} else {
				// ── 策略 2：通过包管理器自动安装 ─────────────────────────────
				pkgManager := commonos.GetPkgManager(ctx.OSInfo)
				if pkgManager == "" {
					return fmt.Errorf(
						"no supported package manager found and --ymp-jdk-package not specified; " +
							"install JDK manually or provide --ymp-jdk-package=<jdk.rpm>")
				}

				pkgName := jdkPkgName(pkgManager, expectedVersion)
				yumMode := ctx.GetParamString("os_yum_mode", "none")
				isRHEL8 := commonos.IsRHEL8(ctx.OSInfo)
				cmd := commonos.BuildInstallCmd(pkgManager, yumMode, pkgName, isRHEL8)

				ctx.Logger.Info("Installing JDK via %s: %s", pkgManager, pkgName)
				if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
					return fmt.Errorf(
						"failed to install JDK via %s (package: %s): %w; "+
							"provide --ymp-jdk-package=<jdk.rpm> to install from a local package instead",
						pkgManager, pkgName, err)
				}
				ctx.Logger.Info("JDK installed via %s successfully", pkgManager)
			}

			// 安装后验证 java 可用
			result, _ := ctx.Execute("which java 2>/dev/null", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf(
					"JDK installation completed but 'java' command still not found; " +
						"check PATH or provide --ymp-jdk-package=<jdk.rpm> with a different package")
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("java -version 2>&1 | head -1", false)
			if result == nil {
				return fmt.Errorf("java command not available after installation")
			}
			ver := strings.TrimSpace(result.GetStdout())
			if ver == "" {
				ver = strings.TrimSpace(result.GetStderr())
			}
			if ver == "" {
				return fmt.Errorf("java -version returned empty output")
			}
			ctx.Logger.Info("✓ JDK verified: %s", ver)
			return nil
		},
	}
}
