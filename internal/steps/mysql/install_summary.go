package mysql

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

func hasCustomSQLScript(ctx *runner.StepContext) bool {
	return ctx != nil && strings.TrimSpace(ctx.GetParamString("mysql_custom_sql_script", "")) != ""
}

func printMysqlInstallSummary(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || ctx.Logger == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	layout, err := layoutFromCtx(ctx)
	if err != nil {
		return err
	}
	platform := ctx.GetTargetPlatform()
	host := commonmysql.TargetHost(ctx)
	stage := installStage(ctx)
	password := ctx.GetParamString("mysql_root_password", "")

	notice := func(msg string) {
		ctx.Logger.ConsoleNotice(stepID, msg)
	}

	if stage == commonmysql.StageSoftware {
		notice(fmt.Sprintf("======== MySQL Software Summary (%s) ========", host))
		notice("[Paths]")
		notice(fmt.Sprintf("  mysql_base=%s", layout.Base))
		if layout.Home != "" {
			notice(fmt.Sprintf("  mysql_home=%s", layout.Home))
		}
		if layout.Version != "" {
			notice(fmt.Sprintf("  mysql_version=%s", layout.Version))
		}
		notice("======== end software summary ========")
		return nil
	}

	notice(fmt.Sprintf("======== MySQL Instance Summary (%s) ========", host))
	notice("[Connection]")
	notice(fmt.Sprintf("  host=%s  port=%d  mysqlx_port=%d", host, layout.Port, layout.MysqlXPort))
	notice("  login=root")
	notice(fmt.Sprintf("  password=%s", commonmysql.DisplayRootPassword(ctx)))
	notice(fmt.Sprintf("  connect_example=mysql -h %s -P %d -uroot -p", host, layout.Port))

	if out, err := commonsql.QueryMysqlSQL(ctx, layout, password, "SELECT VERSION();"); err == nil {
		if ver := commonmysql.ParseMysqlVersionOutput(out); ver != "" {
			notice(fmt.Sprintf("  version=%s", ver))
		}
	}

	notice("[Paths]")
	notice(fmt.Sprintf("  mysql_base=%s", layout.Base))
	if layout.Home != "" {
		notice(fmt.Sprintf("  mysql_home=%s", layout.Home))
	}
	if layout.Data != "" {
		notice(fmt.Sprintf("  mysql_data=%s", layout.Data))
	}
	if layout.Other != "" {
		notice(fmt.Sprintf("  mysql_other=%s", layout.Other))
	}
	if cnf, ok := ctx.Results["mysql_cnf_path"].(string); ok && strings.TrimSpace(cnf) != "" {
		notice(fmt.Sprintf("  config=%s", cnf))
	} else if layout.Other != "" {
		notice(fmt.Sprintf("  config=%s/%s", layout.Other, commonmysql.ConfigFileName(platform)))
	}
	if envPath := mysqlEnvFileDisplay(ctx, layout); envPath != "" {
		notice(fmt.Sprintf("  env_file=%s", envPath))
	}

	notice("[Service]")
	if platform == PlatformWindows {
		notice(fmt.Sprintf("  service=%s", commonmysql.ServiceNameForPort(platform, layout.Port)))
	} else if ctx.GetParamBool("mysql_skip_systemd", false) {
		notice("  service=(systemd skipped; manual mysqld_safe)")
	} else if unit, ok := ctx.Results["mysql_systemd_unit"].(string); ok && unit != "" {
		notice(fmt.Sprintf("  service=%s", unit))
	} else {
		notice(fmt.Sprintf("  service=%s", commonmysql.ServiceNameForPort(platform, layout.Port)))
	}

	notice(fmt.Sprintf("  stage=%s", stage))
	notice("======== end instance summary ========")
	return nil
}

func mysqlEnvFileDisplay(ctx *runner.StepContext, layout Layout) string {
	if ctx.GetTargetPlatform() == PlatformWindows {
		return fmt.Sprintf("%s/%d.bat", layout.Other, layout.Port)
	}
	raw := strings.TrimSpace(ctx.GetParamString("mysql_env_file", ""))
	if raw == "" {
		return fmt.Sprintf("~/.%d", layout.Port)
	}
	return raw
}
