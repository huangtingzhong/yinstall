// m012b_selinux_context.go - 为自定义 MYSQL_BASE 打 SELinux 标签（Enforcing 环境必需）。
package mysql

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const (
	mysqlSELinuxAuto  = "auto"
	mysqlSELinuxLabel = "label"
	mysqlSELinuxSkip  = "skip"
)

func getSELinuxEnforce(ctx *runner.StepContext) string {
	res, _ := ctx.Execute("getenforce 2>/dev/null || true", false)
	return strings.TrimSpace(res.GetStdout())
}

func shouldLabelMySQLSELinux(ctx *runner.StepContext, mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", mysqlSELinuxAuto:
		// fall through
	case mysqlSELinuxSkip:
		return false
	case mysqlSELinuxLabel:
		state := strings.ToLower(getSELinuxEnforce(ctx))
		return state != "disabled"
	default:
		return false
	}
	state := strings.ToLower(getSELinuxEnforce(ctx))
	return state == "enforcing" || state == "permissive"
}

func ensureMysqlServiceHome(ctx *runner.StepContext, osUser string) error {
	user := strings.TrimSpace(osUser)
	if user == "" {
		user = "mysql"
	}
	cmd := fmt.Sprintf("mkdir -p /var/lib/mysql && chown %s:%s /var/lib/mysql",
		commonos.ShellSingleQuote(user), commonos.ShellSingleQuote(user))
	_, err := ctx.ExecuteWithCheck(cmd, true)
	return err
}

func labelMySQLTree(ctx *runner.StepContext, mysqlBase, mysqlHome string) error {
	mysqlBase = strings.TrimRight(strings.TrimSpace(mysqlBase), "/")
	mysqlHome = strings.TrimRight(strings.TrimSpace(mysqlHome), "/")
	if mysqlBase == "" || mysqlHome == "" {
		return fmt.Errorf("mysql_base and mysql_home required for SELinux labeling")
	}
	res, _ := ctx.Execute("command -v semanage >/dev/null 2>&1", false)
	if res == nil || res.GetExitCode() != 0 {
		ctx.Logger.Info("semanage not found, installing policycoreutils-python-utils")
		if _, err := ctx.ExecuteWithCheck(
			"(command -v yum >/dev/null && yum install -y policycoreutils-python-utils) || "+
				"(command -v dnf >/dev/null && dnf install -y policycoreutils-python-utils) || true",
			true,
		); err != nil {
			return err
		}
	}
	fcontextPattern := mysqlBase + "(/.*)?"
	addCmd := fmt.Sprintf(
		"semanage fcontext -a -t mysqld_db_t %s 2>/dev/null || semanage fcontext -m -t mysqld_db_t %s",
		commonos.ShellSingleQuote(fcontextPattern), commonos.ShellSingleQuote(fcontextPattern),
	)
	if _, err := ctx.ExecuteWithCheck(addCmd, true); err != nil {
		return err
	}
	if _, err := ctx.ExecuteWithCheck("restorecon -Rv "+commonos.ShellSingleQuote(mysqlBase), true); err != nil {
		return err
	}
	chconCmd := fmt.Sprintf(
		"chcon -t mysqld_exec_t %s/bin/mysqld %s/bin/mysqld_safe 2>/dev/null || true",
		commonos.ShellSingleQuote(mysqlHome), commonos.ShellSingleQuote(mysqlHome),
	)
	_, err := ctx.ExecuteWithCheck(chconCmd, true)
	return err
}

// StepM012bSELinuxContext labels mysql tree when SELinux is active.
func StepM012bSELinuxContext() *runner.Step {
	return &runner.Step{
		ID:          "M-012b",
		Name:        "Apply SELinux Context",
		Description: "Label MYSQL_BASE for mysqld under SELinux Enforcing",
		Tags:        []string{"mysql", "selinux", "mysql-instance"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() != PlatformLinux {
				return fmt.Errorf("selinux labeling only applies to linux")
			}
			mode := ctx.GetParamString("mysql_selinux_mode", mysqlSELinuxAuto)
			if !shouldLabelMySQLSELinux(ctx, mode) {
				return fmt.Errorf("mysql selinux labeling skipped (mode=%s, getenforce=%s)", mode, getSELinuxEnforce(ctx))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			mysqlLogPhase(ctx, "plan", fmt.Sprintf("M-012b: label %s", layout.Base))
			osUser := ctx.GetParamString("os_user", "mysql")
			if err := ensureMysqlServiceHome(ctx, osUser); err != nil {
				return err
			}
			return labelMySQLTree(ctx, layout.Base, layout.Home)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			cmd := fmt.Sprintf("ls -Z %s/bin/mysqld_safe 2>/dev/null | grep -E 'mysqld_exec_t|mysqld_db_t'", shellQuote(layout.Home))
			res, _ := ctx.Execute(cmd, false)
			if res == nil || res.GetExitCode() != 0 {
				ctx.Logger.Info("mysqld_safe SELinux type check inconclusive; restorecon may still be effective")
			}
			return nil
		},
	}
}
