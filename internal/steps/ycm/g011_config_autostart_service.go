// g011_config_autostart_service.go - 配置 YCM systemd 开机自启
// G-011: 创建 ycm.service（installer.md §7.2）；多实例时按端口/安装目录区分 unit 名

package ycm

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const (
	// DefaultServiceName 默认布局（install dir /opt、Web 端口 9060）的 unit 名，与 installer.md 一致
	DefaultServiceName = "ycm"
	defaultYCMPort     = 9060
	defaultInstallDir  = "/opt"
)

// DetermineServiceName 返回本实例的 systemd unit 名（不含 .service 后缀）。
//
// 同一主机多 YCM 须使用不同 Web 端口和/或不同安装目录，避免 unit 冲突：
//   - 非默认端口 9060 → ycm_<port>（与 clean --ycm-port 推断 /opt/ycm_<port> 对齐）
//   - 默认端口且 installDir 非 /opt → ycm_<路径 slug>
//   - 默认端口且 installDir 为 /opt → ycm
func DetermineServiceName(installDir string, ycmPort int) string {
	installDir = strings.TrimRight(strings.TrimSpace(installDir), "/")
	if installDir == "" {
		installDir = defaultInstallDir
	}
	if ycmPort != defaultYCMPort {
		return fmt.Sprintf("ycm_%d", ycmPort)
	}
	if installDir == defaultInstallDir {
		return DefaultServiceName
	}
	slug := sanitizeServiceNameSlug(strings.TrimPrefix(installDir, "/"))
	if slug == "" {
		return DefaultServiceName
	}
	return "ycm_" + slug
}

// InstallDirFromYCMHome 由 YCM 安装根目录（通常为 {installDir}/ycm）反推 installDir。
func InstallDirFromYCMHome(ycmHome string) string {
	ycmHome = strings.TrimRight(strings.TrimSpace(ycmHome), "/")
	if strings.HasSuffix(ycmHome, "/ycm") {
		return strings.TrimSuffix(ycmHome, "/ycm")
	}
	if ycmHome == "" {
		return defaultInstallDir
	}
	return path.Dir(ycmHome)
}

// ServiceNameFromContext 解析 unit 名：优先 --ycm-service-name，否则按 install_dir + ycm_port 推导。
func ServiceNameFromContext(ctx *runner.StepContext) string {
	if ctx == nil {
		return DefaultServiceName
	}
	if name := strings.TrimSpace(ctx.GetParamString("ycm_service_name", "")); name != "" {
		return name
	}
	installDir := ctx.GetParamString("ycm_install_dir", "")
	if installDir == "" {
		installDir = InstallDirFromYCMHome(ctx.GetParamString("ycm_home", "/opt/ycm"))
	}
	return DetermineServiceName(installDir, ctx.GetParamInt("ycm_port", defaultYCMPort))
}

// StepG011ConfigAutostartService 创建并启用 YCM systemd 自启动服务。
func StepG011ConfigAutostartService() *runner.Step {
	return &runner.Step{
		ID:          "G-011",
		Name:        "Configure YCM Autostart Service",
		Description: "Create and enable systemd unit for YCM boot autostart",
		Tags:        []string{"ycm", "autostart", "systemd"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("ycm_autostart", true) {
				ctx.Logger.Info("YCM autostart disabled (--ycm-autostart=false), step will be skipped")
				return nil
			}

			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			yasadm := yasadmPath(installDir)
			svc := ServiceNameFromContext(ctx)

			if !commonos.CheckSystemdAvailable(ctx) {
				return fmt.Errorf("systemctl not found, systemd may not be available")
			}

			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasadm), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx,
					fmt.Errorf("yasadm not found or not executable at %s (run G-007 first)", yasadm))
			}

			if err := commonos.PrivilegedAccessSkipError(ctx, "YCM autostart service (G-011)"); err != nil {
				return err
			}

			ctx.Logger.Info("YCM autostart pre-check passed (yasadm: %s, service: %s)", yasadm, svc)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmLogPhase(ctx, "plan", "G-011: Configure YCM Autostart Service")
			if !ctx.GetParamBool("ycm_autostart", true) {
				ctx.Logger.Info("YCM autostart skipped (--ycm-autostart=false)")
				return nil
			}
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			yasadm := yasadmPath(installDir)
			svc := ServiceNameFromContext(ctx)

			ctx.Logger.Info("Creating YCM systemd service (installer.md §7.2)")
			ctx.Logger.Info("  yasadm: %s", yasadm)
			ctx.Logger.Info("  unit: %s", serviceUnitPath(svc))

			ycmLogPhase(ctx, "autostart-start", fmt.Sprintf("service=%s", svc))
			result, err := createAutostartService(ctx, installDir, svc)
			if err != nil {
				ycmLogPhase(ctx, "autostart-fail", runner.TruncateForLog(err.Error(), 80))
				return err
			}
			ycmLogPhase(ctx, "autostart-done", result.ServiceName)

			ctx.SetResult("ycm_service_name", result.ServiceName)
			ctx.Logger.Info("YCM autostart service configured: %s", result.ServiceName)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("ycm_autostart", true) {
				return nil
			}
			svc := ServiceNameFromContext(ctx)
			if commonos.VerifyAutostartService(ctx, svc) {
				ctx.Logger.Info("Service %s is enabled for autostart", svc)
			} else {
				ctx.Logger.Warn("Service %s may not be enabled", svc)
			}

			r, _ := ctx.Execute(fmt.Sprintf("systemctl is-active %s 2>/dev/null", svc), false)
			if r != nil {
				ctx.Logger.Info("Service %s status: %s", svc, r.GetStdout())
			}
			return nil
		},
	}
}

// CleanAutostartService 停止、禁用并删除 YCM systemd unit（clean 等流程调用）。
// 无 root/sudo 时打日志并返回 false，不执行 systemctl/rm。
func CleanAutostartService(ctx *runner.StepContext, serviceName string) bool {
	if commonos.SkipIfNoPrivilegedAccess(ctx, "YCM autostart service cleanup") {
		return false
	}
	if serviceName == "" {
		serviceName = ServiceNameFromContext(ctx)
	}
	if !commonos.CheckSystemdAvailable(ctx) {
		return false
	}
	unitFile := serviceUnitPath(serviceName)
	ctx.Execute(fmt.Sprintf("systemctl stop %s 2>/dev/null", serviceName), true)
	ctx.Execute(fmt.Sprintf("systemctl disable %s 2>/dev/null", serviceName), true)
	ctx.Execute(fmt.Sprintf("rm -f %s", unitFile), true)
	ctx.Execute("systemctl daemon-reload", true)
	ctx.Execute(fmt.Sprintf("systemctl reset-failed %s 2>/dev/null", serviceName), true)
	return true
}

// AutostartUnitPath 返回 systemd unit 文件路径（供 clean 校验）。
func AutostartUnitPath(serviceName string) string {
	return serviceUnitPath(serviceName)
}

type autostartResult struct {
	ServiceName string
	YasadmPath  string
	ServiceFile string
}

func sanitizeServiceNameSlug(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func yasadmPath(installDir string) string {
	base := strings.TrimRight(strings.TrimSpace(installDir), "/")
	if base == "" {
		base = defaultInstallDir
	}
	return path.Join(base, "ycm", "ycm", "scripts", "yasadm")
}

func serviceUnitPath(serviceName string) string {
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	return fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
}

func generateServiceContent(yasadmPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Yashan Cloud Manager Service
After=network.target

[Service]
Type=forking
ExecStart=%s ycm start
ExecStop=%s ycm stop
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, yasadmPath, yasadmPath)
}

func createAutostartService(ctx *runner.StepContext, installDir, serviceName string) (*autostartResult, error) {
	yasadm := yasadmPath(installDir)
	result := &autostartResult{
		ServiceName: serviceName,
		YasadmPath:  yasadm,
		ServiceFile: serviceUnitPath(serviceName),
	}

	content := generateServiceContent(yasadm)
	ycmLogPhase(ctx, "op-start", fmt.Sprintf("unit=%s", result.ServiceFile))
	cmd := fmt.Sprintf("cat > %s << 'EOFSERVICE'\n%s\nEOFSERVICE", result.ServiceFile, content)
	if _, err := ctx.Execute(cmd, true); err != nil {
		return result, fmt.Errorf("failed to create %s: %w", result.ServiceFile, err)
	}
	ycmLogPhase(ctx, "op-done", "unit-written")

	ctx.Execute("systemctl daemon-reload", true)
	ycmLogPhase(ctx, "op-start", fmt.Sprintf("enable+start service=%s", result.ServiceName))
	ctx.Execute(fmt.Sprintf("systemctl enable %s", result.ServiceName), true)
	ctx.Execute(fmt.Sprintf("systemctl start %s", result.ServiceName), true)
	ycmLogPhase(ctx, "op-done", result.ServiceName)

	return result, nil
}
