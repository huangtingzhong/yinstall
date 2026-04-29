package db

import (
	"fmt"
	"path"
	"strings"

	"github.com/yinstall/internal/runner"
)

// yashanCharacterSetCanonical：规范化键（大写、无连字符）→ 写入 yashandb.toml 的 CHARACTER_SET 精确取值（崖山支持的字符集）。
var yashanCharacterSetCanonical = map[string]string{
	"UTF8":    "UTF8",
	"GBK":     "GBK",
	"ASCII":   "ASCII",
	"GB18030": "GB18030",
	"BINARY":  "BINARY",
	"LATIN1":  "LATIN1",
	"UTF8MB3": "UTF8MB3",
	"UTF8MB4": "UTF8MB4",
}

const yashanCharacterSetList = "UTF8, GBK, ASCII, GB18030, BINARY, LATIN1, UTF8MB3, UTF8MB4"

// normalizeCharacterSetKey 去空白、转大写并去掉连字符，供查表（如 UTF-8 → UTF8）。
func normalizeCharacterSetKey(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, "-", "")
	return s
}

func canonicalYashanCharacterSet(raw string) (string, error) {
	key := normalizeCharacterSetKey(raw)
	if key == "" {
		return "", fmt.Errorf("character set is empty")
	}
	c, ok := yashanCharacterSetCanonical[key]
	if !ok {
		return "", fmt.Errorf("unsupported character set: %s (supported: %s)", strings.TrimSpace(raw), yashanCharacterSetList)
	}
	return c, nil
}

// StepC015SetCharacterSet 在集群 TOML 中设置字符集
func StepC015SetCharacterSet() *runner.Step {
	return &runner.Step{
		ID:          "C-015",
		Name:        "Set Character Set",
		Description: "Configure database character set",
		Tags:        []string{"db", "config"},
		// 非可选：非法字符集须在 PreCheck 失败并中止安装，不能当作“跳过”处理
		Optional: false,

		PreCheck: func(ctx *runner.StepContext) error {
			_, err := canonicalYashanCharacterSet(ctx.GetParamString("db_character_set", "utf8"))
			return err
		},

		Action: func(ctx *runner.StepContext) error {
			charset, err := canonicalYashanCharacterSet(ctx.GetParamString("db_character_set", "utf8"))
			if err != nil {
				return err
			}
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// 默认 UTF8：与 yasboot 生成的 CHARACTER_SET 一致，无需改文件
			if charset == "UTF8" {
				ctx.Logger.Info("Character set is default (UTF8), skipping modification")
				return nil
			}

			configPath := path.Join(stageDir, clusterName+".toml")

			ctx.Logger.Info("Setting character set to: %s", charset)

			// 修改集群配置中的 CHARACTER_SET
			cmd := fmt.Sprintf(`sed -i 's/CHARACTER_SET.*/CHARACTER_SET = "%s"/' %s`, charset, configPath)
			if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
				return fmt.Errorf("failed to set character set: %w", err)
			}

			// 复查配置行
			result, _ := ctx.Execute(fmt.Sprintf("grep CHARACTER_SET %s", configPath), false)
			if result != nil {
				ctx.Logger.Info("Config updated: %s", result.GetStdout())
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			charset, err := canonicalYashanCharacterSet(ctx.GetParamString("db_character_set", "utf8"))
			if err != nil {
				return err
			}
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			configPath := path.Join(stageDir, clusterName+".toml")

			result, _ := ctx.Execute(fmt.Sprintf("grep -i 'CHARACTER_SET.*%s' %s", charset, configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("Could not verify character set setting")
			}

			return nil
		},
	}
}
