// g006_configure_ports.go - 写入 YCM 部署配置（端口、install_path、辅助端口）
// G-006: 修改 deploy.yml 中的端口与 install_path

package ycm

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	ycmYasAioAPIPortOffset = 8
	ycmAgentPortOffset     = 10
	ycmExportPortOffset    = 11
)

type ycmDeployPortField struct {
	yamlKey  string
	paramKey string
	defVal   int
	offset   int // >0 表示相对 web 端口偏移；0 表示使用 paramKey 值
}

func ycmDeployPortFields() []ycmDeployPortField {
	return []ycmDeployPortField{
		{"ycm_port", "ycm_port", 9060, 0},
		{"prometheus_port", "ycm_prometheus_port", 9061, 1},
		{"loki_http_port", "ycm_loki_http_port", 9062, 2},
		{"loki_grpc_port", "ycm_loki_grpc_port", 9063, 3},
		{"yasdb_exporter_port", "ycm_yasdb_exporter_port", 9064, 4},
		{"agent_port", "", 0, ycmAgentPortOffset},
		{"export_port", "", 0, ycmExportPortOffset},
	}
}

// ycmAuxListenPorts 返回 deploy 中相对 web 端口偏移的辅助监听端口（G-001 预检用）。
func ycmAuxListenPorts(webPort int) []struct {
	name string
	port int
} {
	return []struct {
		name string
		port int
	}{
		{"yas-aio-api", webPort + ycmYasAioAPIPortOffset},
		{"agent", webPort + ycmAgentPortOffset},
		{"export", webPort + ycmExportPortOffset},
	}
}

func ycmDeployPortValue(ctx *runner.StepContext, f ycmDeployPortField) int {
	webPort := ctx.GetParamInt("ycm_port", defaultYCMPort)
	if f.offset > 0 {
		if f.paramKey != "" {
			if _, ok := ctx.Params[f.paramKey]; ok {
				return ctx.GetParamInt(f.paramKey, f.defVal)
			}
		}
		return webPort + f.offset
	}
	return ctx.GetParamInt(f.paramKey, f.defVal)
}

func configureYCMInstallPath(ctx *runner.StepContext, deployFile, ycmHome string) error {
	ycmHome = strings.TrimRight(strings.TrimSpace(ycmHome), "/")
	if ycmHome == "" {
		return fmt.Errorf("ycm_home is empty")
	}
	ctx.Logger.Info("Setting install_path: %s", ycmHome)
	cmd := fmt.Sprintf(`sed -i 's|\(install_path:[[:space:]]*\).*|\1%s|' %s`, ycmHome, deployFile)
	result, err := ctx.Execute(cmd, true)
	if err != nil {
		return fmt.Errorf("failed to set install_path: %w", err)
	}
	if result != nil && result.GetExitCode() != 0 {
		return fmt.Errorf("sed install_path returned exit code %d", result.GetExitCode())
	}
	return nil
}

func configureYCMYasAioAPIPort(ctx *runner.StepContext, deployFile string, port int) error {
	ctx.Logger.Info("Setting yas-aio-api port: %d", port)
	cmd := fmt.Sprintf(`sed -i '/yas-aio-api:/,/^user:/ s/^\([[:space:]]*port:[[:space:]]*\)[0-9]*/\1%d/' %s`, port, deployFile)
	result, err := ctx.Execute(cmd, true)
	if err != nil {
		return fmt.Errorf("failed to set yas-aio-api port: %w", err)
	}
	if result != nil && result.GetExitCode() != 0 {
		return fmt.Errorf("sed yas-aio-api port returned exit code %d", result.GetExitCode())
	}
	return nil
}

// stepConfigurePorts 写入 YCM 端口配置
func stepConfigurePorts() *runner.Step {
	return &runner.Step{
		Name:        "Configure YCM Ports",
		Description: "Write port configuration to deploy.yml",
		Tags:        []string{"ycm", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", deployFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("deploy config not found: %s (run G-005 first)", deployFile))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmLogPhase(ctx, "plan", "G-006: Configure YCM Ports")
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			ycmHome := YCMHomeFromContext(ctx)

			// 备份配置文件
			backupCmd := fmt.Sprintf("cp %s %s.bak 2>/dev/null || true", deployFile, deployFile)
			ctx.Execute(backupCmd, false)
			ctx.Logger.Info("Backed up deploy config: %s.bak", deployFile)

			if err := configureYCMInstallPath(ctx, deployFile, ycmHome); err != nil {
				return err
			}

			for _, p := range ycmDeployPortFields() {
				portVal := ycmDeployPortValue(ctx, p)
				ctx.Logger.Info("Setting %s: %d", p.yamlKey, portVal)

				cmd := fmt.Sprintf("sed -i 's/\\(%s:\\s*\\)[0-9]*/\\1%d/' %s", p.yamlKey, portVal, deployFile)
				result, err := ctx.Execute(cmd, true)
				if err != nil {
					return fmt.Errorf("failed to set %s: %w", p.yamlKey, err)
				}
				if result != nil && result.GetExitCode() != 0 {
					ctx.Logger.Warn("sed returned non-zero for %s, port may not exist in config", p.yamlKey)
				}
			}

			yasAioPort := ctx.GetParamInt("ycm_port", 9060) + ycmYasAioAPIPortOffset
			if err := configureYCMYasAioAPIPort(ctx, deployFile, yasAioPort); err != nil {
				return err
			}

			ctx.Logger.Info("Port and install_path configuration updated in %s", deployFile)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			ycmHome := YCMHomeFromContext(ctx)

			result, _ := ctx.Execute(fmt.Sprintf("grep 'install_path' %s | grep -F '%s'", deployFile, ycmHome), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("install_path may not be correctly set to %s in %s", ycmHome, deployFile)
			} else {
				ctx.Logger.Info("OK: install_path = %s", ycmHome)
			}

			for _, p := range ycmDeployPortFields() {
				portVal := ycmDeployPortValue(ctx, p)
				cmd := fmt.Sprintf("grep '%s' %s | grep -E '(^|[^0-9])%d([^0-9]|$)'", p.yamlKey, deployFile, portVal)
				result, _ := ctx.Execute(cmd, false)
				if result != nil && result.GetExitCode() == 0 {
					ctx.Logger.Info("OK: %s = %d", p.yamlKey, portVal)
				} else {
					ctx.Logger.Warn("Port %s may not be correctly set to %d in %s", p.yamlKey, portVal, deployFile)
				}
			}

			yasAioPort := ctx.GetParamInt("ycm_port", 9060) + ycmYasAioAPIPortOffset
			cmd := fmt.Sprintf("grep 'port' %s | grep -E '(^|[^0-9])%d([^0-9]|$)'", deployFile, yasAioPort)
			result, _ = ctx.Execute(cmd, false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("OK: yas-aio-api port = %d", yasAioPort)
			} else {
				ctx.Logger.Warn("yas-aio-api port may not be correctly set to %d in %s", yasAioPort, deployFile)
			}
			return nil
		},
	}
}
