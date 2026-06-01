// debug_log.go - DB 安装步骤 debug phase 与 SQL 批量执行里程碑
package db

import (
	"fmt"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

func dbLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}

// dbRunSQLPhase 以 query-start/done/fail 里程碑执行单条 sysdba SQL。
func dbRunSQLPhase(ctx *runner.StepContext, osUser, envFile, cluster, label, sql string, showOutput bool) (*commonsql.YasqlResult, error) {
	dbLogPhase(ctx, "query-start", fmt.Sprintf("label=%s sql=%s", label, runner.TruncateForLog(sql, 80)))
	result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, osUser, envFile, cluster, sql, showOutput)
	if err != nil {
		dbLogPhase(ctx, "query-fail", fmt.Sprintf("label=%s err=%s", label, runner.TruncateForLog(err.Error(), 120)))
		return result, err
	}
	dbLogPhase(ctx, "query-done", fmt.Sprintf("label=%s exit=0", label))
	return result, nil
}

// dbRunSQLInstallLayoutPhase 在 install/data 布局下执行单条 sysdba SQL（C-023 等）。
func dbRunSQLInstallLayoutPhase(ctx *runner.StepContext, osUser, installPath, dataPath, label, sql string, showOutput bool) (*commonsql.YasqlResult, error) {
	dbLogPhase(ctx, "query-start", fmt.Sprintf("label=%s sql=%s", label, runner.TruncateForLog(sql, 80)))
	result, err := commonsql.ExecuteSQLAsSysdbaInstallLayoutCtx(ctx, osUser, installPath, dataPath, sql, showOutput)
	if err != nil {
		dbLogPhase(ctx, "query-fail", fmt.Sprintf("label=%s err=%s", label, runner.TruncateForLog(err.Error(), 120)))
		return result, err
	}
	dbLogPhase(ctx, "query-done", fmt.Sprintf("label=%s exit=0", label))
	return result, nil
}
