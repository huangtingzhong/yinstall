package db

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// YashanDB 引擎 redo 文件最小字节数（YAS-02075: 100663296 ≈ 96MiB）。
const minRedoFileBytes int64 = 100663296

// TPCC 优化将 REDO_BUFFER_SIZE 设为 128M，要求 redo 文件不低于该值（否则 YAS-02075）。
const minTpccRedoFileBytes int64 = 128 * 1024 * 1024

// fmtUnitSuffix 检查字符串是否已经有单位后缀 (M, G, T, K等)
func fmtUnitSuffix(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	suffixes := []string{"M", "G", "T", "K", "B"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// ParseRedoFileSizeBytes 将 redo 大小字符串转为字节（无单位则按 MB）。
func ParseRedoFileSizeBytes(size string) (int64, error) {
	s := strings.TrimSpace(size)
	if s == "" {
		return 0, fmt.Errorf("empty redo file size")
	}
	upper := strings.ToUpper(s)
	var mult int64 = 1024 * 1024 // 无单位 = MB（与 CLI 默认语义一致）
	numPart := upper
	switch {
	case strings.HasSuffix(upper, "T"):
		mult = 1024 * 1024 * 1024 * 1024
		numPart = strings.TrimSuffix(upper, "T")
	case strings.HasSuffix(upper, "G"):
		mult = 1024 * 1024 * 1024
		numPart = strings.TrimSuffix(upper, "G")
	case strings.HasSuffix(upper, "M"):
		mult = 1024 * 1024
		numPart = strings.TrimSuffix(upper, "M")
	case strings.HasSuffix(upper, "K"):
		mult = 1024
		numPart = strings.TrimSuffix(upper, "K")
	case strings.HasSuffix(upper, "B") && !strings.HasSuffix(upper, "MB") && !strings.HasSuffix(upper, "GB"):
		// 纯 B：按字节；排除被 M/G 已处理的情况
		mult = 1
		numPart = strings.TrimSuffix(upper, "B")
	}
	numPart = strings.TrimSpace(numPart)
	n, err := strconv.ParseFloat(numPart, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid redo file size %q", size)
	}
	return int64(n * float64(mult)), nil
}

// ValidateRedoFileSize 校验 redo 大小不低于引擎下限。
func ValidateRedoFileSize(size string) error {
	bytes, err := ParseRedoFileSizeBytes(size)
	if err != nil {
		return err
	}
	if bytes < minRedoFileBytes {
		return fmt.Errorf("redo file size %s (%d bytes) is below engine minimum %d bytes (~96M); use e.g. 96M or 128M",
			strings.TrimSpace(size), bytes, minRedoFileBytes)
	}
	return nil
}

// ValidateTpccRedoFileSize --db-tpcc 时 redo 须 ≥128M（与 TPCC REDO_BUFFER_SIZE 一致）。
func ValidateTpccRedoFileSize(size string) error {
	if err := ValidateRedoFileSize(size); err != nil {
		return err
	}
	bytes, err := ParseRedoFileSizeBytes(size)
	if err != nil {
		return err
	}
	if bytes < minTpccRedoFileBytes {
		return fmt.Errorf("--db-tpcc requires redo file size >= 128M (got %s / %d bytes); raise --db-redo-file-size or disable --db-tpcc",
			strings.TrimSpace(size), bytes)
	}
	return nil
}

// stepConfigureRedo 在 yashandb.toml 中配置 REDO 文件相关参数
func stepConfigureRedo() *runner.Step {
	return &runner.Step{
		Name:        "Configure REDO Parameters",
		Description: "Configure REDO_FILE_NUM and REDO_FILE_SIZE in cluster configuration file",
		Tags:        []string{"db", "config", "redo"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			// 集群配置是否存在
			res, _ := ctx.Execute(fmt.Sprintf("test -f %s", configPath), false)
			if res == nil || res.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("cluster config not found at %s", configPath))
			}

			redoFileNum := ctx.GetParamInt("db_redo_file_num", 0)
			redoFileSize := ctx.GetParamString("db_redo_file_size", "")
			if redoFileNum < 0 {
				return fmt.Errorf("invalid db_redo_file_num: %d", redoFileNum)
			}
			if redoFileSize != "" && !fmtUnitSuffix(redoFileSize) {
				redoFileSize = redoFileSize + "M"
				ctx.SetResult("db_redo_file_size_normalized", redoFileSize)
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepName:    "Configure REDO Parameters",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityInfo,
					Code:        "PC.DB.REDO.SIZE_UNIT",
					Message:     fmt.Sprintf("redo file size has no unit; apply will append 'M' (MB): %s", redoFileSize),
					Remediation: "specify a unit explicitly if desired, e.g. 128M or 4G",
				})
			}
			if redoFileSize != "" {
				if err := ValidateRedoFileSize(redoFileSize); err != nil {
					return err
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-017: Configure REDO Parameters")
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			// 获取用户指定的参数
			redoFileNum := ctx.GetParamInt("db_redo_file_num", 0)
			redoFileSize := ctx.GetParamString("db_redo_file_size", "")
			if v, ok := ctx.Results["db_redo_file_size_normalized"]; ok {
				if s, ok := v.(string); ok && s != "" {
					redoFileSize = s
				}
			}

			// 如果 redoFileSize 没有单位后缀，自动添加 "M" (MB)
			if redoFileSize != "" && !fmtUnitSuffix(redoFileSize) {
				redoFileSize = redoFileSize + "M"
				ctx.Logger.Info("Added 'M' suffix to redo file size: %s", redoFileSize)
			}

			// 如果参数未指定，根据内存大小自动设置
			if redoFileNum == 0 || redoFileSize == "" {
				memGB, err := commonos.GetTotalMemoryGB(ctx)
				if err != nil {
					ctx.Logger.Warn("Failed to get memory size: %v, using default values", err)
					if redoFileNum == 0 {
						redoFileNum = 4
					}
					if redoFileSize == "" {
						redoFileSize = "128M"
					}
				} else {
					ctx.Logger.Info("Detected system memory: %d GB", memGB)
					if memGB > 128 {
						if redoFileNum == 0 {
							redoFileNum = 6
						}
						if redoFileSize == "" {
							redoFileSize = "4G"
						}
						ctx.Logger.Info("Memory > 128GB, using enhanced REDO settings")
					} else {
						if redoFileNum == 0 {
							redoFileNum = 4
						}
						if redoFileSize == "" {
							redoFileSize = "128M"
						}
						ctx.Logger.Info("Memory <= 128GB, using standard REDO settings")
					}
				}
			}

			ctx.Logger.Info("Configuring REDO parameters:")
			ctx.Logger.Info("  REDO_FILE_NUM: %d", redoFileNum)
			ctx.Logger.Info("  REDO_FILE_SIZE: %s", redoFileSize)

			// 修改配置文件
			cmds := []string{
				fmt.Sprintf(`sed -i 's/REDO_FILE_NUM.*/REDO_FILE_NUM = %d/' %s`, redoFileNum, configPath),
				fmt.Sprintf(`sed -i 's/REDO_FILE_SIZE.*/REDO_FILE_SIZE = "%s"/' %s`, redoFileSize, configPath),
			}

			for _, cmd := range cmds {
				if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
					return fmt.Errorf("failed to configure REDO parameters: %w", err)
				}
			}

			ctx.Logger.Info("REDO parameters configured successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			// 验证配置是否生效
			result, _ := ctx.Execute(fmt.Sprintf("grep 'REDO_FILE_NUM' %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("REDO_FILE_NUM not found in config file")
			}

			result, _ = ctx.Execute(fmt.Sprintf("grep 'REDO_FILE_SIZE' %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("REDO_FILE_SIZE not found in config file")
			}

			ctx.Logger.Info("Verified REDO parameters in config file")
			return nil
		},
	}
}
