// g005_configure_ports.go - 写入 YCM 端口配置
// G-005: 修改 deploy.yml 中的端口配置

package ycm

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepG005ConfigurePorts 写入 YCM 端口配置
func StepG005ConfigurePorts() *runner.Step {
	return &runner.Step{
		ID:          "G-005",
		Name:        "Configure YCM Ports",
		Description: "Write port configuration to deploy.yml",
		Tags:        []string{"ycm", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", deployFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("deploy config not found: %s (run G-004 first)", deployFile))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")

			// 端口参数
			ports := []struct {
				key      string
				paramKey string
				defVal   int
			}{
				{"ycm_port", "ycm_port", 9060},
				{"prometheus_port", "ycm_prometheus_port", 9061},
				{"loki_http_port", "ycm_loki_http_port", 9062},
				{"loki_grpc_port", "ycm_loki_grpc_port", 9063},
				{"yasdb_exporter_port", "ycm_yasdb_exporter_port", 9064},
			}

			// 备份配置文件
			backupCmd := fmt.Sprintf("cp %s %s.bak 2>/dev/null || true", deployFile, deployFile)
			ctx.Execute(backupCmd, false)
			ctx.Logger.Info("Backed up deploy config: %s.bak", deployFile)

			for _, p := range ports {
				portVal := ctx.GetParamInt(p.paramKey, p.defVal)
				ctx.Logger.Info("Setting %s: %d", p.key, portVal)

				// 使用 sed 替换端口值（匹配 key: <value> 格式）
				cmd := fmt.Sprintf("sed -i 's/\\(%s:\\s*\\)[0-9]*/\\1%d/' %s", p.key, portVal, deployFile)
				result, err := ctx.Execute(cmd, true)
				if err != nil {
					return fmt.Errorf("failed to set %s: %w", p.key, err)
				}
				if result != nil && result.GetExitCode() != 0 {
					ctx.Logger.Warn("sed returned non-zero for %s, port may not exist in config", p.key)
				}
			}

			ctx.Logger.Info("Port configuration updated in %s", deployFile)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")

			// 验证端口值已写入
			ports := []struct {
				key      string
				paramKey string
				defVal   int
			}{
				{"ycm_port", "ycm_port", 9060},
				{"prometheus_port", "ycm_prometheus_port", 9061},
				{"loki_http_port", "ycm_loki_http_port", 9062},
				{"loki_grpc_port", "ycm_loki_grpc_port", 9063},
				{"yasdb_exporter_port", "ycm_yasdb_exporter_port", 9064},
			}

			for _, p := range ports {
				portVal := ctx.GetParamInt(p.paramKey, p.defVal)
				// 使用精确匹配避免误匹配（如 9060 不会匹配到 90600）
				// 匹配格式：key: portVal 或 key: portVal, 或 key: portVal}
				cmd := fmt.Sprintf("grep '%s' %s | grep -E '(^|[^0-9])%d([^0-9]|$)'", p.key, deployFile, portVal)
				result, _ := ctx.Execute(cmd, false)
				if result != nil && result.GetExitCode() == 0 {
					ctx.Logger.Info("OK: %s = %d", p.key, portVal)
				} else {
					ctx.Logger.Warn("Port %s may not be correctly set to %d in %s", p.key, portVal, deployFile)
				}
			}
			return nil
		},
	}
}
