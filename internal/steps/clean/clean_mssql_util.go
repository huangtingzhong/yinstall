package clean

import (
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func mssqlCleanLayout(ctx *runner.StepContext) commonmssql.Layout {
	layout := commonmssql.ResolveLayoutFromContext(ctx)
	enriched, err := commonmssql.EnrichCleanLayoutFromRegistry(ctx, layout)
	if err != nil {
		return layout
	}
	return enriched
}

func mssqlCleanStageFromCtx(ctx *runner.StepContext) string {
	stage, err := commonmssql.ParseStage(ctx.GetParamString("mssql_stage", commonmssql.DefaultCleanStage()))
	if err != nil {
		return commonmssql.StageAll
	}
	return commonmssql.NormalizeCleanStage(stage)
}

func mssqlServiceName(instance string) string {
	if strings.EqualFold(strings.TrimSpace(instance), commonmssql.DefaultInstance) {
		return "MSSQLSERVER"
	}
	return "MSSQL$" + strings.TrimSpace(instance)
}

func mssqlAgentServiceName(instance string) string {
	if strings.EqualFold(strings.TrimSpace(instance), commonmssql.DefaultInstance) {
		return "SQLSERVERAGENT"
	}
	return "SQLAgent$" + strings.TrimSpace(instance)
}
