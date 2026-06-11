package mysql

import (
	"fmt"

	"github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepM010GenerateMyCnf renders my.cnf/my.ini into MYSQL_OTHER.
func StepM010GenerateMyCnf() *runner.Step {
	return &runner.Step{
		ID:          "M-010",
		Name:        "Generate my.cnf",
		Description: "Render my.cnf or my.ini from template",
		Tags:        []string{"mysql", "config", "mysql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			opts := MyCnfOpts{
				ServerID:         ctx.GetParamInt("mysql_server_id", 0),
				InnodbBufferPool: ctx.GetParamString("mysql_innodb_buffer_pool_size", "4G"),
				GTIDMode:         ctx.GetParamString("mysql_gtid_mode", "on"),
				EnforceGTID:      ctx.GetParamString("mysql_enforce_gtid_consistency", "on"),
			}
			if opts.ServerID == 0 {
				opts.ServerID = 221011
			}
			_, content, err := RenderMyCnf(ctx.GetTargetPlatform(), layout.Version,
				ctx.GetParamString("mysql_cnf_template", ""), layout, opts)
			if err != nil {
				return err
			}
			mysqlLogPhase(ctx, "plan", "M-010 my.cnf")
			cfgName := "my.cnf"
			if ctx.GetTargetPlatform() == PlatformWindows {
				cfgName = "my.ini"
			}
			cfgPath := layout.Other + "/" + cfgName
			ctx.LogScriptPreview("file", "my.cnf", content)
			if ctx.GetTargetPlatform() == PlatformWindows {
				err = file.RemoteWriteTextFile(ctx, cfgPath, content, false)
				if err == nil {
					ctx.SetResult("mysql_cnf_path", cfgPath)
				}
				return err
			}
			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", commonos.ShellSingleQuote(cfgPath), content)
			_, err = ctx.ExecuteWithCheck(cmd, UseSudo(ctx))
			ctx.SetResult("mysql_cnf_path", cfgPath)
			return err
		},
	}
}
