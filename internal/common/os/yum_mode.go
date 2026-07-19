package os

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// YumModeLocal 使用 auto 介质（光驱或 ISO）+ local repo 安装。
	YumModeLocal = "local"
	// YumModeHTTP 使用自定义 HTTP(S) yum 源（由 --os-yum-mode 的 IP/URL 识别）。
	YumModeHTTP = "http"

	// DefaultHTTPYumRepoFile HTTP 模式默认 repo 文件（避免覆盖 ISO 的 local.repo）。
	DefaultHTTPYumRepoFile = "/etc/yum.repos.d/yinstall-http.repo"
	// defaultLocalYumRepoFile 与 CLI --os-yum-repo-file 默认值一致。
	defaultLocalYumRepoFile = "/etc/yum.repos.d/local.repo"
	// defaultYumHTTPPathRoot 仅 IP/host 时的默认路径前缀（对齐 yum.sh / DVD：根下 BaseOS+AppStream）。
	defaultYumHTTPPathRoot = "/"

	yumHTTPRepoBaseOS    = "yinstall-baseos"
	yumHTTPRepoAppStream = "yinstall-appstream"
	yumHTTPRepoSingle    = "yinstall"
)

// YumHTTPEndpoint 自定义 yum HTTP(S) 源。
type YumHTTPEndpoint struct {
	Scheme   string // http | https
	Host     string
	Port     string // 可空
	PathRoot string // 如 /repo/OracleLinux 或用户 URL 中的 path
}

var reYumHostname = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// NormalizeYumMode 归一化 CLI 取值：空/none/online → 系统源；local → local；IP/URL → http。
func NormalizeYumMode(mode string) string {
	kind, _, err := ParseYumMode(mode)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(mode))
	}
	return kind
}

// ParseYumMode 解析 --os-yum-mode：返回 kind（""|local|http）与可选 HTTP 端点。
func ParseYumMode(mode string) (kind string, ep *YumHTTPEndpoint, err error) {
	raw := strings.TrimSpace(mode)
	if raw == "" {
		return "", nil, nil
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "none", "online":
		return "", nil, nil
	case "local", "local-iso":
		return YumModeLocal, nil, nil
	case YumModeHTTP, "https":
		return "", nil, fmt.Errorf("os-yum-mode %q needs a host (e.g. 10.10.10.20 or http://10.10.10.20)", raw)
	}

	ep, perr := parseYumHTTPEndpoint(raw)
	if perr == nil {
		return YumModeHTTP, ep, nil
	}
	return "", nil, fmt.Errorf("invalid os-yum-mode %q: use empty, local, IP[:port], or http(s)://host[:port][/path] (%v)", raw, perr)
}

// GetYumModeRaw 返回未归一化的 os_yum_mode 原始串（保留 IP/URL）。
func GetYumModeRaw(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.GetParamString("os_yum_mode", ""))
}

// GetYumMode 从 StepContext 读取并归一化 os_yum_mode（""|local|http）。
func GetYumMode(ctx *runner.StepContext) string {
	return NormalizeYumMode(GetYumModeRaw(ctx))
}

// GetYumHTTPEndpoint 在 http 模式下返回端点；其它模式返回 nil。
func GetYumHTTPEndpoint(ctx *runner.StepContext) *YumHTTPEndpoint {
	kind, ep, err := ParseYumMode(GetYumModeRaw(ctx))
	if err != nil || kind != YumModeHTTP {
		return nil
	}
	return ep
}

// IsLocalYumMode 是否显式 local 模式（始终 auto 介质）。
func IsLocalYumMode(mode string) bool {
	return NormalizeYumMode(mode) == YumModeLocal
}

// IsHTTPYumMode 是否自定义 HTTP(S) yum 源。
func IsHTTPYumMode(mode string) bool {
	return NormalizeYumMode(mode) == YumModeHTTP
}

// ValidateYumMode 校验 os_yum_mode；非法时返回 error。
func ValidateYumMode(mode string) error {
	_, _, err := ParseYumMode(mode)
	return err
}

func parseYumHTTPEndpoint(raw string) (*YumHTTPEndpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty")
	}

	// 完整 URL
	if strings.Contains(raw, "://") {
		return parseYumHTTPURL(raw)
	}

	// host/path 简写 → http://host/path
	if i := strings.Index(raw, "/"); i > 0 {
		return parseYumHTTPURL("http://" + raw)
	}

	host, port, err := splitYumHostPort(raw)
	if err != nil {
		return nil, err
	}
	if err := validateYumHost(host); err != nil {
		return nil, err
	}
	return &YumHTTPEndpoint{
		Scheme:   "http",
		Host:     host,
		Port:     port,
		PathRoot: defaultYumHTTPPathRoot,
	}, nil
}

func parseYumHTTPURL(raw string) (*YumHTTPEndpoint, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if err := validateYumHost(host); err != nil {
		return nil, err
	}
	port := u.Port()
	pathRoot := u.Path
	if pathRoot == "" || pathRoot == "/" {
		pathRoot = defaultYumHTTPPathRoot
	}
	return &YumHTTPEndpoint{
		Scheme:   scheme,
		Host:     host,
		Port:     port,
		PathRoot: pathRoot,
	}, nil
}

func splitYumHostPort(raw string) (host, port string, err error) {
	// [IPv6]:port 或 [IPv6]
	if strings.HasPrefix(raw, "[") {
		h, p, e := net.SplitHostPort(raw)
		if e == nil {
			return h, p, nil
		}
		if strings.HasSuffix(raw, "]") {
			return strings.TrimPrefix(strings.TrimSuffix(raw, "]"), "["), "", nil
		}
		return "", "", fmt.Errorf("invalid IPv6 host %q", raw)
	}
	// host:port（避免把 IPv4 误判：SplitHostPort 对 a.b.c.d:port 可用）
	if strings.Count(raw, ":") == 1 {
		h, p, e := net.SplitHostPort(raw)
		if e == nil {
			return h, p, nil
		}
	}
	return raw, "", nil
}

func validateYumHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if reYumHostname.MatchString(host) {
		return nil
	}
	return fmt.Errorf("invalid host %q", host)
}

func (e *YumHTTPEndpoint) hostPort() string {
	if e == nil {
		return ""
	}
	if e.Port == "" {
		if strings.Contains(e.Host, ":") {
			return "[" + e.Host + "]"
		}
		return e.Host
	}
	return net.JoinHostPort(e.Host, e.Port)
}

// RootURL 返回 scheme://host[:port][/pathRoot]（无尾斜杠）。
func (e *YumHTTPEndpoint) RootURL() string {
	if e == nil {
		return ""
	}
	root := e.Scheme + "://" + e.hostPort()
	p := strings.TrimSpace(e.PathRoot)
	if p == "" || p == "/" {
		return root
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return root + strings.TrimSuffix(p, "/")
}

// BaseURLs 按目标 OS 生成 yum baseurl。
// EL8+：DVD/yum.sh 布局 <root>/BaseOS/ 与 <root>/AppStream/；EL7：<root>/。
func (e *YumHTTPEndpoint) BaseURLs(osInfo *runner.OSInfo) (baseosURL, appstreamURL string, singleURL string) {
	root := e.RootURL()
	if IsRHEL8(osInfo) || (!IsRHEL7(osInfo) && ISOProfileFromOSInfo(osInfo).MajorVer >= 8) {
		baseosURL = root + "/BaseOS/"
		appstreamURL = root + "/AppStream/"
		return baseosURL, appstreamURL, ""
	}
	singleURL = root + "/"
	return "", "", singleURL
}

// BuildYinstallHTTPRepoContent 按 OS 版本生成 yinstall-http.repo 内容。
func BuildYinstallHTTPRepoContent(osInfo *runner.OSInfo, ep *YumHTTPEndpoint) (string, error) {
	if ep == nil {
		return "", fmt.Errorf("nil HTTP yum endpoint")
	}
	baseos, appstream, single := ep.BaseURLs(osInfo)
	if single != "" {
		return fmt.Sprintf(
			"[%s]\nname=yinstall HTTP\nbaseurl=%s\nenabled=1\ngpgcheck=0\n",
			yumHTTPRepoSingle, single,
		), nil
	}
	return fmt.Sprintf(
		"[%s]\nname=yinstall HTTP BaseOS\nbaseurl=%s\nenabled=1\ngpgcheck=0\n\n"+
			"[%s]\nname=yinstall HTTP AppStream\nbaseurl=%s\nenabled=1\ngpgcheck=0\n",
		yumHTTPRepoBaseOS, baseos, yumHTTPRepoAppStream, appstream,
	), nil
}

// ResolveHTTPYumRepoFile HTTP 模式 repo 路径：默认 yinstall-http.repo，显式非 local.repo 时尊重用户。
func ResolveHTTPYumRepoFile(ctx *runner.StepContext) string {
	f := defaultLocalYumRepoFile
	if ctx != nil {
		f = ctx.GetParamString("os_yum_repo_file", defaultLocalYumRepoFile)
	}
	f = strings.TrimSpace(f)
	if f == "" || f == defaultLocalYumRepoFile {
		return DefaultHTTPYumRepoFile
	}
	return f
}

// PrepareHTTPRepo 按 OS 版本写入自定义 HTTP yum repo 文件。
func PrepareHTTPRepo(ctx *runner.StepContext) error {
	ensureOSInfo(ctx)
	ep := GetYumHTTPEndpoint(ctx)
	if ep == nil {
		return fmt.Errorf("os-yum-mode is not a valid HTTP yum endpoint")
	}
	content, err := BuildYinstallHTTPRepoContent(ctx.OSInfo, ep)
	if err != nil {
		return err
	}
	repoFile := ResolveHTTPYumRepoFile(ctx)
	ctx.LogPhase("http-repo-start", fmt.Sprintf(
		"repo_file=%s root=%s",
		repoFile, ep.RootURL(),
	))
	if err := writeYumRepoFile(ctx, repoFile, content); err != nil {
		ctx.LogPhase("http-repo-fail", err.Error())
		return err
	}
	ctx.LogPhase("http-repo-done", fmt.Sprintf("repo_file=%s", repoFile))
	return nil
}

func writeYumRepoFile(ctx *runner.StepContext, repoFile, content string) error {
	ctx.Execute(fmt.Sprintf("mkdir -p %s", path.Dir(repoFile)), true)
	escaped := strings.ReplaceAll(content, "'", `'\''`)
	cmd := fmt.Sprintf("printf '%%s' '%s' > %s", escaped, repoFile)
	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
		return fmt.Errorf("failed to write repo file %s: %w", repoFile, err)
	}
	return nil
}

// IsRepoClassInstallError 判断安装失败是否可能由 yum/dnf 源引起（可触发 auto 介质 fallback）。
func IsRepoClassInstallError(stderr string, exitCode int) bool {
	if exitCode == 0 {
		return false
	}
	s := strings.ToLower(stderr)
	keywords := []string{
		"repomd.xml",
		"failed to download metadata",
		"cannot download repodata",
		"cannot download repomd",
		"no enabled repositories",
		"there are no enabled repositories",
		"all mirrors were tried",
		"curl error (37)",
		"couldn't open file",
		"error: unable to find a match",
		"no package",
	}
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}
