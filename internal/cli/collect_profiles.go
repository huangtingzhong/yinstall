// collect_profiles.go - collect 子命令 profile 到步骤类别的映射
package cli

import (
	"sort"
	"strings"

	"github.com/yinstall/internal/runner"
	collectsteps "github.com/yinstall/internal/steps/collect"
)

// profileToCats 定义每个 profile 展开的 CAT 列表（顺序无关，FilterStepsByCategories 保留注册顺序）。
var profileToCats = map[string][]string{
	"full": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatHW, collectsteps.CatOSUser, collectsteps.CatKernel,
		collectsteps.CatNetwork, collectsteps.CatFirewall, collectsteps.CatPackages, collectsteps.CatStorage,
		collectsteps.CatDBPath, collectsteps.CatDBConfig, collectsteps.CatDBFS, collectsteps.CatDBData, collectsteps.CatDBSQL, collectsteps.CatDBLog,
		collectsteps.CatYAC, collectsteps.CatSessionLog,
	},
	"os": {
		collectsteps.CatMeta, collectsteps.CatHW, collectsteps.CatOSUser, collectsteps.CatKernel,
		collectsteps.CatNetwork, collectsteps.CatFirewall, collectsteps.CatPackages, collectsteps.CatStorage,
	},
	"db": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatHW, collectsteps.CatDBPath,
		collectsteps.CatDBConfig, collectsteps.CatDBFS, collectsteps.CatDBData, collectsteps.CatDBSQL,
	},
	"db-core": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatDBPath, collectsteps.CatDBConfig, collectsteps.CatDBFS, collectsteps.CatDBData, collectsteps.CatDBSQL,
	},
	"db-runtime": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatDBData, collectsteps.CatDBSQL,
	},
	"db-logs": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatDBLog,
	},
	"baseline": {
		collectsteps.CatMeta, collectsteps.CatHW, collectsteps.CatOSUser, collectsteps.CatKernel, collectsteps.CatStorage,
	},
	"network": {
		collectsteps.CatMeta, collectsteps.CatNetwork, collectsteps.CatFirewall,
	},
	"hardware": {
		collectsteps.CatMeta, collectsteps.CatHW,
	},
	"kernel": {
		collectsteps.CatMeta, collectsteps.CatKernel,
	},
	"storage": {
		collectsteps.CatMeta, collectsteps.CatStorage,
	},
	"yac": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatHW, collectsteps.CatNetwork, collectsteps.CatYAC,
		collectsteps.CatDBPath, collectsteps.CatDBData,
	},
	"minimal": {
		collectsteps.CatMeta,
	},
	"standby": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatHW, collectsteps.CatNetwork,
		collectsteps.CatDBPath, collectsteps.CatDBConfig, collectsteps.CatDBData,
	},
	"install-os": {
		collectsteps.CatMeta, collectsteps.CatHW, collectsteps.CatOSUser, collectsteps.CatKernel,
		collectsteps.CatNetwork, collectsteps.CatFirewall, collectsteps.CatPackages, collectsteps.CatStorage,
		collectsteps.CatSessionLog,
	},
	"install-db": {
		collectsteps.CatMeta, collectsteps.CatDBEnv, collectsteps.CatHW, collectsteps.CatOSUser, collectsteps.CatKernel,
		collectsteps.CatNetwork, collectsteps.CatFirewall, collectsteps.CatPackages, collectsteps.CatStorage,
		collectsteps.CatDBPath, collectsteps.CatDBConfig, collectsteps.CatDBFS, collectsteps.CatDBData, collectsteps.CatDBSQL,
		collectsteps.CatYAC, collectsteps.CatSessionLog,
	},
}

// ProfileForInstallArchive 返回安装成功后挂钩 collect 使用的 profile。
func ProfileForInstallArchive(hook string, _ bool) string {
	if hook == "db" {
		return "install-db"
	}
	return "install-os"
}

// ExpandProfile 解析逗号分隔的 profile 字符串，返回去重有序的 CAT 列表。
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
		if strings.HasPrefix(p, "CAT-") {
			addCat(p)
			continue
		}
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

// FilterStepsByCategories 从 allSteps 中保留属于给定 CAT 列表的步骤。
func FilterStepsByCategories(allSteps []*runner.Step, cats []string) []*runner.Step {
	if len(cats) == 0 {
		return allSteps
	}
	catSet := make(map[string]bool, len(cats))
	for _, c := range cats {
		catSet[c] = true
	}
	catSet[collectsteps.CatMeta] = true

	var result []*runner.Step
	for _, s := range allSteps {
		if catSet[collectsteps.StepCategory(s)] {
			result = append(result, s)
		}
	}
	return result
}
