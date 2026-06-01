// s003_install_deps.go - 压测依赖安装
//
// 安装 sysbench、fio、sysstat（含 iostat/mpstat）、numactl、iperf3（可选）。
//
// ISO / yum 模式对齐（与 OS 模块相同语义）：
//
//	--os-yum-mode=none       使用默认已配置仓库（默认）
//	--os-yum-mode=online     等同 none，确保网络可达
//	--os-yum-mode=local-iso  先 EnsureLocalISORepo（挂载 ISO）再安装；
//	                             fio/iperf3 通常不在基础 ISO（属 EPEL），
//	                             安装失败时会提示原因并尝试源码编译兜底。
//
// fio 源码编译路径（repo 无包时，参考 https://github.com/axboe/fio）：
//  1. 在 -L 中查找 fio-*.tar.gz / .zip，经 FindAndDistribute 落到 -R 或远端 $HOME（先查后传）
//  2. Action 开头通过 s03InstallFIOBuildDeps 提前安装 gcc/make/libaio-devel/zlib-devel 等
//  3. 解压 → ./configure --prefix=/usr/local → 校验 CONFIG_LIBAIO → make install
//  4. PostCheck 用 fio --enghelp 确认 libaio/sync/psync/io_uring 等 stressos 可用引擎
//
// iperf3 源码编译路径（repo 无包时，参考 https://github.com/esnet/iperf）：
//  1. 在 LocalSoftwareDirs 中查找 iperf-3.*.tar.gz / iperf3-*.tar.gz
//  2. 编译依赖：gcc make（Prerequisites: None，release 压缩包含预生成 configure）
//  3. 解压 → ./configure --prefix=/usr/local → make -j$(nproc) → make install
//  4. 安装后命令名为 iperf3（不是 iperf）
//
// sysbench 源码编译路径（repo 无包时）：
//  1. 在 LocalSoftwareDirs 中查找 sysbench-1.0.20.tar.gz / .zip
//  2. 安装编译依赖：gcc make autoconf automake libtool m4 pkg-config openssl-devel
//  3. 解压 → autogen.sh → configure → make -j$(nproc) → make install
//  4. ln -sf /usr/local/sysbench/bin/sysbench /usr/local/bin/sysbench
//
// PreCheck：扫描已安装/缺失工具，若需源码编译则预检源码包是否存在。
// PostCheck：逐工具 command -v 验证，写入安装报告。
package stressos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonfile "github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// s03Tool 描述一个需要安装的压测工具。
type s03Tool struct {
	name           string // 命令名（command -v 检测）
	optional       bool   // true=可选，安装失败只 warn
	epolOnly       bool   // true=该包通常只在 EPEL，基础 ISO 无法安装
	hasSourceBuild bool   // true=支持源码编译兜底（pkg 安装失败时尝试）
	dedicatedPkg   bool   // true=走专用安装函数（如 numactl 多包名）
}

// s03Tools 定义所有待检/待装工具（顺序即安装顺序，sysbench 单独处理放最后）。
var s03Tools = []s03Tool{
	{name: "fio", optional: false, epolOnly: true, hasSourceBuild: true},
	{name: "sysstat", optional: false},
	{name: "numactl", optional: false, dedicatedPkg: true},
	{name: "iperf3", optional: true, epolOnly: true, hasSourceBuild: true},
	// sysbench 在 Action 中单独处理（源码编译逻辑更复杂）
}

// StepS03InstallDeps 返回 S-03 步骤：安装压测依赖工具。
func StepS03InstallDeps() *runner.Step {
	return &runner.Step{
		ID:       "S-03",
		Name:     "Install stress test dependencies",
		Optional: true,

		// PreCheck：
		//   1. 检查 --install-deps 开关，关闭则跳过本步骤。
		//   2. 扫描哪些工具已安装、哪些缺失，输出到 INFO。
		//   3. 若缺 sysbench 且不在 repo，预检源码包是否存在（仅记录 warn，不 fail）。
		//   4. 若所有工具均已安装，跳过本步骤（无需重复安装）。
		PreCheck: func(ctx *runner.StepContext) error {
			if !getBool(ctx, "install_deps", true) {
				return fmt.Errorf("dependency installation disabled (--install-deps=false)")
			}

			missing, installed := s03ScanTools(ctx)
			ctx.Logger.Info("[S-03] pre-scan: installed=[%s] missing=[%s]",
				strings.Join(installed, ","),
				func() string {
					if len(missing) == 0 {
						return "none"
					}
					return strings.Join(missing, ",")
				}())

			// 预检各工具的源码包（早期发现，避免安装到一半才报错）
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			yumMode := getStr(ctx, "os_yum_mode", "none")
			isRHEL8 := commonos.IsRHEL8(ctx.OSInfo)

			if s03Contains(missing, "fio") && !s03CanInstallViaPkg(ctx, "fio", pkgManager, yumMode, isRHEL8) {
				if p, err := s03FindFIOSource(ctx); err != nil {
					ctx.Logger.Warn("[S-03] fio not in repo and source package not found: %v", err)
					ctx.Logger.Warn("[S-03] place fio-<version>.tar.gz under --local-software-dirs to enable source build")
				} else {
					ctx.Logger.Info("[S-03] fio source package found at %s, will build from source if repo install fails", p)
				}
			}

			if s03Contains(missing, "iperf3") && !s03CanInstallViaPkg(ctx, "iperf3", pkgManager, yumMode, isRHEL8) {
				if p, err := s03FindIPerfSource(ctx); err != nil {
					ctx.Logger.Warn("[S-03] iperf3 not in repo and source package not found: %v", err)
					ctx.Logger.Warn("[S-03] place iperf-3.x.tar.gz under --local-software-dirs to enable source build")
				} else {
					ctx.Logger.Info("[S-03] iperf3 source package found at %s, will build from source if repo install fails", p)
				}
			}

			// 若 sysbench 缺失，预检源码包是否可用（早期发现，避免安装到一半才报错）
			if s03Contains(missing, "sysbench") && !s03CanInstallViaPkg(ctx, "sysbench", pkgManager, yumMode, isRHEL8) {
				if _, err := s03FindSysbenchSource(ctx); err != nil {
					ctx.Logger.Warn("[S-03] sysbench not in repo and source package not found: %v", err)
					ctx.Logger.Warn("[S-03] place sysbench-1.0.20.tar.gz under --local-software-dirs to enable source build")
				} else {
					ctx.Logger.Info("[S-03] sysbench source package found, will build from source if repo install fails")
				}
			}

			if s03Contains(missing, "numactl") {
				pkgs := s03NumactlPackages(pkgManager)
				ctx.Logger.Info("[S-03] numactl missing; will install OS packages: %s", strings.Join(pkgs, ", "))
				for _, pkg := range pkgs {
					if !s03CanInstallViaPkg(ctx, pkg, pkgManager, yumMode, isRHEL8) {
						ctx.Logger.Warn("[S-03] numactl package %q not found in %s repo (yum_mode=%s)",
							pkg, pkgManager, yumMode)
					}
				}
			}

			// 若所有工具均已安装，跳过本步骤
			allTools := append([]string{"sysbench"}, s03ToolNames()...)
			var allMissing []string
			for _, t := range allTools {
				if !s03ToolAvailable(ctx, t) {
					allMissing = append(allMissing, t)
				}
			}
			if len(allMissing) == 0 {
				return fmt.Errorf("all tools already installed: %s (use -f S-03 to reinstall)",
					strings.Join(allTools, ", "))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			hostDir := stressHostDir(ctx)
			reportPath := filepath.Join(hostDir, "deps", "install_report.txt")
			var reportLines []string
			add := func(line string) {
				reportLines = append(reportLines, line)
				ctx.Logger.Info("[S-03] %s", line)
			}

			add("=== Dependency Installation Report ===")
			add(fmt.Sprintf("host:      %s", ctx.Executor.Host()))
			add(fmt.Sprintf("time:      %s", time.Now().UTC().Format(time.RFC3339)))

			// pkgManager/yumMode/isRHEL8 在 Action 中独立读取（Action 与 PreCheck 是不同调用）
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			yumMode := getStr(ctx, "os_yum_mode", "none")
			isRHEL8 := commonos.IsRHEL8(ctx.OSInfo)

			add(fmt.Sprintf("pkg_mgr:   %s", pkgManager))
			add(fmt.Sprintf("yum_mode:  %s", yumMode))
			add("")

			// ── 1. 挂载本地 ISO（local-iso 模式）──────────────────────────────
			if yumMode == "local-iso" {
				add("--- ISO repo setup ---")
				if err := commonos.EnsureLocalISORepo(ctx); err != nil {
					add(fmt.Sprintf("WARNING: EnsureLocalISORepo failed: %v", err))
					add("  Packages that depend on ISO repo may fail to install.")
					appendWarning(ctx, "S-03", fmt.Sprintf("EnsureLocalISORepo: %v", err))
				} else {
					add("ISO repo mounted and configured OK")
				}
				add("")
			}

			// ── 1.5 提前安装 fio 编译依赖（含 libaio-devel，供 repo 包与源码编译共用）──
			add("--- fio build dependencies (early) ---")
			if err := s03InstallFIOBuildDeps(ctx, pkgManager, yumMode, isRHEL8); err != nil {
				add(fmt.Sprintf("WARNING: fio build deps: %v", err))
				add("  fio source build may lack libaio engine; IO bench default --io-engine=libaio will fail")
				appendWarning(ctx, "S-03", fmt.Sprintf("fio build deps: %v", err))
			} else {
				add("fio build dependencies OK (libaio-devel, gcc, make, zlib-devel, ...)")
			}
			add("")

			// ── 2. 安装 fio / sysstat / numactl / iperf3 ──────────────────────
			add("--- Package installation ---")
			for _, tool := range s03Tools {
				if tool.name == "fio" {
					if done, line, warn := s03InstallOrRebuildFIO(ctx, pkgManager, yumMode, isRHEL8); done {
						add(line)
						if warn != nil {
							appendWarning(ctx, "S-03", warn.Error())
						}
						continue
					}
				}

				if tool.dedicatedPkg && tool.name == "numactl" {
					add("--- numactl (OS packages) ---")
					add(fmt.Sprintf("  packages:  %s", strings.Join(s03NumactlPackages(pkgManager), " ")))
					ok, method := s03InstallNumactl(ctx, pkgManager, yumMode, isRHEL8)
					if ok {
						add(fmt.Sprintf("  %-12s OK  (%s)", tool.name, method))
					} else {
						add(fmt.Sprintf("  %-12s WARN (install failed; NUMA CPU/MEM bind tests may be skipped)", tool.name))
						appendWarning(ctx, "S-03", "failed to install numactl packages")
					}
					add("")
					continue
				}

				ok, method := s03InstallTool(ctx, tool.name, pkgManager, yumMode, isRHEL8)
				if ok {
					add(fmt.Sprintf("  %-12s OK  (%s)", tool.name, method))
					continue
				}

				// pkg 安装失败且支持源码编译兜底
				if tool.hasSourceBuild {
					add(fmt.Sprintf("  %-12s pkg install failed, attempting source build...", tool.name))
					if tool.epolOnly {
						add(fmt.Sprintf("  NOTE: %s is typically in EPEL and not in the base ISO", tool.name))
					}
					var srcErr error
					switch tool.name {
					case "fio":
						srcErr = s03BuildFIOFromSource(ctx, pkgManager, yumMode, isRHEL8)
					case "iperf3":
						srcErr = s03BuildIPerfFromSource(ctx, pkgManager, yumMode, isRHEL8)
					}
					if srcErr != nil {
						add(fmt.Sprintf("  %-12s source build FAILED: %v", tool.name, srcErr))
						if tool.optional {
							add(fmt.Sprintf("  %-12s SKIP (optional, not available)", tool.name))
						} else {
							add(fmt.Sprintf("  %-12s WARN (IO benchmarks will be skipped)", tool.name))
							appendWarning(ctx, "S-03", fmt.Sprintf("%s source build failed: %v", tool.name, srcErr))
						}
					} else {
						add(fmt.Sprintf("  %-12s OK  (source build)", tool.name))
					}
					continue
				}

				// 无源码编译路径
				if tool.optional {
					add(fmt.Sprintf("  %-12s SKIP (optional, not available)", tool.name))
					if tool.epolOnly && yumMode == "local-iso" {
						add(fmt.Sprintf("  NOTE: %s is typically in EPEL and may not be in the base ISO; "+
							"install manually or use --os-yum-mode=none/online", tool.name))
					}
				} else {
					add(fmt.Sprintf("  %-12s WARN (install failed, stress tests may be affected)", tool.name))
					if tool.epolOnly && yumMode == "local-iso" {
						add(fmt.Sprintf("  NOTE: %s is typically in EPEL and may not be in the base ISO; "+
							"place %s-<version>.tar.gz under --local-software-dirs for source build", tool.name, tool.name))
					}
					appendWarning(ctx, "S-03", fmt.Sprintf("failed to install %s", tool.name))
				}
			}
			add("")

			// ── 3. sysbench：先尝试 repo，失败则源码编译 ───────────────────────
			add("--- sysbench ---")
			if ok, method := s03InstallTool(ctx, "sysbench", pkgManager, yumMode, isRHEL8); ok {
				add(fmt.Sprintf("  sysbench     OK  (%s)", method))
			} else {
				add("  sysbench not in repo, attempting source build (sysbench-1.0.20)...")
				if err := s03BuildSysbenchFromSource(ctx, pkgManager, yumMode, isRHEL8); err != nil {
					add(fmt.Sprintf("  sysbench source build FAILED: %v", err))
					add("  CPU/MEM benchmarks will be skipped.")
					appendWarning(ctx, "S-03", fmt.Sprintf("sysbench source build failed: %v", err))
				} else {
					add("  sysbench     OK  (source build)")
				}
			}
			add("")

			// 写入安装报告（PostCheck 会追加验证结果）
			ctx.Results["deps_report_path"] = reportPath
			ctx.Results["deps_report_lines"] = reportLines
			return nil
		},

		// PostCheck：逐工具 command -v 验证，追加到安装报告，不 fail（S-03 是 Optional）。
		PostCheck: func(ctx *runner.StepContext) error {
			reportPath, _ := ctx.Results["deps_report_path"].(string)
			prevLines, _ := ctx.Results["deps_report_lines"].([]string)

			var lines []string
			lines = append(lines, prevLines...)
			lines = append(lines, "--- Post-install verification ---")

			allTools := append(s03ToolNames(), "sysbench")
			for _, tool := range allTools {
				cmdName := tool
				if s03ToolAvailable(ctx, cmdName) {
					// 获取版本号（best-effort）
					ver := s03GetVersion(ctx, cmdName)
					lines = append(lines, fmt.Sprintf("  %-12s AVAILABLE  %s", cmdName, ver))
					ctx.Logger.Info("[S-03] verified: %s %s", cmdName, ver)
					if cmdName == "fio" {
						if missing, err := s03VerifyFIOEngines(ctx); err != nil {
							lines = append(lines, fmt.Sprintf("  fio-engines  CHECK FAILED  %v", err))
							ctx.Logger.Warn("[S-03] fio engine check: %v", err)
						} else if len(missing) > 0 {
							lines = append(lines, fmt.Sprintf("  fio-engines  MISSING  %s", strings.Join(missing, ",")))
							ctx.Logger.Warn("[S-03] fio missing engines: %s", strings.Join(missing, ","))
						} else {
							lines = append(lines, "  fio-engines  OK  libaio,sync,psync,io_uring")
						}
					}
					if cmdName == "numactl" {
						if err := s03VerifyNumactl(ctx); err != nil {
							lines = append(lines, fmt.Sprintf("  numactl-hw   CHECK FAILED  %v", err))
							ctx.Logger.Warn("[S-03] numactl verify: %v", err)
						} else {
							lines = append(lines, "  numactl-hw   OK  (numactl --hardware)")
						}
					}
				} else {
					lines = append(lines, fmt.Sprintf("  %-12s MISSING    (some benchmarks will be skipped)", cmdName))
					ctx.Logger.Warn("[S-03] %s not available after install (some stress tests will be skipped)", cmdName)
				}
			}

			if reportPath != "" {
				if err := writeTextFile(reportPath, strings.Join(lines, "\n")+"\n"); err != nil {
					ctx.Logger.Warn("[S-03] write install_report.txt: %v", err)
				}
			}
			return nil
		},
	}
}

// ── 工具探测与安装 ────────────────────────────────────────────────────────────

// s03ToolAvailable 检查工具是否已在远端 PATH 中可用。
func s03ToolAvailable(ctx *runner.StepContext, tool string) bool {
	r, _ := stressExecute(ctx, "command -v "+tool+" >/dev/null 2>&1", false, 10*time.Second)
	return r != nil && r.GetExitCode() == 0
}

// s03CanInstallViaPkg 探测包管理器是否有该包可用（不安装，仅查询）。
// 仅做 best-effort 探测，返回 false 表示不确定/不可用，不代表一定失败。
func s03CanInstallViaPkg(ctx *runner.StepContext, pkg, pkgManager, yumMode string, isRHEL8 bool) bool {
	if s03ToolAvailable(ctx, pkg) {
		return true
	}
	var checkCmd string
	switch pkgManager {
	case "apt":
		checkCmd = "apt-cache show " + pkg + " >/dev/null 2>&1"
	case "dnf":
		checkCmd = "dnf info " + pkg + " >/dev/null 2>&1"
	default:
		checkCmd = "yum info " + pkg + " >/dev/null 2>&1"
	}
	r, _ := stressExecute(ctx, checkCmd, false, 15*time.Second)
	return r != nil && r.GetExitCode() == 0
}

// s03InstallOrRebuildFIO 处理已装但缺 libaio 等引擎、或 -f S-03 强制重装的情况。
// 返回 done=true 表示无需再走 pkg/通用源码路径；warn 非 nil 时仅记录警告。
func s03InstallOrRebuildFIO(ctx *runner.StepContext, pkgManager, yumMode string, isRHEL8 bool) (done bool, line string, warn error) {
	if !s03ToolAvailable(ctx, "fio") {
		return false, "", nil
	}

	force := ctx.IsForceStep()
	missing, engErr := s03VerifyFIOEngines(ctx)
	if engErr != nil {
		ctx.Logger.Warn("[S-03] fio engine check: %v", engErr)
	}
	needRebuild := force || len(missing) > 0
	if !needRebuild {
		return true, "  fio          OK  (already-installed)", nil
	}

	if _, err := s03FindFIOSource(ctx); err != nil {
		if len(missing) > 0 {
			return true,
				fmt.Sprintf("  fio          WARN (installed, missing engines: %s; no source package to rebuild)",
					strings.Join(missing, ",")),
				fmt.Errorf("fio missing engines %v", missing)
		}
		if force {
			return true, "  fio          OK  (already-installed, no source package for forced rebuild)", nil
		}
		return true, "  fio          OK  (already-installed)", nil
	}

	reason := "forced rebuild"
	if len(missing) > 0 {
		reason = "rebuild (missing engines: " + strings.Join(missing, ",") + ")"
	}
	ctx.Logger.Info("[S-03] fio %s from source", reason)
	if err := s03BuildFIOFromSource(ctx, pkgManager, yumMode, isRHEL8); err != nil {
		return true, fmt.Sprintf("  fio          source rebuild FAILED: %v", err), err
	}
	if missing2, _ := s03VerifyFIOEngines(ctx); len(missing2) > 0 {
		return true,
			fmt.Sprintf("  fio          WARN (rebuilt, still missing engines: %s)", strings.Join(missing2, ",")),
			fmt.Errorf("fio still missing engines: %v", missing2)
	}
	return true, "  fio          OK  (source rebuild)", nil
}

// s03NumactlPackages 返回安装 numactl 命令所需的 OS 包名（按包管理器区分）。
func s03NumactlPackages(pkgManager string) []string {
	switch pkgManager {
	case "apt":
		return []string{"numactl"}
	default:
		// RHEL/CentOS/OL/Kylin 等：numactl 提供 CLI；libnuma 为 NUMA 库（部分 ISO 需显式安装）
		return []string{"numactl", "libnuma"}
	}
}

// s03InstallNumactl 通过 OS 包安装 numactl（及依赖包），确保 command -v numactl 可用。
func s03InstallNumactl(ctx *runner.StepContext, pkgManager, yumMode string, isRHEL8 bool) (bool, string) {
	if s03ToolAvailable(ctx, "numactl") {
		return true, "already-installed"
	}
	pkgs := s03NumactlPackages(pkgManager)
	ctx.Logger.Info("[S-03] installing numactl packages: %v", pkgs)

	// 优先一次安装全部包
	cmd := commonos.BuildInstallCmd(pkgManager, yumMode, strings.Join(pkgs, " "), isRHEL8)
	r, _ := stressExecute(ctx, cmd, true, 5*time.Minute)
	if r != nil && r.GetExitCode() == 0 && s03ToolAvailable(ctx, "numactl") {
		return true, fmt.Sprintf("%s (%s)", pkgManager, strings.Join(pkgs, "+"))
	}

	// 逐包安装（部分环境合并安装会失败）
	for _, pkg := range pkgs {
		if s03ToolAvailable(ctx, "numactl") {
			break
		}
		if pkg != "numactl" && commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
			continue
		}
		one := commonos.BuildInstallCmd(pkgManager, yumMode, pkg, isRHEL8)
		r, _ = stressExecute(ctx, one, true, 5*time.Minute)
		if r != nil && r.GetExitCode() != 0 {
			ctx.Logger.Warn("[S-03] package %s install exit=%d", pkg, r.GetExitCode())
		}
	}
	if s03ToolAvailable(ctx, "numactl") {
		return true, fmt.Sprintf("%s (per-package)", pkgManager)
	}
	return false, ""
}

// s03VerifyNumactl 确认 numactl 命令可执行且能读取拓扑。
func s03VerifyNumactl(ctx *runner.StepContext) error {
	if !s03ToolAvailable(ctx, "numactl") {
		return fmt.Errorf("numactl not in PATH")
	}
	r, _ := stressExecute(ctx, "numactl --hardware 2>/dev/null | head -3", false, 15*time.Second)
	if r == nil || r.GetExitCode() != 0 || strings.TrimSpace(r.GetStdout()) == "" {
		return fmt.Errorf("numactl --hardware produced no output")
	}
	return nil
}

// s03InstallTool 检查工具是否已安装，未安装则通过包管理器安装；
// 返回 (是否可用, 安装来源描述)。
func s03InstallTool(ctx *runner.StepContext, tool, pkgManager, yumMode string, isRHEL8 bool) (bool, string) {
	if s03ToolAvailable(ctx, tool) {
		return true, "already-installed"
	}
	cmd := commonos.BuildInstallCmd(pkgManager, yumMode, tool, isRHEL8)
	r, _ := stressExecute(ctx, cmd, true, 5*time.Minute)
	if r != nil && r.GetExitCode() == 0 && s03ToolAvailable(ctx, tool) {
		return true, pkgManager
	}
	return false, ""
}

// s03GetVersion 获取工具版本字符串（best-effort，失败返回空）。
func s03GetVersion(ctx *runner.StepContext, tool string) string {
	// 常见版本 flag 优先级：--version > version > -V
	for _, flag := range []string{"--version", "version", "-V"} {
		r, _ := stressExecute(ctx,
			tool+" "+flag+" 2>&1 | head -1",
			false, 10*time.Second)
		if r != nil && r.GetExitCode() == 0 {
			line := strings.TrimSpace(r.GetStdout())
			if line != "" && !strings.Contains(strings.ToLower(line), "unknown") {
				return "(" + line + ")"
			}
		}
	}
	return ""
}

// ── fio 编译依赖与源码编译 ─────────────────────────────────────────────────────
// 参考：https://github.com/axboe/fio
// stressos 默认/常用 ioengine：libaio（数据文件）、sync/psync（logwrite 覆盖）、io_uring（可选）。
// libaio 引擎须在 configure 前安装 libaio-devel/libaio-dev，否则 CONFIG_LIBAIO 不会写入。

// s03FIORequiredEngines 为 S-07 源码安装后必须可用的 fio 引擎（sync/psync/io_uring 为 Linux 内置编译）。
var s03FIORequiredEngines = []string{"libaio", "sync", "psync", "io_uring"}

// s03InstallFIOBuildDeps 在装 fio 包或源码编译之前安装编译依赖；libaio-devel 为硬性要求。
func s03InstallFIOBuildDeps(ctx *runner.StepContext, pkgManager, yumMode string, isRHEL8 bool) error {
	deps := s03FIOBuildDepPackages(pkgManager)
	pkgNames := make([]string, len(deps))
	for i, d := range deps {
		pkgNames[i] = d.pkg
	}
	ctx.Logger.Info("[S-03] installing fio build dependencies (early): %v", pkgNames)

	var failedRequired []string
	for _, dep := range deps {
		if s03FIOBuildDepSatisfied(ctx, dep) {
			continue
		}
		cmd := commonos.BuildInstallCmd(pkgManager, yumMode, dep.pkg, isRHEL8)
		r, _ := stressExecute(ctx, cmd, true, 5*time.Minute)
		if r == nil || r.GetExitCode() != 0 {
			if dep.required {
				failedRequired = append(failedRequired, dep.pkg)
			}
			ctx.Logger.Warn("[S-03] fio build dep '%s' install failed", dep.pkg)
			continue
		}
		if dep.required && !s03FIOBuildDepSatisfied(ctx, dep) {
			failedRequired = append(failedRequired, dep.pkg)
		}
	}

	for _, must := range []string{"gcc", "make"} {
		if !s03ToolAvailable(ctx, must) {
			failedRequired = append(failedRequired, must)
		}
	}
	if !s03FIOHasLibaioHeaders(ctx) {
		if pkgManager == "apt" {
			failedRequired = append(failedRequired, "libaio-dev")
		} else {
			failedRequired = append(failedRequired, "libaio-devel")
		}
	}

	if len(failedRequired) > 0 {
		return fmt.Errorf("required fio build dependencies missing: %s", strings.Join(failedRequired, ", "))
	}
	return nil
}

type s03FIOBuildDep struct {
	pkg      string
	required bool // true=安装失败则整个 fio 编译路径失败
}

func s03FIOBuildDepPackages(pkgManager string) []s03FIOBuildDep {
	if pkgManager == "apt" {
		return []s03FIOBuildDep{
			{pkg: "gcc", required: true},
			{pkg: "make", required: true},
			{pkg: "libaio1", required: true},
			{pkg: "libaio-dev", required: true},
			{pkg: "zlib1g-dev", required: false},
			{pkg: "unzip", required: false},
		}
	}
	return []s03FIOBuildDep{
		{pkg: "gcc", required: true},
		{pkg: "make", required: true},
		{pkg: "libaio", required: true},
		{pkg: "libaio-devel", required: true},
		{pkg: "zlib-devel", required: false},
		{pkg: "unzip", required: false},
	}
}

func s03FIOBuildDepSatisfied(ctx *runner.StepContext, dep s03FIOBuildDep) bool {
	switch dep.pkg {
	case "libaio-devel", "libaio-dev":
		return s03FIOHasLibaioHeaders(ctx)
	case "libaio", "libaio1":
		return s03FIOPkgInstalled(ctx, dep.pkg)
	}
	// 开发包无独立命令，用 rpm/dpkg 探测
	switch dep.pkg {
	case "zlib-devel", "zlib1g-dev":
		return s03FIOPkgInstalled(ctx, dep.pkg)
	case "unzip":
		return s03ToolAvailable(ctx, "unzip")
	default:
		return s03ToolAvailable(ctx, dep.pkg)
	}
}

func s03FIOHasLibaioHeaders(ctx *runner.StepContext) bool {
	// 勿用 aio.h（glibc POSIX AIO），fio libaio 引擎需要 libaio.h（libaio-devel）。
	r, _ := stressExecute(ctx, `test -f /usr/include/libaio.h`, false, 10*time.Second)
	return r != nil && r.GetExitCode() == 0
}

func s03FIOPkgInstalled(ctx *runner.StepContext, pkg string) bool {
	r, _ := stressExecute(ctx, "rpm -q "+pkg+" >/dev/null 2>&1", false, 10*time.Second)
	if r != nil && r.GetExitCode() == 0 {
		return true
	}
	r, _ = stressExecute(ctx, "dpkg -s "+pkg+" >/dev/null 2>&1", false, 10*time.Second)
	return r != nil && r.GetExitCode() == 0
}

// s03VerifyFIOEngines 检查 fio 是否包含 stressos 需要的引擎。
func s03VerifyFIOEngines(ctx *runner.StepContext) (missing []string, err error) {
	if !s03ToolAvailable(ctx, "fio") {
		return nil, fmt.Errorf("fio not in PATH")
	}
	r, _ := stressExecute(ctx, "fio --enghelp 2>/dev/null", false, 15*time.Second)
	if r == nil || r.GetExitCode() != 0 {
		return nil, fmt.Errorf("fio --enghelp failed")
	}
	help := r.GetStdout() + r.GetStderr()
	for _, eng := range s03FIORequiredEngines {
		if !strings.Contains(help, eng) {
			missing = append(missing, eng)
		}
	}
	return missing, nil
}

// s03DistributeSourcePackage 将源码包分发到远端（-R 优先，否则 $HOME）：先查找，必要时上传。
func s03DistributeSourcePackage(ctx *runner.StepContext, localSrcPath string) (string, error) {
	name := filepath.Base(localSrcPath)
	remotePath, err := commonfile.FindAndDistribute(ctx, name, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir)
	if err != nil {
		return "", err
	}
	ctx.Logger.Info("[S-03] source package on remote: %s", remotePath)
	return remotePath, nil
}

// s03BuildFIOFromSource 从本地软件目录找到 fio 源码包，分发到 -R/$HOME 后编译安装。
func s03BuildFIOFromSource(ctx *runner.StepContext, pkgManager, yumMode string, isRHEL8 bool) error {
	localSrcPath, err := s03FindFIOSource(ctx)
	if err != nil {
		return fmt.Errorf("%w; place fio-<version>.tar.gz under --local-software-dirs", err)
	}

	remoteSrcPath, err := s03DistributeSourcePackage(ctx, localSrcPath)
	if err != nil {
		return fmt.Errorf("distribute fio source: %w", err)
	}

	if err := s03InstallFIOBuildDeps(ctx, pkgManager, yumMode, isRHEL8); err != nil {
		return fmt.Errorf("fio build dependencies: %w", err)
	}

	shQ := commonos.ShellSingleQuote

	// 勿 rm /tmp/fio-src-*：会与刚上传的 zip/tar 同名冲突；只清理历史解压目录
	cleanFio := "rm -rf /tmp/fio-3.* /tmp/fio-fio-* 2>/dev/null || true"
	var extractCmd string
	switch {
	case strings.HasSuffix(remoteSrcPath, ".zip"):
		extractCmd = fmt.Sprintf("%s\nunzip -oq %s -d /tmp/", cleanFio, shQ(remoteSrcPath))
	case strings.HasSuffix(remoteSrcPath, ".tar.bz2"):
		extractCmd = fmt.Sprintf("%s\ntar -xjf %s -C /tmp/", cleanFio, shQ(remoteSrcPath))
	default:
		extractCmd = fmt.Sprintf("%s\ntar -xzf %s -C /tmp/", cleanFio, shQ(remoteSrcPath))
	}

	// configure 自动探测 libaio/zlib/posix aio 等；须已装 libaio-devel 才会有 CONFIG_LIBAIO。
	// GitHub archive 解压后目录名为 fio-fio-<ver>，官方 snaps 为 fio-<ver>。
	script := fmt.Sprintf(`set -e
%s
SRC=$(find /tmp -maxdepth 1 -type d \( -name 'fio-[0-9]*' -o -name 'fio-fio-*' \) 2>/dev/null | head -1)
test -n "$SRC" || { echo "ERROR: cannot find fio source dir under /tmp"; exit 1; }
cd "$SRC"
echo "==> source dir: $SRC"
test -f /usr/include/libaio.h || {
  echo "ERROR: /usr/include/libaio.h missing; install libaio + libaio-devel before building fio"
  exit 1
}
echo "==> running configure (enable libaio, zlib, built-in sync/psync/io_uring on Linux)..."
./configure --prefix=/usr/local
grep -q 'CONFIG_LIBAIO' config-host.h || {
  echo "ERROR: configure did not enable libaio (CONFIG_LIBAIO missing); check config.log"
  exit 1
}
echo "==> configure summary (engines):"
grep -E 'Linux AIO|zlib|POSIX AIO' config.log 2>/dev/null | tail -5 || true
echo "==> running make..."
make -j$(nproc)
echo "==> running make install..."
make install
echo "==> fio engines (stressos required):"
fio --enghelp 2>&1 | head -25
for eng in libaio sync psync io_uring; do
  fio --enghelp 2>&1 | grep -qw "$eng" || { echo "ERROR: fio engine missing after build: $eng"; exit 1; }
done
rm -rf "$SRC"
echo "==> fio version:"
fio --version
`, extractCmd)

	buildTimeout := stressSourceBuildTimeout(ctx)
	stressLogPhase(ctx, "build-start", "fio from source timeout_cap="+fmt.Sprintf("%ds", int(buildTimeout.Seconds())))
	if _, err := stressRunShell(ctx, script, true, buildTimeout); err != nil {
		stressLogPhase(ctx, "build-fail", "fio from source: "+err.Error())
		return fmt.Errorf("build/install: %w", err)
	}
	stressLogPhase(ctx, "build-done", "fio installed to /usr/local/bin/fio")
	ctx.Logger.Info("[S-03] fio installed from source to /usr/local/bin/fio")
	return nil
}

// s03FindFIOSource 在 LocalSoftwareDirs 中查找 fio 源码包。
// 接受任意版本号（fio-3.35.tar.gz、fio-3.38.tar.gz 等）以及 GitHub archive 格式。
func s03FindFIOSource(ctx *runner.StepContext) (string, error) {
	searched := make([]string, 0, len(ctx.LocalSoftwareDirs))
	for _, dir := range ctx.LocalSoftwareDirs {
		searched = append(searched, dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// 匹配 fio-<version>.tar.gz / .tar.bz2 / .zip
			if strings.HasPrefix(name, "fio-") &&
				(strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.bz2") || strings.HasSuffix(name, ".zip")) {
				p := filepath.Join(dir, name)
				ctx.Logger.Info("[S-03] found fio source: %s", p)
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("fio source package not found (searched dirs: [%s], pattern: fio-*.tar.gz|*.zip)",
		strings.Join(searched, ", "))
}

// ── iperf3 源码编译 ────────────────────────────────────────────────────────────
// 参考：https://github.com/esnet/iperf
// iperf3 release 压缩包已包含预生成的 configure 脚本，Prerequisites: None，
// 最小依赖仅需 gcc + make，直接 ./configure && make && make install。
// 安装后命令名为 iperf3（不是 iperf），位于 /usr/local/bin/iperf3。

// s03BuildIPerfFromSource 从本地软件目录找到 iperf-3.*.tar.gz / iperf3-*.tar.gz，
// 上传后编译安装到 /usr/local/bin/iperf3。
func s03BuildIPerfFromSource(ctx *runner.StepContext, pkgManager, yumMode string, isRHEL8 bool) error {
	localSrcPath, err := s03FindIPerfSource(ctx)
	if err != nil {
		return fmt.Errorf("%w; place iperf-3.x.tar.gz under --local-software-dirs", err)
	}

	remoteSrcPath, err := s03DistributeSourcePackage(ctx, localSrcPath)
	if err != nil {
		return fmt.Errorf("distribute iperf3 source: %w", err)
	}

	// iperf3 Prerequisites: None（per README）。
	// 安装 gcc + make 即可，openssl-devel 可选（支持认证功能，缺失时自动 --without-openssl）。
	var buildDeps []string
	if pkgManager == "apt" {
		buildDeps = []string{"gcc", "make", "libssl-dev"}
	} else {
		buildDeps = []string{"gcc", "make", "openssl-devel"}
	}

	ctx.Logger.Info("[S-03] installing iperf3 build dependencies: %s", strings.Join(buildDeps, " "))
	for _, dep := range buildDeps {
		cmd := commonos.BuildInstallCmd(pkgManager, yumMode, dep, isRHEL8)
		r, _ := stressExecute(ctx, cmd, true, 5*time.Minute)
		if r == nil || r.GetExitCode() != 0 {
			ctx.Logger.Warn("[S-03] iperf3 build dep '%s' install failed (continuing)", dep)
		}
	}

	// gcc 和 make 是硬性要求
	for _, must := range []string{"gcc", "make"} {
		if !s03ToolAvailable(ctx, must) {
			return fmt.Errorf("required build tool '%s' not available; cannot compile iperf3 from source", must)
		}
	}

	shQ := commonos.ShellSingleQuote

	// release 压缩包解压后目录名为 iperf-3.x（如 iperf-3.18），
	// GitHub archive 也遵循相同约定。
	// 若 openssl 不可用，configure 自动回退 --without-openssl。
	cleanIPerf := "rm -rf /tmp/iperf-3.* 2>/dev/null || true"
	var extractIPerf string
	if strings.HasSuffix(remoteSrcPath, ".zip") {
		extractIPerf = fmt.Sprintf("%s\nunzip -oq %s -d /tmp/", cleanIPerf, shQ(remoteSrcPath))
	} else {
		extractIPerf = fmt.Sprintf("%s\ntar -xzf %s -C /tmp/", cleanIPerf, shQ(remoteSrcPath))
	}
	script := fmt.Sprintf(`set -e
%s
SRC=$(find /tmp -maxdepth 1 -type d -name 'iperf-3.*' 2>/dev/null | head -1)
test -n "$SRC" || { echo "ERROR: cannot find iperf-3.* source dir under /tmp"; exit 1; }
cd "$SRC"
echo "==> source dir: $SRC"
echo "==> running configure..."
./configure --prefix=/usr/local --without-openssl 2>/dev/null || ./configure --prefix=/usr/local
echo "==> running make..."
make -j$(nproc)
echo "==> running make install..."
make install
rm -rf "$SRC"
echo "==> iperf3 version:"
iperf3 --version
`, extractIPerf)

	buildTimeout := stressSourceBuildTimeout(ctx)
	stressLogPhase(ctx, "build-start", "iperf3 from source timeout_cap="+fmt.Sprintf("%ds", int(buildTimeout.Seconds())))
	if _, err := stressRunShell(ctx, script, true, buildTimeout); err != nil {
		stressLogPhase(ctx, "build-fail", "iperf3 from source: "+err.Error())
		return fmt.Errorf("build/install: %w", err)
	}
	stressLogPhase(ctx, "build-done", "iperf3 installed to /usr/local/bin/iperf3")
	ctx.Logger.Info("[S-03] iperf3 installed from source to /usr/local/bin/iperf3")
	return nil
}

// s03FindIPerfSource 在 LocalSoftwareDirs 中查找 iperf3 源码包。
// 接受官方命名（iperf-3.18.tar.gz）和 iperf3-*.tar.gz 两种格式。
func s03FindIPerfSource(ctx *runner.StepContext) (string, error) {
	searched := make([]string, 0, len(ctx.LocalSoftwareDirs))
	for _, dir := range ctx.LocalSoftwareDirs {
		searched = append(searched, dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// 匹配 iperf-3.*.tar.gz / .zip 或 iperf3-*
			if (strings.HasPrefix(name, "iperf-3.") || strings.HasPrefix(name, "iperf3-")) &&
				(strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip")) {
				p := filepath.Join(dir, name)
				ctx.Logger.Info("[S-03] found iperf3 source: %s", p)
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("iperf3 source package not found (searched dirs: [%s], pattern: iperf-3.x.tar.gz|zip)",
		strings.Join(searched, ", "))
}

// ── sysbench 源码编译 ──────────────────────────────────────────────────────────

// s03BuildSysbenchFromSource 从本地软件目录找到 sysbench-1.0.20 源码包，
// 上传后编译安装，完成后 ln -sf 到 /usr/local/bin/sysbench。
func s03BuildSysbenchFromSource(ctx *runner.StepContext, pkgManager, yumMode string, isRHEL8 bool) error {
	localSrcPath, err := s03FindSysbenchSource(ctx)
	if err != nil {
		return fmt.Errorf("%w; place sysbench-1.0.20.tar.gz or .zip under --local-software-dirs", err)
	}

	remoteSrcPath, err := s03DistributeSourcePackage(ctx, localSrcPath)
	if err != nil {
		return fmt.Errorf("distribute sysbench source: %w", err)
	}

	// 编译依赖：
	//   autoconf + m4   → autogen.sh 生成 configure 脚本（之前版本遗漏了这两项）
	//   automake        → 生成 Makefile.in
	//   libtool         → 共享库支持
	//   gcc + make      → 编译与链接
	//   pkg-config      → 探测依赖库
	//   openssl-devel / libssl-dev → TLS 支持（可选但 configure 默认探测）
	var buildDeps []string
	if pkgManager == "apt" {
		buildDeps = []string{
			"gcc", "make", "autoconf", "automake", "libtool", "m4",
			"pkg-config", "libssl-dev",
		}
	} else {
		buildDeps = []string{
			"gcc", "make", "autoconf", "automake", "libtool", "m4",
			"pkg-config", "openssl-devel",
		}
	}

	ctx.Logger.Info("[S-03] installing sysbench build dependencies: %s", strings.Join(buildDeps, " "))
	var failedBuildDeps []string
	for _, dep := range buildDeps {
		cmd := commonos.BuildInstallCmd(pkgManager, yumMode, dep, isRHEL8)
		r, _ := stressExecute(ctx, cmd, true, 5*time.Minute)
		if r == nil || r.GetExitCode() != 0 {
			failedBuildDeps = append(failedBuildDeps, dep)
			ctx.Logger.Warn("[S-03] build dep '%s' install failed (continuing)", dep)
		}
	}
	if len(failedBuildDeps) > 0 {
		ctx.Logger.Warn("[S-03] some build deps unavailable: %s; source build may fail",
			strings.Join(failedBuildDeps, ", "))
	}

	// 验证最关键的编译工具确实可用（autoconf 缺失会导致 autogen.sh 静默失败）
	for _, must := range []string{"gcc", "make", "autoconf", "automake"} {
		if !s03ToolAvailable(ctx, must) {
			return fmt.Errorf("required build tool '%s' not available after install; "+
				"cannot compile sysbench from source (ISO repos may lack this tool)", must)
		}
	}

	shQ := commonos.ShellSingleQuote

	// 对 zip 和 tar.gz 均解压到 /tmp/，通过 find 定位源码目录（容错 zip 内层目录名）。
	// 清理旧残留，避免 unzip 提示覆盖。
	var extractCmd string
	if strings.HasSuffix(remoteSrcPath, ".zip") {
		extractCmd = fmt.Sprintf(
			"rm -rf /tmp/sysbench-1.* 2>/dev/null || true\nunzip -oq %s -d /tmp/",
			shQ(remoteSrcPath))
	} else {
		extractCmd = fmt.Sprintf(
			"rm -rf /tmp/sysbench-1.* 2>/dev/null || true\ntar -xzf %s -C /tmp/",
			shQ(remoteSrcPath))
	}

	script := fmt.Sprintf(`set -e
%s
SRC=$(find /tmp -maxdepth 1 -type d -name 'sysbench-1.*' 2>/dev/null | head -1)
test -n "$SRC" || { echo "ERROR: cannot find sysbench-1.* source dir under /tmp"; exit 1; }
cd "$SRC"
echo "==> source dir: $SRC"
if [ -f autogen.sh ]; then
  echo "==> running autogen.sh..."
  ./autogen.sh
  echo "==> autogen.sh OK"
fi
echo "==> running configure..."
./configure --prefix=/usr/local/sysbench --without-mysql
echo "==> running make..."
make -j$(nproc)
echo "==> running make install..."
make install
ln -sf /usr/local/sysbench/bin/sysbench /usr/local/bin/sysbench 2>/dev/null || true
rm -rf "$SRC"
echo "==> sysbench version:"
sysbench --version
`, extractCmd)

	buildTimeout := stressSourceBuildTimeout(ctx)
	stressLogPhase(ctx, "build-start", "sysbench from source timeout_cap="+fmt.Sprintf("%ds", int(buildTimeout.Seconds())))
	if _, err := stressRunShell(ctx, script, true, buildTimeout); err != nil {
		stressLogPhase(ctx, "build-fail", "sysbench from source: "+err.Error())
		return fmt.Errorf("build/install: %w", err)
	}
	stressLogPhase(ctx, "build-done", "sysbench installed to /usr/local/sysbench")
	ctx.Logger.Info("[S-03] sysbench installed from source to /usr/local/sysbench")
	return nil
}

// s03FindSysbenchSource 在 LocalSoftwareDirs 中查找 sysbench-1.0.20 源码包。
func s03FindSysbenchSource(ctx *runner.StepContext) (string, error) {
	candidates := []string{
		"sysbench-1.0.20.tar.gz",
		"sysbench-1.0.20.zip",
		"sysbench-1.0.20.tar.bz2",
	}
	searched := make([]string, 0, len(ctx.LocalSoftwareDirs))
	for _, dir := range ctx.LocalSoftwareDirs {
		searched = append(searched, dir)
		for _, name := range candidates {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				ctx.Logger.Info("[S-03] found sysbench source: %s", p)
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("sysbench source package not found (searched dirs: [%s], candidates: %v)",
		strings.Join(searched, ", "), candidates)
}

// ── 辅助函数 ──────────────────────────────────────────────────────────────────

// s03ScanTools 扫描所有工具的安装状态，返回 (缺失列表, 已安装列表)。
func s03ScanTools(ctx *runner.StepContext) (missing, installed []string) {
	allTools := append(s03ToolNames(), "sysbench")
	for _, t := range allTools {
		if s03ToolAvailable(ctx, t) {
			installed = append(installed, t)
		} else {
			missing = append(missing, t)
		}
	}
	return
}

// s03ToolNames 返回 s03Tools 中所有工具的命令名（不含 sysbench）。
func s03ToolNames() []string {
	names := make([]string, 0, len(s03Tools))
	for _, t := range s03Tools {
		names = append(names, t.name)
	}
	return names
}

// s03Contains 检查字符串切片中是否包含指定元素。
func s03Contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
