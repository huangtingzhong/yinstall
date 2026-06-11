package mysql

import (
	"fmt"

	"github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

// Layout is an alias for common layout type.
type Layout = commonmysql.Layout

// ResolveLayout delegates to common/mysql.
var ResolveLayout = commonmysql.ResolveLayout

func layoutFromCtx(ctx *runner.StepContext) (Layout, error) {
	if v, ok := ctx.Results["mysql_layout"].(Layout); ok {
		return v, nil
	}
	params := make(map[string]interface{}, len(ctx.Params)+2)
	for k, v := range ctx.Params {
		params[k] = v
	}
	if params["target_platform"] == nil || params["target_platform"] == "" {
		params["target_platform"] = ctx.GetTargetPlatform()
	}
	version, _ := params["mysql_version"].(string)
	if version == "" {
		if v := ctx.GetParamString("mysql_version", ""); v != "" {
			version = v
		} else if pkg := ctx.GetParamString("mysql_package", ""); pkg != "" {
			var err error
			version, err = file.ParseMysqlVersionFromPackage(pkg)
			if err != nil {
				return Layout{}, err
			}
		}
	}
	if version == "" {
		return Layout{}, fmt.Errorf("mysql_version not set (run M-004 or pass --mysql-package)")
	}
	params["mysql_version"] = version
	return ResolveLayout(params)
}
