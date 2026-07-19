package db

import (
	"fmt"
	"regexp"
	"strings"
)

// SystemParam 表示一条待写入 SPFILE 的 ALTER SYSTEM SET 参数。
type SystemParam struct {
	Name  string
	Value string
}

var systemParamNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseSpfileParams 解析 --db-spfile-params：name=value|name=value，value 原样保留（含引号）。
func ParseSpfileParams(spec string) ([]SystemParam, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	var out []SystemParam
	for i, part := range strings.Split(spec, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("invalid spfile param segment %q (segment %d): expected name=value", part, i+1)
		}
		name := strings.TrimSpace(part[:eq])
		value := strings.TrimSpace(part[eq+1:])
		if name == "" {
			return nil, fmt.Errorf("invalid spfile param segment %q (segment %d): empty parameter name", part, i+1)
		}
		if !systemParamNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid spfile parameter name %q (segment %d): must match [A-Za-z_][A-Za-z0-9_]*", name, i+1)
		}
		out = append(out, SystemParam{Name: name, Value: value})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no spfile parameters parsed from %q", spec)
	}
	return out, nil
}

// BuildAlterSystemSpfileSQLs 生成 ALTER SYSTEM SET ... SCOPE=SPFILE 语句列表。
func BuildAlterSystemSpfileSQLs(params []SystemParam) []string {
	sqls := make([]string, 0, len(params))
	for _, p := range params {
		sqls = append(sqls, fmt.Sprintf("ALTER SYSTEM SET %s = %s SCOPE=SPFILE", p.Name, p.Value))
	}
	return sqls
}
