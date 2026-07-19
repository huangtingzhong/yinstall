package os

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// ISODeviceAuto 表示按目标 OS 自动选择 ISO（先探测光驱，再搜索软件目录）。
	ISODeviceAuto = "auto"
)

// defaultBlockDevices 自动模式下优先探测的光驱/块设备。
var defaultBlockDevices = []string{"/dev/cdrom", "/dev/sr0"}

// ISOProfile 描述目标机期望的 ISO 特征（一期：RHEL 系）。
type ISOProfile struct {
	Family   string // rhel7 | rhel8
	DistroID string // ol, rhel, centos, rocky, kylin, ...
	MajorVer int
	MinorVer int // -1 表示未知或未指定
	Arch     string
}

// ISOMetadata 从已挂载 ISO 读取的元数据。
type ISOMetadata struct {
	Version string
	Major   int
	Minor   int // -1 未知
	Arch    string
	Family  string // rhel7 | rhel8
	Source  string // treeinfo | discinfo | media.repo
}

// ISOProbeMountpoint 临时探测挂载点（与正式挂载点分离）。
const ISOProbeMountpoint = "/tmp/yinstall_iso_probe"

// IsAutoISODevice 判断是否为自动 ISO 选择模式。
func IsAutoISODevice(device string) bool {
	d := strings.TrimSpace(strings.ToLower(device))
	return d == "" || d == ISODeviceAuto
}

// DefaultBlockDevices 返回自动模式下探测的块设备列表。
func DefaultBlockDevices() []string {
	out := make([]string, len(defaultBlockDevices))
	copy(out, defaultBlockDevices)
	return out
}

// ISOProfileFromOSInfo 根据 B-000 检测结果构建 ISO 匹配 profile。
func ISOProfileFromOSInfo(osInfo *runner.OSInfo) ISOProfile {
	p := ISOProfile{MinorVer: -1}
	if osInfo == nil {
		p.Arch = "x86_64"
		p.Family = "rhel8"
		return p
	}

	p.DistroID = strings.ToLower(strings.TrimSpace(osInfo.ID))
	p.Arch = NormalizeArch(osInfo.Arch)
	p.MajorVer, p.MinorVer = parseVersionID(osInfo.VersionID)

	switch {
	case IsRHEL7(osInfo):
		p.Family = "rhel7"
	case IsRHEL8(osInfo):
		p.Family = "rhel8"
	default:
		// 未识别发行版时按 RHEL8 布局处理（Kylin/UOS 等已在 DetectOSType 标记）
		if p.MajorVer == 7 {
			p.Family = "rhel7"
		} else {
			p.Family = "rhel8"
		}
	}
	return p
}

// NormalizeArch 归一化 CPU 架构名。
func NormalizeArch(arch string) string {
	a := strings.ToLower(strings.TrimSpace(arch))
	switch a {
	case "aarch64", "arm64":
		return "aarch64"
	case "x86_64", "amd64", "x64":
		return "x86_64"
	case "":
		return "x86_64"
	default:
		return a
	}
}

func parseVersionID(versionID string) (major, minor int) {
	major, minor = -1, -1
	v := strings.TrimSpace(versionID)
	if v == "" {
		return major, minor
	}
	// Kylin V10、10.x 等
	v = strings.TrimPrefix(strings.ToUpper(v), "V")
	parts := strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
	if len(parts) == 0 {
		return major, minor
	}
	if n, err := strconv.Atoi(parts[0]); err == nil {
		major = n
	}
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			minor = n
		}
	}
	return major, minor
}

// ScoreISOFilename 对 ISO 文件名打分（越高越匹配）。
func ScoreISOFilename(name string, profile ISOProfile) int {
	base := strings.ToLower(filepathBase(name))
	if !strings.HasSuffix(base, ".iso") {
		return -10000
	}
	base = strings.TrimSuffix(base, ".iso")
	score := 0

	if s := scoreArchInName(base, profile.Arch); s < 0 {
		return s
	} else {
		score += s
	}

	score += scoreDistroKeywords(base, profile.DistroID)
	score += scoreMajorVersion(base, profile.MajorVer)
	score += scoreMinorVersion(base, profile.MinorVer)
	score += scoreISOVariant(base)

	for _, bad := range []string{"windows", "macos", "darwin", "src", "source"} {
		if strings.Contains(base, bad) {
			return -10000
		}
	}
	return score
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func scoreArchInName(name, wantArch string) int {
	conflict := map[string][]string{
		"aarch64": {"x86_64", "amd64", "x86-64", "i386", "i686"},
		"x86_64":  {"aarch64", "arm64", "ppc64le", "s390x"},
	}
	for _, tok := range conflict[wantArch] {
		if strings.Contains(name, tok) {
			return -1000
		}
	}
	for _, tok := range []string{wantArch, strings.ReplaceAll(wantArch, "_", "-")} {
		if tok != "" && strings.Contains(name, tok) {
			return 100
		}
	}
	if wantArch == "aarch64" && strings.Contains(name, "arm64") {
		return 100
	}
	if wantArch == "x86_64" && strings.Contains(name, "amd64") {
		return 100
	}
	return 0
}

func scoreDistroKeywords(name, distroID string) int {
	keywords := distroISOKeywords(distroID)
	best := 0
	for _, kw := range keywords {
		if kw != "" && strings.Contains(name, kw) {
			best = 50
			break
		}
	}
	if best == 0 {
		for _, kw := range []string{"rhel", "centos", "ol", "oracle", "rocky", "almalinux", "kylin", "uos"} {
			if strings.Contains(name, kw) {
				best = 20
				break
			}
		}
	}
	return best
}

func distroISOKeywords(distroID string) []string {
	switch strings.ToLower(distroID) {
	case "ol", "oracle":
		return []string{"oracle", "ol", "oraclelinux"}
	case "rhel":
		return []string{"rhel", "redhat"}
	case "centos":
		return []string{"centos"}
	case "rocky":
		return []string{"rocky"}
	case "almalinux", "alma":
		return []string{"alma", "almalinux"}
	case "kylin":
		return []string{"kylin", "neokylin"}
	case "uos":
		return []string{"uos", "uniontech", "deepin"}
	default:
		return nil
	}
}

var majorVersionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|[^0-9])u(\d+)(?:[^0-9]|$)`),
	regexp.MustCompile(`(?:^|[^0-9])v(\d+)(?:[^0-9]|$)`),
	regexp.MustCompile(`(?:^|[^0-9])release[-_]?(\d+)(?:[^0-9]|$)`),
	regexp.MustCompile(`(?:^|[^0-9])el(\d+)(?:[^0-9]|$)`),
	regexp.MustCompile(`(?:^|[^0-9])rhel[-_]?(\d+)(?:[^0-9]|$)`),
	regexp.MustCompile(`(?:^|[^0-9])-(\d+)(?:[._-]|$)`),
}

func scoreMajorVersion(name string, wantMajor int) int {
	if wantMajor <= 0 {
		return 0
	}
	found := extractMajorCandidates(name)
	if len(found) == 0 {
		return 0
	}
	best := -500
	for _, m := range found {
		switch {
		case m == wantMajor:
			if best < 30 {
				best = 30
			}
		case m != wantMajor:
			if best > -500 {
				continue
			}
			best = -500
		}
	}
	return best
}

func extractMajorCandidates(name string) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, re := range majorVersionPatterns {
		for _, m := range re.FindAllStringSubmatch(name, -1) {
			if len(m) < 2 {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil || n <= 0 || n > 20 {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

func scoreMinorVersion(name string, wantMinor int) int {
	if wantMinor < 0 {
		return 0
	}
	// 8.8 / u8.8 / 8_8
	re := regexp.MustCompile(`(?:^|[^0-9])` + strconv.Itoa(wantMinor/10) + `[._-]` + strconv.Itoa(wantMinor%10) + `(?:[^0-9]|$)`)
	if re.MatchString(name) {
		return 15
	}
	reU := regexp.MustCompile(`(?:^|[^0-9])u` + strconv.Itoa(wantMinor/10) + `[._-]?` + strconv.Itoa(wantMinor%10) + `(?:[^0-9]|$)`)
	if reU.MatchString(name) {
		return 15
	}
	return 0
}

func scoreISOVariant(name string) int {
	switch {
	case strings.Contains(name, "boot") || strings.Contains(name, "netinst") || strings.Contains(name, "minimal"):
		return -50
	case strings.Contains(name, "dvd") || strings.Contains(name, "everything") || strings.Contains(name, "full"):
		return 10
	default:
		return 0
	}
}

// SelectBestISOFilename 从候选文件名中选择最佳匹配。
func SelectBestISOFilename(names []string, profile ISOProfile) (string, int, error) {
	type scored struct {
		name  string
		score int
	}
	var list []scored
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		s := ScoreISOFilename(n, profile)
		if s <= -1000 {
			continue
		}
		list = append(list, scored{name: n, score: s})
	}
	if len(list) == 0 {
		return "", 0, fmt.Errorf("no ISO filename matches OS profile (family=%s distro=%s major=%d arch=%s)",
			profile.Family, profile.DistroID, profile.MajorVer, profile.Arch)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].name < list[j].name
	})
	return list[0].name, list[0].score, nil
}

// ISOMetadataMatchesProfile 判断 ISO 元数据是否与目标 OS 一致（架构 + 主版本；有次版本则要求一致）。
func ISOMetadataMatchesProfile(meta ISOMetadata, profile ISOProfile) bool {
	if profile.MajorVer > 0 && meta.Major > 0 && meta.Major != profile.MajorVer {
		return false
	}
	if meta.Arch != "" && profile.Arch != "" && NormalizeArch(meta.Arch) != profile.Arch {
		return false
	}
	if meta.Minor >= 0 && profile.MinorVer >= 0 && meta.Minor != profile.MinorVer {
		return false
	}
	if profile.Family == "rhel8" && meta.Family == "rhel7" {
		return false
	}
	if profile.Family == "rhel7" && meta.Family == "rhel8" {
		return false
	}
	return true
}

// ParseISOMetadataFromTreeinfo 解析 .treeinfo。
func ParseISOMetadataFromTreeinfo(content string) ISOMetadata {
	meta := ISOMetadata{Minor: -1, Source: "treeinfo"}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch k {
		case "version":
			meta.Version = v
			meta.Major, meta.Minor = parseVersionString(v)
		case "arch":
			meta.Arch = NormalizeArch(v)
		}
	}
	if meta.Major >= 8 {
		meta.Family = "rhel8"
	} else if meta.Major == 7 {
		meta.Family = "rhel7"
	}
	return meta
}

// ParseISOMetadataFromDiscinfo 解析 .discinfo 首段版本信息。
func ParseISOMetadataFromDiscinfo(content string) ISOMetadata {
	meta := ISOMetadata{Minor: -1, Source: "discinfo"}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return meta
	}
	first := strings.ToLower(lines[0])
	meta.Version = strings.TrimSpace(lines[0])

	if strings.Contains(first, "aarch64") || strings.Contains(first, "arm64") {
		meta.Arch = "aarch64"
	} else if strings.Contains(first, "x86_64") || strings.Contains(first, "amd64") {
		meta.Arch = "x86_64"
	}

	re := regexp.MustCompile(`(\d+)\.(\d+)`)
	if m := re.FindStringSubmatch(first); len(m) == 3 {
		meta.Major, _ = strconv.Atoi(m[1])
		meta.Minor, _ = strconv.Atoi(m[2])
	} else if reM := regexp.MustCompile(`(?:release|linux)\s+(\d+)`); len(reM.FindStringSubmatch(first)) == 2 {
		meta.Major, _ = strconv.Atoi(reM.FindStringSubmatch(first)[1])
	}
	if meta.Major >= 8 {
		meta.Family = "rhel8"
	} else if meta.Major == 7 {
		meta.Family = "rhel7"
	}
	return meta
}

// ParseISOMetadataFromMediaRepo 解析 BaseOS/media.repo 中的 version 字段。
func ParseISOMetadataFromMediaRepo(content string) ISOMetadata {
	meta := ISOMetadata{Minor: -1, Source: "media.repo"}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "version") {
			continue
		}
		_, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		meta.Version = v
		meta.Major, meta.Minor = parseVersionString(v)
		break
	}
	if meta.Major >= 8 {
		meta.Family = "rhel8"
	} else if meta.Major == 7 {
		meta.Family = "rhel7"
	}
	return meta
}

func parseVersionString(v string) (major, minor int) {
	major, minor = -1, -1
	v = strings.TrimSpace(v)
	if v == "" {
		return major, minor
	}
	parts := strings.Split(v, ".")
	if len(parts) > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			major = n
		}
	}
	if len(parts) > 1 {
		if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			minor = n
		}
	}
	return major, minor
}

// MergeISOMetadata 合并多来源元数据（后者补全前者空缺）。
func MergeISOMetadata(parts ...ISOMetadata) ISOMetadata {
	var out ISOMetadata
	out.Minor = -1
	for _, p := range parts {
		if p.Version != "" && out.Version == "" {
			out.Version = p.Version
		}
		if p.Major > 0 && out.Major <= 0 {
			out.Major = p.Major
		}
		if p.Minor >= 0 && out.Minor < 0 {
			out.Minor = p.Minor
		}
		if p.Arch != "" && out.Arch == "" {
			out.Arch = NormalizeArch(p.Arch)
		}
		if p.Family != "" && out.Family == "" {
			out.Family = p.Family
		}
		if p.Source != "" && out.Source == "" {
			out.Source = p.Source
		}
	}
	return out
}
