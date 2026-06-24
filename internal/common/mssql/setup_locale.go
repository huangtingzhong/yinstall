package mssql

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yinstall/internal/runner"
)

const setupExitLanguageMismatch = 2227437582 // 0x84C4000E / int32 -2067529714

var setupMediaLangPrefixRE = regexp.MustCompile(`^([a-z]{2})_`)

// SetupMediaLanguage describes SQL Server setup media locale inferred from filename.
type SetupMediaLanguage struct {
	Tag       string // BCP-47 tag, e.g. zh-CN; empty when unknown
	Universal bool   // English media works on any OS locale
	Label     string
}

// DetectSetupMediaLanguage infers media language from ISO/dir filename.
// Unknown names skip the locale gate (Universal=false, Tag="").
func DetectSetupMediaLanguage(name string) SetupMediaLanguage {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	if base == "" {
		return SetupMediaLanguage{Label: "unknown"}
	}
	if isEnglishSetupMediaName(base) {
		return SetupMediaLanguage{Tag: "en-US", Universal: true, Label: "English"}
	}
	if m := setupMediaLangPrefixRE.FindStringSubmatch(base); len(m) == 2 {
		if tag, label := localeFromMediaPrefix(m[1]); tag != "" {
			return SetupMediaLanguage{Tag: tag, Label: label}
		}
	}
	return SetupMediaLanguage{Label: "unknown"}
}

func isEnglishSetupMediaName(base string) bool {
	if strings.HasPrefix(base, "en_") {
		return true
	}
	return strings.Contains(base, "enu")
}

func localeFromMediaPrefix(prefix string) (tag, label string) {
	switch prefix {
	case "cn", "zh":
		return "zh-CN", "简体中文"
	case "tw":
		return "zh-TW", "繁体中文"
	case "de":
		return "de-DE", "Deutsch"
	case "fr":
		return "fr-FR", "Français"
	case "ja":
		return "ja-JP", "日本語"
	case "ko":
		return "ko-KR", "한국어"
	case "ru":
		return "ru-RU", "Русский"
	case "es":
		return "es-ES", "Español"
	case "pt":
		return "pt-BR", "Português"
	case "it":
		return "it-IT", "Italiano"
	default:
		return "", ""
	}
}

func setupMediaLocaleSource(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	if p := strings.TrimSpace(ctx.GetParamString("mssql_setup_package", "")); p != "" {
		return filepath.Base(p)
	}
	if v, ok := ctx.Results["mssql_setup_remote_path"].(string); ok && strings.TrimSpace(v) != "" {
		return filepath.Base(v)
	}
	if v, ok := ctx.Results["mssql_setup_local_path"].(string); ok && strings.TrimSpace(v) != "" {
		return filepath.Base(v)
	}
	return ""
}

func probeSystemLocale(ctx *runner.StepContext) (string, error) {
	res, err := ctx.Execute(`powershell -NoProfile -Command "(Get-WinSystemLocale).Name"`, false)
	if err != nil {
		return "", err
	}
	locale := strings.TrimSpace(firstOutputLine(res.GetStdout()))
	if locale == "" {
		return "", fmt.Errorf("empty system locale")
	}
	return locale, nil
}

func localeMatchesMedia(media SetupMediaLanguage, osLocale string) bool {
	if media.Universal || media.Tag == "" {
		return true
	}
	osLocale = strings.ToLower(strings.TrimSpace(osLocale))
	mediaTag := strings.ToLower(media.Tag)
	if osLocale == "" {
		return true
	}
	if strings.HasPrefix(mediaTag, "zh-") {
		return strings.HasPrefix(osLocale, "zh")
	}
	mediaPrimary := strings.SplitN(mediaTag, "-", 2)[0]
	osPrimary := strings.SplitN(osLocale, "-", 2)[0]
	return mediaPrimary == osPrimary
}

func formatLocaleMismatchError(media SetupMediaLanguage, osLocale, source string) string {
	return fmt.Sprintf(
		"SQL Server 安装介质语言 (%s, 来自 %q) 与目标机 Windows 系统区域 (%s) 不匹配；"+
			"setup.exe 将失败 (exit 0x84C4000E)。请改用英文 ISO (文件名含 en_ 或 ENU)，"+
			"或将目标机「区域 → 管理 → 非 Unicode 程序的语言」改为与介质一致并重启，"+
			"或使用 --mssql-ignore-locale-check 跳过预检",
		media.Label, source, osLocale,
	)
}

// EnsureSetupLocaleCompatible verifies setup media language matches target OS locale.
func EnsureSetupLocaleCompatible(ctx *runner.StepContext) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if ctx.GetParamBool("mssql_ignore_locale_check", false) {
		return nil
	}
	source := setupMediaLocaleSource(ctx)
	if source == "" {
		return nil
	}
	media := DetectSetupMediaLanguage(source)
	if media.Universal || media.Tag == "" {
		return nil
	}
	osLocale, err := probeSystemLocale(ctx)
	if err != nil {
		if ctx.Logger != nil {
			ctx.Logger.Warn("locale check: cannot read OS system locale (%v); skipping", err)
		}
		return nil
	}
	if localeMatchesMedia(media, osLocale) {
		return nil
	}
	return fmt.Errorf("%s", formatLocaleMismatchError(media, osLocale, source))
}

// SetupExitCodeError returns a user-facing error for known setup.exe exit codes.
func SetupExitCodeError(code int) string {
	if isSetupLanguageMismatchExit(code) {
		return "SQL Server 安装介质语言与目标机 OS 区域不匹配 (setup exit 0x84C4000E)；" +
			"请使用英文 ISO，或将 Windows 系统区域改为与介质一致，或使用 --mssql-ignore-locale-check 跳过预检"
	}
	norm := normalizeSetupExitCode(code)
	return fmt.Sprintf("setup.exe exit code %d", norm)
}

func isSetupLanguageMismatchExit(code int) bool {
	u := uint32(code)
	signed := int32(u)
	if signed == -2067529714 {
		return true
	}
	if code == setupExitLanguageMismatch {
		return true
	}
	return u == uint32(setupExitLanguageMismatch)
}

// SetupMediaPackageName returns the setup package filename used for locale inference.
func SetupMediaPackageName(ctx *runner.StepContext) string {
	return setupMediaLocaleSource(ctx)
}
