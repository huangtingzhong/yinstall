package mysql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepPortCheck verifies mysql and mysqlx ports are free.
func stepPortCheck() *runner.Step {
	checkPort := func(ctx *runner.StepContext, port int) error {
		platform := ctx.GetTargetPlatform()
		var cmd string
		switch platform {
		case PlatformWindows:
			cmd = fmt.Sprintf(`powershell -NoProfile -Command "(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Measure-Object).Count -eq 0"`, port)
		default:
			cmd = fmt.Sprintf("ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':%d([^0-9]|$)' || true", port, port)
		}
		res, err := ctx.Execute(cmd, false)
		if err != nil {
			return err
		}
		if platform == PlatformWindows {
			if res == nil {
				return fmt.Errorf("port %d check failed on %s", port, ctx.Executor.Host())
			}
			out := strings.TrimSpace(strings.ToLower(res.GetStdout()))
			if out == "false" || res.GetExitCode() != 0 {
				return fmt.Errorf("port %d already in use on %s", port, ctx.Executor.Host())
			}
			return nil
		}
		if res != nil && strings.TrimSpace(res.GetStdout()) != "" {
			return fmt.Errorf("port %d already in use on %s", port, ctx.Executor.Host())
		}
		return nil
	}

	run := func(ctx *runner.StepContext) error {
		port := ctx.GetParamInt("mysql_port", 3306)
		mysqlXPort, _ := strconv.Atoi(fmt.Sprintf("%d0", port))
		if err := checkPort(ctx, port); err != nil {
			return err
		}
		return checkPort(ctx, mysqlXPort)
	}

	return &runner.Step{
		Name:        "Port Check",
		Description: "Verify MySQL and MySQLX ports are available",
		Tags:        []string{"mysql", "port", "mysql-instance"},
		PreCheck:    run,
		Action: func(ctx *runner.StepContext) error {
			mysqlLogPhase(ctx, "plan", "M-003 port check")
			return run(ctx)
		},
	}
}
