// autostart.go - 自启动配置公共函数
// 提供 yashan_monit.sh 脚本和 systemd 服务配置的通用逻辑
// 被 DB 安装和备库添加步骤共用

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// AutostartConfig 自启动配置参数
type AutostartConfig struct {
	User        string // 操作系统用户名
	ClusterName string // 数据库集群名
	BeginPort   int    // 数据库起始端口
	IsYACMode   bool   // 是否 YAC 模式
}

// AutostartResult 自启动配置结果
type AutostartResult struct {
	ScriptPath  string // 脚本路径
	ServiceName string // 服务名称
	ServiceArg  string // 服务参数 (bashrc 或端口号)
	YasdbCount  int    // 运行中的 yasdb 进程数
}

const (
	// ScriptPath yashan_monit.sh 脚本路径
	ScriptPath = "/usr/local/bin/yashan_monit.sh"
)

// GenerateMonitScript 生成 yashan_monit.sh 脚本内容
// 支持单实例和多实例场景
func GenerateMonitScript(user string, ycsrootagentAutostart string) string {
	return fmt.Sprintf(`#!/bin/bash
if [ $# -eq 0 ]; then
    echo "Usage: $0 <PORT> or $0 bashrc"
    echo "Example: $0 1788"
    echo "Example: $0 bashrc"
    exit 1
fi

MONIT_AUTOSTART="true"                
YCSROOTAGENT_AUTOSTART="%s"
YASDB_USER=%s                              
INTERVAL=3                              
PORT=$1

# Determine environment file based on PORT argument
if [ "$PORT" = "bashrc" ]; then
    ENV_FILE="/home/${YASDB_USER}/.bashrc"
else
    ENV_FILE="/home/${YASDB_USER}/.${PORT}"
fi

if [ -f "$ENV_FILE" ]; then
    source "$ENV_FILE"
else
    echo "$(date) Error: Environment file $ENV_FILE not found"
    exit 1
fi

if [ -z "$YASDB_HOME" ]; then
    echo "$(date) Error: YASDB_HOME is not set. Please check $ENV_FILE"
    exit 1
fi

export LD_LIBRARY_PATH=$YASDB_HOME/lib

MONITRC_FILE="$YASDB_HOME/om/monit/monitrc"
if [ -f "$MONITRC_FILE" ]; then
    CURRENT_OWNER=$(stat -c '%%U' "$MONITRC_FILE" 2>/dev/null || stat -f '%%Su' "$MONITRC_FILE" 2>/dev/null)
    if [ "$CURRENT_OWNER" != "$YASDB_USER" ]; then
        echo "$(date) Fixing monitrc owner: $CURRENT_OWNER -> $YASDB_USER"
        chown $YASDB_USER "$MONITRC_FILE"
    fi
    
    CURRENT_PERM=$(stat -c '%%a' "$MONITRC_FILE" 2>/dev/null)
    if [ -z "$CURRENT_PERM" ]; then
        PERM_STR=$(ls -ld "$MONITRC_FILE" 2>/dev/null | awk '{print $1}')
        if [ -z "$PERM_STR" ]; then
            CURRENT_PERM="000"
        else
            CURRENT_PERM="000"
        fi
    fi
    if [ "$CURRENT_PERM" != "700" ]; then
        echo "$(date) Fixing monitrc permissions: $CURRENT_PERM -> 700"
        chmod 0700 "$MONITRC_FILE"
    fi
else
    echo "$(date) Warning: $MONITRC_FILE not found"
    exit 1
fi

while true; do    
    if [ "$MONIT_AUTOSTART" = "true" ]; then
        if ! pgrep -a monit | grep "$YASDB_HOME" > /dev/null; then
            echo "$(date) monit abnormal, try restart..." 
            if [ "$PORT" = "bashrc" ]; then
                su - $YASDB_USER -c "source /home/${YASDB_USER}/.bashrc && $YASDB_HOME/om/bin/monit -c $YASDB_HOME/om/monit/monitrc" &
            else
                su - $YASDB_USER -c "source /home/${YASDB_USER}/.${PORT} && $YASDB_HOME/om/bin/monit -c $YASDB_HOME/om/monit/monitrc" &
            fi
        fi
    fi
    if [ "$YCSROOTAGENT_AUTOSTART" = "true" ]; then
       if [ -n "$YASCS_HOME" ]; then
         if ! pgrep -a ycsrootagent | grep "$YASCS_HOME" > /dev/null; then
           echo "$(date) ycsrootagent abnormal, try restart..."
           if [ "$PORT" = "bashrc" ]; then
               su - $YASDB_USER -c "source /home/${YASDB_USER}/.bashrc && sudo env LD_LIBRARY_PATH=$YASDB_HOME/lib $YASDB_HOME/bin/ycsrootagent start -H $YASCS_HOME" & 
           else
               su - $YASDB_USER -c "source /home/${YASDB_USER}/.${PORT} && sudo env LD_LIBRARY_PATH=$YASDB_HOME/lib $YASDB_HOME/bin/ycsrootagent start -H $YASCS_HOME" & 
           fi
         fi
       fi
    fi
    sleep "$INTERVAL"
done
`, ycsrootagentAutostart, user)
}

// GenerateServiceContent 生成 systemd 服务文件内容
func GenerateServiceContent(clusterName, serviceArg, serviceName string) string {
	return fmt.Sprintf(`[Unit]
Description=Yashan Monitor Service (%s)
After=network.target

[Service]
ExecStart=%s %s
Type=simple
Restart=always
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`, clusterName, ScriptPath, serviceArg, serviceName)
}

// DetermineServiceName 根据 yasdb 进程数确定服务名称和参数
// - 单实例: yashan_monit, bashrc
// - 多实例: yashan_monit_<port>, <port>
func DetermineServiceName(yasdbCount int, beginPort int) (serviceName string, serviceArg string) {
	if yasdbCount <= 1 {
		return "yashan_monit", "bashrc"
	}
	return fmt.Sprintf("yashan_monit_%d", beginPort), fmt.Sprintf("%d", beginPort)
}

// CreateAutostartScript 创建或更新 yashan_monit.sh 脚本
func CreateAutostartScript(ctx *runner.StepContext, cfg *AutostartConfig) error {
	ycsrootagentAutostart := "false"
	if cfg.IsYACMode {
		ycsrootagentAutostart = "true"
	}

	scriptContent := GenerateMonitScript(cfg.User, ycsrootagentAutostart)

	cmd := fmt.Sprintf("cat > %s << 'EOFSCRIPT'\n%s\nEOFSCRIPT", ScriptPath, scriptContent)
	if _, err := ctx.Execute(cmd, true); err != nil {
		return fmt.Errorf("failed to create yashan_monit.sh: %w", err)
	}

	if _, err := ctx.Execute(fmt.Sprintf("chmod +x %s", ScriptPath), true); err != nil {
		return fmt.Errorf("failed to make script executable: %w", err)
	}

	return nil
}

// CreateAutostartService 创建并启动 systemd 服务
func CreateAutostartService(ctx *runner.StepContext, cfg *AutostartConfig) (*AutostartResult, error) {
	yasdbCount := GetYasdbProcessCount(ctx)

	serviceName, serviceArg := DetermineServiceName(yasdbCount, cfg.BeginPort)

	result := &AutostartResult{
		ScriptPath:  ScriptPath,
		ServiceName: serviceName,
		ServiceArg:  serviceArg,
		YasdbCount:  yasdbCount,
	}

	serviceContent := GenerateServiceContent(cfg.ClusterName, serviceArg, serviceName)
	serviceFile := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	cmd := fmt.Sprintf("cat > %s << 'EOFSERVICE'\n%s\nEOFSERVICE", serviceFile, serviceContent)
	if _, err := ctx.Execute(cmd, true); err != nil {
		return result, fmt.Errorf("failed to create service file: %w", err)
	}

	ctx.Execute("systemctl daemon-reload", true)
	ctx.Execute(fmt.Sprintf("systemctl enable %s", serviceName), true)
	ctx.Execute(fmt.Sprintf("systemctl start %s", serviceName), true)

	return result, nil
}

// ConfigureAutostart 配置自启动（脚本 + 服务）
func ConfigureAutostart(ctx *runner.StepContext, cfg *AutostartConfig) (*AutostartResult, error) {
	if err := CreateAutostartScript(ctx, cfg); err != nil {
		return nil, err
	}
	return CreateAutostartService(ctx, cfg)
}

// VerifyAutostartService 验证服务是否已启用
func VerifyAutostartService(ctx *runner.StepContext, serviceName string) bool {
	result, _ := ctx.Execute(fmt.Sprintf("systemctl is-enabled %s 2>/dev/null", serviceName), false)
	if result != nil && strings.TrimSpace(result.GetStdout()) == "enabled" {
		return true
	}
	return false
}

// CheckSystemdAvailable 检查 systemd 是否可用
func CheckSystemdAvailable(ctx *runner.StepContext) bool {
	result, _ := ctx.Execute("which systemctl", false)
	return result != nil && result.GetExitCode() == 0
}
