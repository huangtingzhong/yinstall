package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// DiskGroupConfig 表示一组 YAC 盘配置（Name 为可选 legacy 角色标签，非 YFS 组名）。
type DiskGroupConfig struct {
	Name  string
	Disks []string
}

// ParseDiskGroupConfig 解析盘参数：推荐 `/dev/a,/dev/b`；兼容旧 `role:/dev/a,/dev/b`。
func ParseDiskGroupConfig(config string) (*DiskGroupConfig, error) {
	name, disks, err := splitYACDiskGroupSpec(config)
	if err != nil {
		return nil, err
	}
	if name == "" && len(disks) == 0 {
		return nil, nil
	}
	return &DiskGroupConfig{Name: name, Disks: disks}, nil
}

// splitYACDiskGroupSpec 拆出可选 role 与盘路径列表；空串返回 "", nil, nil。
func splitYACDiskGroupSpec(config string) (name string, disks []string, err error) {
	config = strings.TrimSpace(config)
	if config == "" {
		return "", nil, nil
	}
	diskPart := config
	if idx := strings.Index(config, ":"); idx > 0 {
		role := strings.TrimSpace(config[:idx])
		rest := strings.TrimSpace(config[idx+1:])
		// legacy：role 不含 / 与逗号（避免把路径误判为角色）
		if role != "" && !strings.ContainsAny(role, "/,") {
			if rest == "" {
				return "", nil, fmt.Errorf("disk role '%s' must have at least one disk", role)
			}
			name = role
			diskPart = rest
		}
	}
	for _, d := range strings.Split(diskPart, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			disks = append(disks, d)
		}
	}
	if len(disks) == 0 {
		if name != "" {
			return "", nil, fmt.Errorf("disk role '%s' must have at least one disk", name)
		}
		return "", nil, fmt.Errorf("invalid disk list %q, expected '/dev/disk1,/dev/disk2'", config)
	}
	return name, disks, nil
}

// DiskGroupConfigsDistinct 两组是否应分别处理（legacy 异名，或盘路径不同）。
func DiskGroupConfigsDistinct(a, b *DiskGroupConfig) bool {
	if a == nil || b == nil {
		return a != b
	}
	if a.Name != "" && b.Name != "" {
		return a.Name != b.Name
	}
	if len(a.Disks) != len(b.Disks) {
		return true
	}
	for i := range a.Disks {
		if a.Disks[i] != b.Disks[i] {
			return true
		}
	}
	return false
}

// DiskGroupConfigLabel 日志用标签；Name 空时用 fallback。
func DiskGroupConfigLabel(dg *DiskGroupConfig, fallback string) string {
	if dg != nil && strings.TrimSpace(dg.Name) != "" {
		return dg.Name
	}
	return fallback
}

// stepValidateYacDiskgroups 校验 YAC diskgroup 配置与磁盘类型
func stepValidateYacDiskgroups() *runner.Step {
	return &runner.Step{
		Name:        "Validate YAC Diskgroups",
		Description: "Validate YAC diskgroup configuration and check disk types",
		Tags:        []string{"os", "yac", "diskgroup"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// 仅在 YAC 模式下执行
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if !isYACMode {
				return fmt.Errorf("not in YAC mode, skipping diskgroup validation")
			}

			systemdgStr := ctx.GetParamString("yac_systemdg", "")
			datadgStr := ctx.GetParamString("yac_datadg", "")
			archdgStr := ctx.GetParamString("yac_archdg", "")

			systemdg, err := ParseDiskGroupConfig(systemdgStr)
			if err != nil {
				return fmt.Errorf("invalid systemdg: %w", err)
			}
			if systemdg == nil {
				return fmt.Errorf("systemdg is required in YAC mode")
			}

			datadg, err := ParseDiskGroupConfig(datadgStr)
			if err != nil {
				return fmt.Errorf("invalid datadg: %w", err)
			}
			if datadg == nil {
				return fmt.Errorf("datadg is required in YAC mode")
			}

			archdg, err := ParseDiskGroupConfig(archdgStr)
			if err != nil {
				return fmt.Errorf("invalid archdg: %w", err)
			}
			if archdg == nil {
				// 保持只读：仅上报，不修改 ctx.Params
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepName:    "Validate YAC Diskgroups",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityInfo,
					Code:        "PC.OS.YAC.ARCHDG_DEFAULT",
					Message:     "archdg is not set; apply will default to using datadg (precheck will not mutate parameters).",
					Remediation: "If you need a separate archive diskgroup, set --yac-archdg explicitly.",
				})
			}

			// 检查磁盘是否存在（只读）
			allDisks := make(map[string]bool)
			hasMultipath := false
			hasNonMultipath := false

			for _, dg := range []*DiskGroupConfig{systemdg, datadg} {
				for _, d := range dg.Disks {
					allDisks[d] = true
					if IsMultipathDisk(d) {
						hasMultipath = true
					} else {
						hasNonMultipath = true
					}
				}
			}
			if archdg != nil && DiskGroupConfigsDistinct(archdg, datadg) {
				for _, d := range archdg.Disks {
					allDisks[d] = true
					if IsMultipathDisk(d) {
						hasMultipath = true
					} else {
						hasNonMultipath = true
					}
				}
			}

			for disk := range allDisks {
				res, _ := ctx.Execute(fmt.Sprintf("test -b %s || test -e %s", disk, disk), false)
				if res == nil || res.GetExitCode() != 0 {
					return fmt.Errorf("disk %s not found", disk)
				}
			}

			if hasMultipath && hasNonMultipath {
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepName:    "Validate YAC Diskgroups",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityWarn,
					Code:        "PC.OS.YAC.MIXED_MULTIPATH",
					Message:     "Detected mixed multipath and non-multipath disks; this may complicate or break later configuration.",
					Remediation: "Prefer a consistent disk type (all multipath or all non-multipath) and verify udev/multipath configuration.",
				})
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-022: Validate YAC Diskgroups")
			systemdgStr := ctx.GetParamString("yac_systemdg", "")
			datadgStr := ctx.GetParamString("yac_datadg", "")
			archdgStr := ctx.GetParamString("yac_archdg", "")

			// 解析 systemdg（必填）
			systemdg, err := ParseDiskGroupConfig(systemdgStr)
			if err != nil {
				return fmt.Errorf("invalid systemdg: %w", err)
			}
			if systemdg == nil {
				return fmt.Errorf("systemdg is required in YAC mode")
			}

			// 解析 datadg（必填）
			datadg, err := ParseDiskGroupConfig(datadgStr)
			if err != nil {
				return fmt.Errorf("invalid datadg: %w", err)
			}
			if datadg == nil {
				return fmt.Errorf("datadg is required in YAC mode")
			}

			// 解析 archdg（可选，默认等同 datadg）
			archdg, err := ParseDiskGroupConfig(archdgStr)
			if err != nil {
				return fmt.Errorf("invalid archdg: %w", err)
			}
			if archdg == nil {
				// 未指定 archdg 时使用 datadg
				archdg = datadg
				ctx.Logger.Info("archdg not specified, using datadg %s", DiskGroupConfigLabel(datadg, "datadg"))
			}

			// 缓存解析结果供后续步骤使用
			ctx.SetResult("yac_systemdg_config", systemdg)
			ctx.SetResult("yac_datadg_config", datadg)
			ctx.SetResult("yac_archdg_config", archdg)

			// 汇总全部磁盘并判断是否含 multipath 设备
			allDisks := make(map[string]bool)
			hasMultipath := false
			hasNonMultipath := false

			for _, dg := range []*DiskGroupConfig{systemdg, datadg} {
				if dg == nil {
					continue
				}
				for _, disk := range dg.Disks {
					allDisks[disk] = true
					if IsMultipathDisk(disk) {
						hasMultipath = true
					} else {
						hasNonMultipath = true
					}
				}
			}

			if archdg != nil && DiskGroupConfigsDistinct(archdg, datadg) {
				for _, disk := range archdg.Disks {
					allDisks[disk] = true
					if IsMultipathDisk(disk) {
						hasMultipath = true
					} else {
						hasNonMultipath = true
					}
				}
			}

			ctx.Logger.Info("YAC Diskgroups:")
			ctx.Logger.Info("  systemdg: %s -> %v", DiskGroupConfigLabel(systemdg, "systemdg"), systemdg.Disks)
			ctx.Logger.Info("  datadg:   %s -> %v", DiskGroupConfigLabel(datadg, "datadg"), datadg.Disks)
			ctx.Logger.Info("  archdg:   %s -> %v", DiskGroupConfigLabel(archdg, "archdg"), archdg.Disks)

			ctx.Logger.Info("Checking disk existence...")
			for disk := range allDisks {
				result, _ := ctx.Execute(fmt.Sprintf("test -b %s || test -e %s", disk, disk), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("disk %s not found", disk)
				}
				ctx.Logger.Info("  %s: OK", disk)
			}

			needMultipath := hasNonMultipath && !hasMultipath
			ctx.SetResult("yac_need_multipath", needMultipath)
			ctx.SetResult("yac_has_multipath_disks", hasMultipath)

			if hasMultipath && hasNonMultipath {
				ctx.Logger.Warn("Mixed multipath and non-multipath disks detected")
			}

			if hasMultipath {
				ctx.Logger.Info("Multipath disks detected, skipping multipath software configuration")
			} else {
				ctx.Logger.Info("Non-multipath disks detected, enabling multipath and udev configuration")
				ctx.Params["yac_multipath_enable"] = true
				ctx.Params["yac_multipath_auto_wwid"] = true
				ctx.Params["yac_need_multipath"] = true
				ctx.Params["yac_raw_disk_udev"] = true
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// 确认解析结果已写入 Results
			if ctx.Results["yac_systemdg_config"] == nil {
				return fmt.Errorf("systemdg config not stored")
			}
			if ctx.Results["yac_datadg_config"] == nil {
				return fmt.Errorf("datadg config not stored")
			}
			return nil
		},
	}
}
