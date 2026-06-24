package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS005PrepareDirectories() *runner.Step {
	return &runner.Step{
		ID:   "MS-005",
		Name: "Prepare Directories",
		Tags: []string{"mssql", "mssql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout := commonmssql.ResolveLayoutFromContext(ctx)
			if err := commonmssql.ValidateProgramLayoutMajor(layout); err != nil {
				return err
			}
			for _, d := range layout.InstanceDirs() {
				script := fmt.Sprintf(`New-Item -ItemType Directory -Force -Path '%s' | Out-Null`, strings.ReplaceAll(d, "'", "''"))
				ctx.LogScriptPreview("powershell", "MS-005 mkdir", script)
				if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false); err != nil {
					return err
				}
			}
			ctx.SetResult("mssql_layout", layout)
			return nil
		},
	}
}
