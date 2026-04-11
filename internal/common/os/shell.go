package os

import "strings"

// ShellSingleQuote 将字符串安全地包裹在 shell 单引号内。
// 单引号内 shell 不做任何展开（$、\、`、! 等均保持原样），
// 唯一需要处理的是字符串自身的单引号：先关闭当前单引号，
// 插入转义单引号 \'，再重新开启单引号。
//
// 例如：abc'def  =>  'abc'\''def'
func ShellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// YasqlQuotePassword 将密码安全地格式化为 yasql 连接串中的密码部分。
// 格式：'"password"'
//   - 外层单引号防止 shell 展开（$、!、空格等）
//   - 内层双引号告诉 yasql 密码是一个整体（处理 /、@、空格等 yasql 分隔符）
//   - 密码中的单引号用 '\'' 转义，双引号用 \" 转义
//
// 例如：P@ss'w"d  =>  '"P@ss'\''w\"d"'
func YasqlQuotePassword(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "'", `'\''`)
	return `'"` + s + `"'`
}

// ShellEscapeForSuC 转义字符串使其可安全嵌入 su - user -c '...' 的双层单引号中。
// 最外层 su -c 已有一对单引号，内层参数再需要单引号时使用此函数。
// 结果形如 '\''value'\'' ，可直接拼入外层 su -c '...' 命令的 -p 参数等。
func ShellEscapeForSuC(s string) string {
	return "'\\''" + strings.ReplaceAll(s, "'", "'\\''") + "'\\''"
}

// YasbootAppendExtraArgs 将用户自定义的 yasboot 附加参数拼到命令末尾（空格分隔）。
// escapeInnerSingleQuotesForSu 为 true 时，将 extra 中的单引号转义为 '\''，用于整条子命令被包在
// su - user -c '...' 的单引号对内（如 C-011 gen-config）。为 false 时原样追加，适用于整条命令随后会由
// ExecuteAsUserWithEnv 等对整段命令做引号转义的场景。
func YasbootAppendExtraArgs(base, extra string, escapeInnerSingleQuotesForSu bool) string {
	e := strings.TrimSpace(extra)
	if e == "" {
		return base
	}
	if escapeInnerSingleQuotesForSu {
		e = strings.ReplaceAll(e, `'`, `'\''`)
	}
	return base + " " + e
}
