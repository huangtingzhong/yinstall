package db

import (
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

const (
	c031SQLProfile           = "ALTER PROFILE default LIMIT failed_login_attempts UNLIMITED"
	c031SQLDateFormat        = "ALTER SYSTEM SET date_format='yyyy-mm-dd hh24:mi:ss' SCOPE=SPFILE"
	c031ResultProfileSkipped = "c031_profile_skipped"
)

// isC031ProfileAlterSkippable 判断 ALTER PROFILE 是否因 MASTER 容器无法修改 local object 而可跳过。
func isC031ProfileAlterSkippable(err error, res *commonsql.YasqlResult) bool {
	var b strings.Builder
	if err != nil {
		b.WriteString(err.Error())
	}
	if res != nil {
		b.WriteString(res.Stdout)
		b.WriteString(res.Stderr)
	}
	u := strings.ToUpper(b.String())
	if strings.Contains(u, "YAS-02887") &&
		(strings.Contains(u, "2937") || strings.Contains(u, "LOCAL OBJECT")) {
		return true
	}
	if strings.Contains(u, "ERRCODE: 2937") ||
		strings.Contains(u, "CANNOT ALTER/DROP LOCAL OBJECT") {
		return true
	}
	return false
}

func shouldSkipC031ProfilePostCheck(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	if StepContextHasEnableBranch(ctx) {
		return true
	}
	if v, ok := ctx.Results[c031ResultProfileSkipped].(bool); ok && v {
		return true
	}
	return false
}
