// collect_profiles.go - collect 子命令 profile 到步骤类别的映射
// 定义 profile（如 full/os/db）到 CAT 类别的展开规则，以及 R- 步骤 -> CAT 映射，
// 供 collect 子命令按 profile 过滤步骤使用。
package cli

import (
	"sort"
	"strings"

	"github.com/yinstall/internal/runner"
)

// 步骤类别常量（英文，仅用于步骤分类）
const (
	catMeta       = "CAT-META"
	catDBEnv      = "CAT-DB-ENV"
	catHW         = "CAT-HW"
	catOSUser     = "CAT-OS-USER"
	catKernel     = "CAT-KERNEL"
	catNetwork    = "CAT-NETWORK"
	catFirewall   = "CAT-FIREWALL"
	catPackages   = "CAT-PACKAGES"
	catStorage    = "CAT-STORAGE"
	catDBPath     = "CAT-DB-PATH"
	catDBConfig   = "CAT-DB-CONFIG"
	catDBFS       = "CAT-DB-FS"
	catDBData     = "CAT-DB-DATA"
	catDBSQL      = "CAT-DB-SQL"
	catDBLog      = "CAT-DB-LOG"
	catYAC        = "CAT-YAC"
	catSessionLog = "CAT-SESSION-LOG"
)

// stepCategoryMap 记录每个 R- 步骤所属的 CAT 类别。
var stepCategoryMap = map[string]string{
	"R-001": catMeta,
	"R-002": catMeta,
	"R-003": catMeta,
	"R-004": catDBEnv,
	"R-010": catHW,
	"R-011": catHW,
	"R-012": catOSUser,
	"R-013": catKernel,
	"R-014": catKernel,
	"R-015": catNetwork,
	"R-016": catNetwork,
	"R-017": catFirewall,
	"R-018": catPackages,
	"R-019": catStorage,
	"R-020": catDBPath,
	"R-021": catDBConfig,
	"R-022": catDBFS,
	"R-023": catDBData,
	"R-024": catDBData,
	"R-025": catDBData,
	"R-026": catDBSQL,
	"R-027": catDBConfig,
	"R-028": catSessionLog,
	"R-029": catMeta,
	"R-030": catYAC,
	"R-034": catDBLog,
}

// profileToCats 定义每个 profile 展开的 CAT 列表（顺序无关，FilterStepsByCategories 保留注册顺序）。
var profileToCats = map[string][]string{
	"full": {
		catMeta, catDBEnv, catHW, catOSUser, catKernel,
		catNetwork, catFirewall, catPackages, catStorage,
		catDBPath, catDBConfig, catDBFS, catDBData, catDBSQL, catDBLog,
		catYAC, catSessionLog,
	},
	"os": {
		catMeta, catHW, catOSUser, catKernel,
		catNetwork, catFirewall, catPackages, catStorage,
	},
	"db": {
		catMeta, catDBEnv, catHW, catDBPath,
		catDBConfig, catDBFS, catDBData, catDBSQL,
	},
	"db-core": {
		catMeta, catDBEnv, catDBPath, catDBConfig, catDBFS, catDBData, catDBSQL,
	},
	"db-runtime": {
		catMeta, catDBEnv, catDBData, catDBSQL,
	},
	"db-logs": {
		catMeta, catDBEnv, catDBLog,
	},
	"baseline": {
		catMeta, catHW, catOSUser, catKernel, catStorage,
	},
	"network": {
		catMeta, catNetwork, catFirewall,
	},
	"hardware": {
		catMeta, catHW,
	},
	"kernel": {
		catMeta, catKernel,
	},
	"storage": {
		catMeta, catStorage,
	},
	"yac": {
		catMeta, catDBEnv, catHW, catNetwork, catYAC,
		catDBPath, catDBData,
	},
	"minimal": {
		catMeta,
	},
	"standby": {
		catMeta, catDBEnv, catHW, catNetwork,
		catDBPath, catDBConfig, catDBData,
	},
	// install-os / install-db：os|db 全局 --archive/-a 挂钩专用（勿与独立 collect 的 os/db profile 混淆）
	"install-os": {
		catMeta, catHW, catOSUser, catKernel,
		catNetwork, catFirewall, catPackages, catStorage,
		catSessionLog,
	},
	"install-db": {
		catMeta, catDBEnv, catHW, catOSUser, catKernel,
		catNetwork, catFirewall, catPackages, catStorage,
		catDBPath, catDBConfig, catDBFS, catDBData, catDBSQL,
		catYAC, catSessionLog,
	},
}

// ProfileForInstallArchive 返回安装成功后挂钩 collect 使用的 profile。
//   - install-os：仅 OS 基线（不含 DB）
//   - install-db：OS + DB；含 CAT-YAC（多节点时 R-030 执行，单节点 PreCheck 跳过）
func ProfileForInstallArchive(hook string, _ bool) string {
	if hook == "db" {
		return "install-db"
	}
	return "install-os"
}

// ExpandProfile 解析逗号分隔的 profile 字符串，返回去重有序的 CAT 列表。
// 若 profile 为空字符串则返回 full profile 对应的 CAT 列表。
// 支持直接传入 CAT-* 类别名，允许用户精细控制步骤范围。
func ExpandProfile(profile string) []string {
	if profile == "" {
		profile = "full"
	}

	seen := make(map[string]bool)
	var cats []string

	addCat := func(c string) {
		if !seen[c] {
			seen[c] = true
			cats = append(cats, c)
		}
	}

	for _, p := range strings.Split(profile, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 若直接传入 CAT-* 形式则作为原始类别添加
		if strings.HasPrefix(p, "CAT-") {
			addCat(p)
			continue
		}
		// 按 profile 名展开
		if catList, ok := profileToCats[p]; ok {
			for _, c := range catList {
				addCat(c)
			}
		}
	}
	return cats
}

// ListProfiles 返回所有可用 profile 名称（已排序）。
func ListProfiles() []string {
	names := make([]string, 0, len(profileToCats))
	for k := range profileToCats {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// StepCategory 返回步骤的 CAT 类别。若不在映射中则返回 catMeta。
func StepCategory(stepID string) string {
	if c, ok := stepCategoryMap[stepID]; ok {
		return c
	}
	return catMeta
}

// FilterStepsByCategories 从 allSteps 中保留属于给定 CAT 列表的步骤，
// 保持步骤原始注册顺序（CAT-META 步骤始终保留）。
func FilterStepsByCategories(allSteps []*runner.Step, cats []string) []*runner.Step {
	if len(cats) == 0 {
		return allSteps
	}
	catSet := make(map[string]bool, len(cats))
	for _, c := range cats {
		catSet[c] = true
	}
	// CAT-META 步骤始终保留（初始化/收尾为基础设施步骤）
	catSet[catMeta] = true

	var result []*runner.Step
	for _, s := range allSteps {
		cat := StepCategory(s.ID)
		if catSet[cat] {
			result = append(result, s)
		}
	}
	return result
}
