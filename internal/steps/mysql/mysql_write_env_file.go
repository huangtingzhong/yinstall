package mysql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepWriteEnvFile writes shell env file for mysql instance.
func stepWriteEnvFile() *runner.Step {
	write := func(ctx *runner.StepContext) error {
		layout, err := layoutFromCtx(ctx)
		if err != nil {
			return err
		}
		port := layout.Port
		envContent := fmt.Sprintf(`export VERSION=%s
export MYSQL_PORT=%d
export MYSQL_BASE=%s
export MYSQL_HOME=%s
export MYSQL_DATA=%s
export MYSQL_OTHER=%s
export PATH=$MYSQL_HOME/bin:$PATH
`, layout.Version, port, layout.Base, layout.Home, layout.Data, layout.Other)

		platform := ctx.GetTargetPlatform()
		mysqlLogPhase(ctx, "plan", "M-009 env file")
		ctx.LogScriptPreview("shell", "env-file", envContent)

		switch platform {
		case PlatformWindows:
			bat := fmt.Sprintf(`set VERSION=%s
set MYSQL_PORT=%d
set MYSQL_BASE=%s
set MYSQL_HOME=%s
set MYSQL_DATA=%s
set MYSQL_OTHER=%s
set PATH=%%MYSQL_HOME%%\bin;%%PATH%%
`, layout.Version, port, layout.Base, layout.Home, layout.Data, layout.Other)
			batPath := fmt.Sprintf("%s/%d.bat", layout.Other, port)
			err = file.RemoteWriteTextFile(ctx, batPath, bat, false)
			return err
		default:
			envFile, err := resolveMysqlEnvFilePath(ctx, ctx.GetParamString("mysql_env_file", ""), port)
			if err != nil {
				return err
			}
			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", commonos.ShellSingleQuote(envFile), envContent)
			_, err = ctx.ExecuteWithCheck(cmd, UseSudo(ctx))
			return err
		}
	}

	return &runner.Step{
		Name:        "Write Env File",
		Description: "Write mysql environment file",
		Tags:        []string{"mysql", "env", "mysql-instance"},
		PreCheck: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
			}
			return requireSoftwareForInstanceStage(ctx, layout)
		},
		Action: write,
	}
}

// resolveMysqlEnvFilePath returns the env file path for the operator (SSH login or local executor).
// Default ~/.{port} is resolved via $HOME of the current session user, not the mysql product user.
func resolveMysqlEnvFilePath(ctx *runner.StepContext, raw string, port int) (string, error) {
	envFile := strings.TrimSpace(raw)
	if envFile == "" {
		envFile = fmt.Sprintf("~/.%d", port)
	}
	if envFile == "" || envFile[0] != '~' {
		return envFile, nil
	}
	homeRes, _ := ctx.Execute("echo $HOME", false)
	home := "/root"
	if homeRes != nil {
		if h := strings.TrimSpace(homeRes.GetStdout()); h != "" {
			home = h
		}
	}
	return home + envFile[1:], nil
}
