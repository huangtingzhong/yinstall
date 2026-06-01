// shell.go - 安装域多行 shell 脚本远端执行（临时文件 + LogScriptPreview）
package install

import (
	"fmt"
	"os"
	"time"

	"github.com/yinstall/internal/runner"
)

// RunShellScript 以临时文件方式在目标机执行 shell 脚本（与 stressRunShell / collectRunShell 对称）。
// 避免多行脚本内嵌 bash -c 时的引号/换行问题；执行前写入 script=shell 预览。
func RunShellScript(ctx *runner.StepContext, script string, sudo bool) (stdout string, err error) {
	if ctx == nil || ctx.Executor == nil {
		return "", fmt.Errorf("executor not available")
	}

	localTmp, err := os.CreateTemp("", "install_sh_*.sh")
	if err != nil {
		return "", fmt.Errorf("create local tmp shell file: %w", err)
	}
	localTmpName := localTmp.Name()
	defer os.Remove(localTmpName)

	if _, err := localTmp.WriteString(script + "\n"); err != nil {
		localTmp.Close()
		return "", fmt.Errorf("write local tmp shell: %w", err)
	}
	localTmp.Close()

	remotePath := fmt.Sprintf("/tmp/.install_sh_%d.sh", time.Now().UnixNano())
	ctx.LogScriptPreview("shell", "remote="+remotePath, script)
	if err := ctx.Executor.Upload(localTmpName, remotePath, ctx.UploadContext()); err != nil {
		return "", fmt.Errorf("upload shell file: %w", err)
	}

	_, _ = ctx.Execute(fmt.Sprintf("chmod 755 %s", remotePath), false)

	result, execErr := ctx.ExecuteWithCheck(fmt.Sprintf("bash %s", remotePath), sudo)

	_, _ = ctx.Execute(fmt.Sprintf("rm -f %s", remotePath), false)

	if result != nil {
		stdout = result.GetStdout()
	}
	return stdout, execErr
}
