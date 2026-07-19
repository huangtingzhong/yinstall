// s007_io_bench.go - IO 压测（fio 三场景）
// 运行三种 fio 模式：随机读写混合、随机读、顺序写（日志型）。
//
// 文件系统模式（默认）：--io-dir 下创建测试文件。
// 块设备模式：--io-device /dev/... --danger-io-device
//   - readwrite（默认）：randrw + randread + logwrite，会覆盖设备数据
//   - readonly：仅 randread，不写入
//
// IO 引擎：--io-engine 为全局默认；--io-engine-data / --io-engine-logwrite 分别覆盖数据文件与 redo；
// --io-engine-randrw / --io-engine-randread 可再细粒度覆盖。
// PreCheck：文件系统检查 df 空间；块设备检查 test -b、未挂载、容量 >= io-size*1.1。
package stressos

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepIoBench 返回 S-07 步骤：IO 压测。
func stepIoBench() *runner.Step {
	return &runner.Step{
		Name:     "IO benchmark (fio 3 scenarios)",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !getBool(ctx, "stress_io", true) {
				return fmt.Errorf("IO benchmark disabled (--io=false)")
			}
			if !s03ToolAvailable(ctx, "fio") {
				return fmt.Errorf("fio not available; install with --install-deps")
			}

			ioDevice := getStr(ctx, "io_device", "")
			if ioDevice != "" {
				// 块设备模式：必须显式开启危险开关
				if !getBool(ctx, "danger_io_device", false) {
					return fmt.Errorf("io-device mode is disabled; use --danger-io-device to enable (DANGEROUS)")
				}
				if !commonos.IsSafeUnixBlockDevicePath(ioDevice) {
					return fmt.Errorf("unsafe block device path: %s (must be under /dev/ and contain no shell metacharacters)", ioDevice)
				}
				mode := strings.ToLower(getStr(ctx, "io_device_mode", "readwrite"))
				if mode != "readonly" && mode != "readwrite" {
					return fmt.Errorf("invalid --io-device-mode=%s (must be readonly or readwrite)", mode)
				}

				// 必须是块设备节点
				r, _ := stressExecute(ctx, fmt.Sprintf("test -b %s", ioDevice), false, 10*time.Second)
				if r == nil || r.GetExitCode() != 0 {
					return fmt.Errorf("path is not a block device: %s (use lsblk to find the correct /dev/... path)", ioDevice)
				}

				if mode == "readwrite" {
					ctx.Logger.Warn("[S-07] io-device-mode=readwrite: fio WILL overwrite data on %s (randrw + logwrite)", ioDevice)
				}

				// 拒绝已挂载设备，避免破坏正在使用的文件系统
				// lsblk -no MOUNTPOINT <dev>：有输出表示已挂载
				mountR, _ := stressExecute(ctx, fmt.Sprintf("lsblk -no MOUNTPOINT %s 2>/dev/null | head -1", ioDevice), false, 10*time.Second)
				if mountR != nil && strings.TrimSpace(mountR.GetStdout()) != "" {
					return fmt.Errorf("block device %s appears to be mounted (mountpoint=%s); refusing to run fio on a mounted device",
						ioDevice, strings.TrimSpace(mountR.GetStdout()))
				}

				// 容量检查：device size >= io-size * 1.1
				ioSizeStr := getStr(ctx, "io_size", "10G")
				return s07CheckBlockDeviceSize(ctx, ioDevice, ioSizeStr)
			}

			// 文件系统模式：磁盘空间检查：可用空间 > io-size * 1.1
			ioDir := getStr(ctx, "io_dir", "/data/yashan")
			ioSizeStr := getStr(ctx, "io_size", "10G")
			return s07CheckDiskSpace(ctx, ioDir, ioSizeStr)
		},
		Action: func(ctx *runner.StepContext) error {
			hostDir := stressHostDir(ctx)
			ioResultDir := filepath.Join(hostDir, "io")

			ioDir := getStr(ctx, "io_dir", "/data/yashan")
			ioSize := getStr(ctx, "io_size", "10G")
			ioDirect := getInt(ctx, "io_direct", 1)
			ioTimeSec := getInt(ctx, "io_time", 120)
			keepFiles := getBool(ctx, "keep_io_files", false)
			ioDevice := getStr(ctx, "io_device", "")
			ioDeviceMode := strings.ToLower(getStr(ctx, "io_device_mode", "readwrite"))

			// IO 引擎：全局默认 + 按场景覆盖（数据文件 vs logwrite）
			engineRandrw := s07ResolveIOEngine(ctx, "randrw")
			engineRandread := s07ResolveIOEngine(ctx, "randread")
			engineLogwrite := s07ResolveIOEngine(ctx, "logwrite")
			iodepthCLI := getInt(ctx, "io_iodepth", 0) // 0=auto
			numjobsCLI := getInt(ctx, "io_numjobs", 0) // 0=auto
			rwmixRead := getInt(ctx, "io_rwmix_read", 70)
			ioFsync := getInt(ctx, "io_fsync", 1)
			bsRandrw := getStr(ctx, "io_bs_randrw", "8k")
			bsRandread := getStr(ctx, "io_bs_randread", "8k")
			bsLogwrite := getStr(ctx, "io_bs_logwrite", "4k")

			nprocInt := s07NprocInt(strconv.Itoa(s05CPUCount(ctx)))

			// iodepth/numjobs 自动计算（0 = auto）
			autoDepth := nprocInt * 2
			if autoDepth < 4 {
				autoDepth = 4
			}
			iodepthRW := iodepthCLI
			if iodepthRW <= 0 {
				iodepthRW = autoDepth
			}
			numjobsRW := numjobsCLI
			if numjobsRW <= 0 {
				numjobsRW = autoDepth
			}

			if ioDevice != "" {
				ctx.Logger.Info("[S-07] IO benchmark (block device): dev=%s mode=%s size=%s direct=%d duration=%ds",
					ioDevice, ioDeviceMode, ioSize, ioDirect, ioTimeSec)
				// 块设备模式强制 numjobs=1，避免多 job 对同一设备区域重叠写入
				numjobsRW = 1
			} else {
				ctx.Logger.Info("[S-07] IO benchmark (filesystem): dir=%s size=%s direct=%d duration=%ds",
					ioDir, ioSize, ioDirect, ioTimeSec)
			}
			ctx.Logger.Info("[S-07] randrw: engine=%s bs=%s iodepth=%d numjobs=%d rwmixread=%d%%",
				engineRandrw, bsRandrw, iodepthRW, numjobsRW, rwmixRead)
			ctx.Logger.Info("[S-07] randread: engine=%s bs=%s iodepth=%d numjobs=%d",
				engineRandread, bsRandread, iodepthRW, numjobsRW)
			ctx.Logger.Info("[S-07] logwrite: engine=%s bs=%s iodepth=1 numjobs=1 fsync=%d",
				engineLogwrite, bsLogwrite, ioFsync)

			// fio 公共运行时参数
			runtimeArgs := ""
			if ioTimeSec > 0 {
				runtimeArgs = fmt.Sprintf("--time_based --runtime=%d", ioTimeSec)
			}

			// 三组 fio 场景（每场景独立 bs/iodepth/numjobs）
			jobs := []struct {
				jobFile    string
				testFile   string
				resultBase string
				tplVars    map[string]string
			}{
				{
					// 随机读写混合：模拟 OLTP 数据文件 IO（读多写少）
					jobFile:    "randrw_8k_70r30w.fio",
					testFile:   filepath.Join(ioDir, "fio_test_randrw"),
					resultBase: "randrw",
					tplVars: map[string]string{
						"IOENGINE":  engineRandrw,
						"BS":        bsRandrw,
						"DIRECT":    strconv.Itoa(ioDirect),
						"IODEPTH":   strconv.Itoa(iodepthRW),
						"NUMJOBS":   strconv.Itoa(numjobsRW),
						"RWMIXREAD": strconv.Itoa(rwmixRead),
					},
				},
				{
					// 随机读：模拟数据库查询扫描 / buffer pool miss
					jobFile:    "randread_8k.fio",
					testFile:   filepath.Join(ioDir, "fio_test_randread"),
					resultBase: "randread",
					tplVars: map[string]string{
						"IOENGINE": engineRandread,
						"BS":       bsRandread,
						"DIRECT":   strconv.Itoa(ioDirect),
						"IODEPTH":  strconv.Itoa(iodepthRW),
						"NUMJOBS":  strconv.Itoa(numjobsRW),
					},
				},
				{
					// 顺序同步写：模拟 redo log / WAL 写入（iodepth=1 numjobs=1 fsync=N 固定）
					jobFile:    "logwrite_4k_fsync1.fio",
					testFile:   filepath.Join(ioDir, "fio_test_logwrite"),
					resultBase: "logwrite",
					tplVars: map[string]string{
						"IOENGINE": engineLogwrite,
						"BS":       bsLogwrite,
						"DIRECT":   strconv.Itoa(ioDirect),
						"IODEPTH":  "1", // redo log 写入必须单队列
						"NUMJOBS":  "1",
						"FSYNC":    strconv.Itoa(ioFsync),
					},
				},
			}

			// 块设备模式：filename 指向设备；readwrite 跑三场景（与文件系统模式一致）
			if ioDevice != "" {
				for i := range jobs {
					jobs[i].testFile = ioDevice
				}
				switch ioDeviceMode {
				case "readonly":
					// 仅 randread，不写入块设备
					jobs = []struct {
						jobFile    string
						testFile   string
						resultBase string
						tplVars    map[string]string
					}{jobs[1]}
					ctx.Logger.Info("[S-07] io-device-mode=readonly: randread only (no writes)")
				default: // readwrite
					ctx.Logger.Info("[S-07] io-device-mode=readwrite: randrw + randread + logwrite on %s (data will be overwritten)", ioDevice)
				}
			}

			plannedWall := len(jobs) * ioTimeSec
			modeLabel := "filesystem"
			if ioDevice != "" {
				modeLabel = "block:" + ioDeviceMode
			}
			stressLogPhase(ctx, "plan",
				fmt.Sprintf("scenarios=%d mode=%s duration_per=%ds (~%ds wall) keep_files=%v",
					len(jobs), modeLabel, ioTimeSec, plannedWall, keepFiles))

			for _, job := range jobs {
				iodepthStr := job.tplVars["IODEPTH"]
				numjobsStr := job.tplVars["NUMJOBS"]
				ctx.Logger.Info("[S-07] running fio job: %s (engine=%s bs=%s iodepth=%s numjobs=%s)",
					job.jobFile, job.tplVars["IOENGINE"], job.tplVars["BS"], iodepthStr, numjobsStr)

				jobContent, err := readEmbedFIOJob(job.jobFile)
				if err != nil {
					appendWarning(ctx, fmt.Sprintf("read fio template %s: %v", job.jobFile, err))
					continue
				}

				// 将 SIZE 和 FILENAME 加入替换表
				job.tplVars["SIZE"] = ioSize
				job.tplVars["FILENAME"] = strings.ReplaceAll(job.testFile, "'", `'\''`)

				jobContent = applyFIOTemplate(jobContent, job.tplVars)

				// fio 结果文件路径（目标机上）
				remoteJobFile := fmt.Sprintf("/tmp/stress_fio_%s_%d.fio", job.resultBase, time.Now().UnixNano())
				remoteJSONOut := fmt.Sprintf("/tmp/stress_fio_%s_%d.json", job.resultBase, time.Now().UnixNano())

				// 构造 fio 参数：JSON 输出 + 文本输出 + 运行时长
				fioArgs := fmt.Sprintf("--output-format=json --output=%s %s",
					remoteJSONOut, runtimeArgs)

				benchTimeout := stressBenchTimeout(time.Duration(ioTimeSec) * time.Second)
				if ioTimeSec <= 0 {
					benchTimeout = 2 * time.Hour // 无时长限制时给 2h 保护
				}

				target := job.testFile
				if ioDevice != "" {
					target = ioDevice + " [" + ioDeviceMode + "]"
				}
				stressLogPhase(ctx, "bench-start",
					fmt.Sprintf("scenario=%s job=%s target=%s engine=%s bs=%s iodepth=%s numjobs=%s size=%s timeout_cap=%ds",
						job.resultBase, job.jobFile, target, job.tplVars["IOENGINE"], job.tplVars["BS"],
						job.tplVars["IODEPTH"], job.tplVars["NUMJOBS"], ioSize, int(benchTimeout.Seconds())))

				wallStart := time.Now()
				txtOut, fioErr := stressRunFIO(ctx, jobContent, remoteJobFile, fioArgs, benchTimeout)
				wallDur := time.Since(wallStart)
				exitCode := 0
				if fioErr != nil {
					exitCode = -1
				}
				fioSummary := stressFIOSummary(txtOut)
				if fioErr != nil {
					stressLogPhase(ctx, "bench-fail",
						fmt.Sprintf("scenario=%s wall=%s exit=%d err=%v %s",
							job.resultBase, wallDur.Round(time.Millisecond), exitCode, fioErr, fioSummary))
				} else {
					stressLogPhase(ctx, "bench-done",
						fmt.Sprintf("scenario=%s wall=%s exit=%d %s",
							job.resultBase, wallDur.Round(time.Millisecond), exitCode, fioSummary))
				}

				// 保存文本输出
				txtDest := filepath.Join(ioResultDir, job.resultBase+".txt")
				header := fmt.Sprintf("=== fio %s target=%s engine=%s bs=%s direct=%d iodepth=%s numjobs=%s size=%s ===\n",
					job.jobFile, target, job.tplVars["IOENGINE"], job.tplVars["BS"], ioDirect,
					job.tplVars["IODEPTH"], job.tplVars["NUMJOBS"], ioSize)
				txtContent := header + txtOut
				if fioErr != nil {
					appendWarning(ctx, fmt.Sprintf("fio %s failed: %v", job.jobFile, fioErr))
					txtContent += "\nERROR: " + fioErr.Error()
				}
				if err2 := writeTextFile(txtDest, txtContent+"\n"); err2 != nil {
					appendWarning(ctx, "write "+txtDest+": "+err2.Error())
				}

				// 下载 JSON 输出（fio --output）
				if fioErr == nil {
					jsonDest := filepath.Join(ioResultDir, job.resultBase+".json")
					s07DownloadFIOJSON(ctx, remoteJSONOut, jsonDest)
				}

				// 清理测试文件
				if !keepFiles && ioDevice == "" {
					_, _ = stressExecute(ctx, fmt.Sprintf("rm -f %s %s",
						strings.ReplaceAll(job.testFile, "'", `'\''`), remoteJSONOut), false, 30*time.Second)
				} else if !keepFiles && ioDevice != "" {
					// 块设备模式不做 rm -f filename，只清理远端 JSON 文件
					_, _ = stressExecute(ctx, fmt.Sprintf("rm -f %s", remoteJSONOut), false, 30*time.Second)
				}
			}

			ctx.Logger.Info("[S-07] IO benchmark completed, results in %s", ioResultDir)
			return nil
		},
	}
}

// s07ResolveIOEngine 按场景解析 fio ioengine。
// 优先级（randrw/randread）：--io-engine-<scenario> > --io-engine-data > --io-engine
// logwrite：--io-engine-logwrite > --io-engine
// scenario: randrw | randread | logwrite
func s07ResolveIOEngine(ctx *runner.StepContext, scenario string) string {
	base := strings.TrimSpace(getStr(ctx, "io_engine", "libaio"))
	if base == "" {
		base = "libaio"
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(getStr(ctx, k, "")); v != "" {
				return v
			}
		}
		return base
	}
	switch scenario {
	case "randrw":
		return pick("io_engine_randrw", "io_engine_data")
	case "randread":
		return pick("io_engine_randread", "io_engine_data")
	case "logwrite":
		return pick("io_engine_logwrite")
	default:
		return base
	}
}

// s07CheckBlockDeviceSize 检查块设备容量是否足够（>= ioSize * 1.1）。
func s07CheckBlockDeviceSize(ctx *runner.StepContext, device, ioSizeStr string) error {
	requiredKB, err := s07ParseSizeToKB(ioSizeStr)
	if err != nil {
		ctx.Logger.Warn("[S-07] cannot parse --io-size=%s, skipping block device size check: %v", ioSizeStr, err)
		return nil
	}

	// 使用 blockdev 获取字节数（可能需要 root，但本工具通常具备 sudo）
	r, _ := stressExecute(ctx,
		fmt.Sprintf("blockdev --getsize64 %s 2>/dev/null || lsblk -nbdo SIZE %s 2>/dev/null | head -1", device, device),
		true, 10*time.Second)
	if r == nil || r.GetExitCode() != 0 {
		ctx.Logger.Warn("[S-07] cannot determine size of block device %s, continuing anyway", device)
		return nil
	}
	sizeBytesStr := strings.TrimSpace(r.GetStdout())
	sizeBytes, err := strconv.ParseInt(sizeBytesStr, 10, 64)
	if err != nil {
		ctx.Logger.Warn("[S-07] cannot parse block device size bytes=%q, continuing anyway", sizeBytesStr)
		return nil
	}

	availKB := sizeBytes / 1024
	neededKB := int64(float64(requiredKB) * 1.1)
	if availKB < neededKB {
		return fmt.Errorf("block device size insufficient: dev=%s size=%dG required=%dG (io-size=%s *1.1)",
			device, availKB/1024/1024, neededKB/1024/1024, ioSizeStr)
	}
	ctx.Logger.Info("[S-07] block device size check passed: dev=%s size=%dG needed=%dG",
		device, availKB/1024/1024, neededKB/1024/1024)
	return nil
}

// s07CheckDiskSpace 检查 ioDir 所在分区的可用空间是否足够（>= ioSize * 1.1）。
func s07CheckDiskSpace(ctx *runner.StepContext, ioDir, ioSizeStr string) error {
	// 解析 io-size 为 KB
	requiredKB, err := s07ParseSizeToKB(ioSizeStr)
	if err != nil {
		ctx.Logger.Warn("[S-07] cannot parse --io-size=%s, skipping disk space check: %v", ioSizeStr, err)
		return nil
	}

	// df -k --output=avail <dir>（GNU df）
	r, _ := stressExecute(ctx,
		fmt.Sprintf("df -k --output=avail %s 2>/dev/null | tail -1 || df -k %s 2>/dev/null | awk 'NR==2{print $4}'",
			ioDir, ioDir),
		false, 10*time.Second)
	if r == nil || r.GetExitCode() != 0 {
		ctx.Logger.Warn("[S-07] cannot determine available disk space in %s, continuing anyway", ioDir)
		return nil
	}

	availStr := strings.TrimSpace(r.GetStdout())
	availKB, err := strconv.ParseInt(availStr, 10, 64)
	if err != nil {
		ctx.Logger.Warn("[S-07] cannot parse available KB=%q, continuing anyway", availStr)
		return nil
	}

	// 需要 10% 额外空间
	neededKB := int64(float64(requiredKB) * 1.1)
	if availKB < neededKB {
		return fmt.Errorf("disk space insufficient in %s: available=%dG required=%dG (io-size=%s *1.1)",
			ioDir, availKB/1024/1024, neededKB/1024/1024, ioSizeStr)
	}

	ctx.Logger.Info("[S-07] disk space check passed: available=%dG needed=%dG in %s",
		availKB/1024/1024, neededKB/1024/1024, ioDir)
	return nil
}

// s07ParseSizeToKB 将 "10G", "1024M", "102400K" 等大小字符串解析为 KB。
func s07ParseSizeToKB(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(strings.ToUpper(sizeStr))
	if len(sizeStr) == 0 {
		return 0, fmt.Errorf("empty size string")
	}
	unit := sizeStr[len(sizeStr)-1]
	numStr := sizeStr[:len(sizeStr)-1]
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size number %q: %w", numStr, err)
	}
	switch unit {
	case 'G':
		return int64(num * 1024 * 1024), nil
	case 'M':
		return int64(num * 1024), nil
	case 'K':
		return int64(num), nil
	default:
		return 0, fmt.Errorf("unknown size unit %q", unit)
	}
}

// s07NprocInt 将 nproc 字符串转为 int（最大 16，最小 1）。
func s07NprocInt(nproc string) int {
	n, err := strconv.Atoi(strings.TrimSpace(nproc))
	if err != nil || n <= 0 {
		return 4
	}
	if n > 16 {
		return 16
	}
	return n
}

// s07DownloadFIOJSON 从目标机拉取 fio JSON 输出文件（通过读取 stdout）。
func s07DownloadFIOJSON(ctx *runner.StepContext, remotePath, localDest string) {
	r, err := stressExecute(ctx, fmt.Sprintf("cat %s 2>/dev/null", remotePath), false, 10*time.Second)
	if err != nil || r == nil || r.GetExitCode() != 0 {
		return
	}
	content := r.GetStdout()
	if content == "" {
		return
	}
	if err2 := writeTextFile(localDest, content); err2 != nil {
		appendWarning(ctx, "write fio json "+localDest+": "+err2.Error())
	}
}
