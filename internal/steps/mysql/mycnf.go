package mysql

import (
	"bytes"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	commonmysql "github.com/yinstall/internal/common/mysql"
)

//go:embed templates/*.tmpl
var cnfTemplateFS embed.FS

type cnfTemplateData struct {
	MYSQL_PORT               string
	MYSQLX_PORT              string
	MYSQL_HOME               string
	MYSQL_DATA               string
	MYSQL_OTHER              string
	SERVER_ID                string
	INNODB_BUFFER_POOL_SIZE  string
	GTID_MODE                string
	ENFORCE_GTID_CONSISTENCY string
	INNODB_REDO_OR_LOG_LINES string
	VALIDATE_PASSWORD_PLUGIN string
	IsUnix                   bool
	IsLinux                  bool
	IsWindows                bool
}

// RenderMyCnf renders my.cnf/my.ini from mysql80.tmpl or mysql57.tmpl (paths via placeholders).
func RenderMyCnf(platform, version, explicitTemplate string, layout commonmysql.Layout, opts MyCnfOpts) (filename string, content string, err error) {
	tmplID := normalizeTemplateID(explicitTemplate, version)
	if tmplID == "" {
		tmplID = SelectTemplateID(version)
	}
	body, err := loadCnfTemplateBody(tmplID)
	if err != nil {
		return "", "", err
	}
	if opts.ServerID == 0 {
		opts.ServerID = 221011
	}
	if opts.InnodbBufferPool == "" {
		opts.InnodbBufferPool = "4G"
	}
	if opts.GTIDMode == "" {
		opts.GTIDMode = "on"
	}
	if opts.EnforceGTID == "" {
		opts.EnforceGTID = "on"
	}
	isWindows := platform == PlatformWindows
	isLinux := platform == PlatformLinux
	data := cnfTemplateData{
		MYSQL_PORT:               strconv.Itoa(layout.Port),
		MYSQLX_PORT:              strconv.Itoa(layout.MysqlXPort),
		MYSQL_HOME:               layout.Home,
		MYSQL_DATA:               layout.Data,
		MYSQL_OTHER:              layout.Other,
		SERVER_ID:                strconv.Itoa(opts.ServerID),
		INNODB_BUFFER_POOL_SIZE:  opts.InnodbBufferPool,
		GTID_MODE:                opts.GTIDMode,
		ENFORCE_GTID_CONSISTENCY: opts.EnforceGTID,
		INNODB_REDO_OR_LOG_LINES: innodbRedoLines(version),
		VALIDATE_PASSWORD_PLUGIN: validatePasswordPlugin(platform),
		IsUnix:                   !isWindows,
		IsLinux:                  isLinux,
		IsWindows:                isWindows,
	}
	if isWindows {
		filename = "my.ini"
	} else {
		filename = "my.cnf"
	}
	t, err := template.New(tmplID).Parse(body)
	if err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", "", err
	}
	out := strings.TrimSpace(buf.String()) + "\n"
	out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	return filename, out, nil
}

func loadCnfTemplateBody(id string) (string, error) {
	path := "templates/" + id + ".tmpl"
	b, err := cnfTemplateFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unknown mysql cnf template %q: %w", id, err)
	}
	return string(b), nil
}

// MyCnfOpts holds render overrides.
type MyCnfOpts struct {
	ServerID         int
	InnodbBufferPool string
	GTIDMode         string
	EnforceGTID      string
}

// SelectTemplateID picks template by MySQL major version only (platform handled in template).
func SelectTemplateID(version string) string {
	if isMySQL57(version) {
		return "mysql57"
	}
	return "mysql80"
}

func normalizeTemplateID(explicit, version string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit == "" {
		return ""
	}
	switch explicit {
	case "mysql80", "mysql57":
		return explicit
	case "standard80", "standard80_linux", "standard80_darwin", "win80":
		return "mysql80"
	case "legacy57", "legacy57_linux", "legacy57_darwin", "legacy57_win":
		return "mysql57"
	default:
		return explicit
	}
}

func isMySQL57(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	if strings.HasPrefix(version, "5.7") {
		return true
	}
	major := version
	if i := strings.Index(version, "."); i > 0 {
		major = version[:i]
	}
	return major == "5"
}

func validatePasswordPlugin(platform string) string {
	if platform == PlatformWindows {
		return "validate_password.dll"
	}
	return "validate_password.so"
}

func innodbRedoLines(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 3 {
		minor, _ := strconv.Atoi(parts[2])
		if len(parts) >= 2 {
			maj, _ := strconv.Atoi(parts[0])
			min, _ := strconv.Atoi(parts[1])
			if maj == 8 && min == 0 && minor > 0 && minor < 30 {
				return "innodb_log_file_size=1024M\ninnodb_log_files_in_group=4"
			}
		}
	}
	return "innodb_redo_log_capacity=10240M"
}
