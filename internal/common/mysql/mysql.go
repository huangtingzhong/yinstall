package mysql

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	PlatformLinux   = "linux"
	PlatformDarwin  = "darwin"
	PlatformWindows = "windows"

	DefaultBaseLinux   = "/mysql/app/mysql"
	DefaultBaseWindows = "D:/mysql/app/mysql"

	DefaultRemoteSoftwareDirLinux   = "/mysql/app/soft"
	DefaultRemoteSoftwareDirWindows = "D:/mysql/app/soft"
)

// DefaultBase returns the default MYSQL_BASE for the target platform.
func DefaultBase(platform string) string {
	if platform == PlatformWindows {
		return DefaultBaseWindows
	}
	return DefaultBaseLinux
}

// DefaultRemoteSoftwareDir returns the default remote software directory for the target platform.
func DefaultRemoteSoftwareDir(platform string) string {
	if platform == PlatformWindows {
		return DefaultRemoteSoftwareDirWindows
	}
	return DefaultRemoteSoftwareDirLinux
}

// Layout holds resolved MySQL directory paths.
type Layout struct {
	Version    string
	Port       int
	Base       string
	Home       string
	Data       string
	Other      string
	MysqlXPort int
}

// ResolveLayout computes MYSQL_HOME/DATA/OTHER from params.
func ResolveLayout(params map[string]interface{}) (Layout, error) {
	version := paramString(params, "mysql_version", "")
	port := paramInt(params, "mysql_port", 3306)
	platform := paramString(params, "target_platform", PlatformLinux)
	base := paramString(params, "mysql_base", DefaultBase(platform))
	portStr := strconv.Itoa(port)
	data := fmt.Sprintf("%s/oradata/%s/data", base, portStr)
	other := fmt.Sprintf("%s/oradata/%s/other", base, portStr)
	home := ""
	if h := paramString(params, "mysql_home", ""); h != "" {
		home = strings.TrimRight(strings.ReplaceAll(h, `\`, `/`), "/")
	} else if version != "" {
		home = fmt.Sprintf("%s/product/%s/dbhome_1", base, version)
	}
	return Layout{
		Version:    version,
		Port:       port,
		Base:       base,
		Home:       home,
		Data:       data,
		Other:      other,
		MysqlXPort: mysqlXPortFromPort(port),
	}, nil
}

func mysqlXPortFromPort(port int) int {
	n, _ := strconv.Atoi(fmt.Sprintf("%d0", port))
	return n
}

func paramString(params map[string]interface{}, key, def string) string {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func paramInt(params map[string]interface{}, key string, def int) int {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		if n, err := strconv.Atoi(t); err == nil {
			return n
		}
	}
	return def
}
