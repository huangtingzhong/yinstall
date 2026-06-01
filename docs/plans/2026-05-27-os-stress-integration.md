---
title: OS 压测集成到 yinstall（CPU/MEM/NET/IO）
date: 2026-05-27
status: draft
owner: yihan
---

## 背景与目标

参考 `tpcc standard.md` 中“操作系统压测”章节，将 OS 压测能力集成到 `yinstall`，实现 **CPU / MEM / NET / IO** 四类压测的自动化：

- 自动安装依赖软件
- 自动执行压测
- 自动采集压测结果与运行时性能数据
- **YAC 多节点**模式下默认在**所有节点**执行并分别归档

约束：

- 输出到终端/错误信息保持英文（与现有 `yinstall` 一致）
- 使用现有 Step 框架与 SSH 执行模型（支持非 root + sudo）
- 压测产物与采集结果按主机归档到本地目录

## 设计概览

建议新增独立子命令，避免与 `collect` 混淆：

- `yinstall stress os ...`（推荐）

该子命令复用现有“连通性阶段 + per-host 执行阶段”的编排方式：

- Phase 1：连通性检查（一次性建连与验证）
- Phase 2：按主机执行压测步骤（YAC 模式下每台主机都执行）
- Phase 3：本地汇总与产物打包（生成 manifest/summary）

## CLI 设计（建议）

### 基础参数

- `-t, --targets <ip1,ip2,...>`：目标主机列表
- `--output-dir <dir>`：本地归档目录（默认 `~/.yinstall/stress/<timestamp>`）
- `--install-deps`：是否自动安装依赖（默认 `true`）
- `--deps-only`：仅安装依赖并退出（默认 `false`）
- `--cpu` / `--mem` / `--io` / `--net`：选择压测子集（默认 cpu+mem+io，net 可选）
- `--parallel-hosts <n>`：并发执行主机数（默认 1，避免同时打满多节点）

### 超时与时长

- `--stress-cmd-timeout <sec>`：单条命令最大执行时间（0=不限制）
- `--cpu-time <sec>`：CPU 压测时长（默认 60）
- `--mem-time <sec>`：内存压测时长（默认 30/60）
- `--io-time <sec>`：fio 压测时长（默认 300）

### CPU（sysbench）

- `--cpu-max-prime <n>`：默认 200000
- `--cpu-threads-single <n>`：默认 1
- `--cpu-threads-multi <n>`：默认 `nproc*2`

### MEM（sysbench memory + numactl 可选）

- `--mem-block-size <size>`：默认 `8K`
- `--mem-total-size <size>`：默认 `50G`（非绑核）
- `--mem-total-size-numa <size>`：默认 `10G`（绑核绑内存）
- `--mem-threads <n>`：默认 `nproc*2`
- `--mem-numa-bind`：是否执行 `numactl --membind=0 --cpunodebind=0` 场景（默认 true/可选）

### IO（fio）

与文档一致：典型 OLTP 场景 8K、70%读 30%写、direct=1、libaio。

- `--io-dir <path>`：fio 测试文件目录（默认 `/data/yashan`）
- `--io-size <size>`：默认 `30G`
- `--io-bs <size>`：默认 `8k`（可扩展 256k/1M 套餐）
- `--io-rwmixread <pct>`：默认 70
- `--io-iodepth-single <n>`：默认 1
- `--io-numjobs-single <n>`：默认 1
- `--io-iodepth-multi <n>`：默认 `nproc*2`
- `--io-numjobs-multi <n>`：默认 `nproc*2`
- `--io-direct <0|1>`：默认 1
- `--io-output-format <text|json>`：建议默认 `json`（便于汇总指标）

建议补充（更贴近数据库 IO 形态）：

- 除“8K 70/30 randrw”外，增加一组 **log-write 型测试**（小块同步写、低并发、关注延迟）：
  - `bs=4k|8k`，`rw=write`（或 `randwrite`），`iodepth=1`，`numjobs=1`
  - `fsync=1` 或 `fdatasync=1`（模拟每次提交刷盘的开销，关注 p95/p99）
- 增加一组 **pure randread**（8k randread）用于分离读性能上限，便于与 randrw 对比定位瓶颈。

安全护栏（必须做）：

- 执行 fio 前检查 `--io-dir` 所在挂载点可用空间（`df -k`），确保 `io-size` 不会打满磁盘（建议预留至少 10% 或固定阈值）。
- fio 测试文件默认清理（已在风险章节约束），并在清理失败时写入 warning 与清理建议。

### NET（可选）

NET 压测需要对端协同，建议先支持最小集合：

- `--ping-target <ip>`：执行 ping 延迟统计（按文档 awk 汇总）
- `--iperf-server <host>`：iperf3 server 所在主机（可为 targets 中某台）
- `--iperf-time <sec>`：默认 60
- `--iperf-parallel <n>`：默认 4
- `--iperf-json`：输出 JSON（默认 true）

## 依赖软件安装设计

目标：兼容常见 Linux（RHEL/CentOS/Anolis/Kylin/UOS 等），基于现有包管理探测逻辑（yum/dnf/apt）。

### 基于 ISO 的离线安装（复用 OS 模块能力）

优先复用现有 OS 模块中“挂载 ISO 并配置本地 repo”的能力（与 `yinstall os/db` 保持一致），核心目标是：

- 优先通过 **ISO 自带 repo** 离线安装依赖（yum/dnf/apt）
- 只在 ISO repo 不包含时，才走“本地包/源码编译”兜底

可直接引用的能力（示例）：

- `internal/common/os/EnsureLocalISORepo(ctx)`：挂载 ISO 并生成本地 yum 源（若项目中已有）
- `internal/common/os/GetPkgManager(ctx)` / `BuildInstallCmd(...)`：生成安装命令并执行

说明：

- “ISO 自带”指 ISO repo 中存在该包，而非系统默认已安装。
- 实现上依旧以 `command -v` 探测为准，缺失则尝试 repo 安装。

### 必需依赖（建议）

- `sysbench`：CPU/MEM 压测
- `fio`：IO 压测

### 推荐依赖（用于运行时指标采集）

- `sysstat`：提供 `iostat`/`mpstat`
- `dstat`：综合指标采集（若发行版无该包，可降级为 `vmstat`/`sar`/`pidstat`）
- `numactl`：NUMA 绑核/绑内存

### 可选依赖

- `iperf3`：网络吞吐（需要 server/client 协同）
- `netperf`：文档提到可补充（一期不强制）

### 安装策略（repo 优先 + 源码兜底）

1. `command -v <tool>` 探测是否已安装
2. 未安装则先确保 ISO 本地 repo 已配置（复用 OS 模块），再按 OS 包管理器安装
3. 若 repo 安装失败（包不存在/依赖缺失/源裁剪），按“源码安装或本地包安装”兜底（见下文 sysbench）
4. 通过 `--sudo` 与现有非 root 权限模型兼容（sudo -n）
5. 记录安装日志与版本信息到归档目录（repo vs source）

### sysbench（ISO repo 缺失时的源码安装方案）

动机：部分发行版 ISO repo 不包含 sysbench 或版本不可用，需提供稳定离线安装路径。

#### 1) 安装源代码编译依赖（优先 ISO repo）

在 RHEL 系发行版通常需要（按实际 OS 调整包名）：

- `gcc`, `make`, `automake`, `libtool`, `pkgconfig`
- `openssl-devel` / `libssl-dev`
- `lua-devel` / `liblua5.1-0-dev`（取决于 sysbench 版本）

实现上：

- 仍遵循 “command -v + repo install” 的策略安装这些编译依赖

#### 2) 搜索 sysbench 源码包（控制端）

支持从以下位置查找（按优先级）：

- `--local-software-dirs` 指定目录下的 `sysbench-1.0.20.tar.gz` 或 `sysbench-1.0.20.zip`
- 项目内置的软件目录（若后续要内置，可与 `collect embed` 同样方式做 embed 或随交付包分发）

查找成功后上传到目标机（统一走现有上传接口）。

#### 3) 目标机上源码编译安装（建议安装到固定前缀）

建议前缀：

- `/usr/local/sysbench`（或 `/opt/yinstall/sysbench`）

典型流程（示例，具体以 sysbench 版本为准）：

- 解压源码包到临时目录（如 `/tmp/sysbench-src-<ts>`）
- `./autogen.sh`（若需要）
- `./configure --prefix=/usr/local/sysbench`
- `make -j <nproc>`
- `make install`
- 建立软链接：`ln -sf /usr/local/sysbench/bin/sysbench /usr/local/bin/sysbench`

#### 4) 版本与可用性校验

- `sysbench --version`
- 运行最小用例：`sysbench cpu --threads=1 --time=1 run`

#### 5) 产物记录

在归档中写入：

- `deps/sysbench_install_method.txt`（repo/source）
- `deps/sysbench_version.txt`
- `deps/sysbench_build_log.txt`

## 自定义压测内容（参考 collect：内置自定义 + 外置自定义）

压测场景在不同客户/硬件/存储上差异很大，建议像 `yinstall collect` 一样引入“配置驱动”的扩展机制，使后续扩展压测项不需要改 Go 代码。

### 设计目标

- **内置自定义（built-in）**：二进制内置一套默认压测规则与脚本，开箱可用
- **外置自定义（external）**：用户通过 `--rules-file` 传入 YAML，追加规则或覆盖内置规则
- **YAC 多节点**：规则默认按 host 执行（per-host），确保在所有节点运行并分别归档
- **权限一致**：规则支持 `sudo` 与非 root 登录用户场景（与 `collect/db` 一致）

### 文件与目录（建议）

参考 collect 的 embed 目录布局：

```
internal/steps/stress/embed/
├── stress_rules.yaml
└── scripts/
    ├── shell/
    │   ├── runtime/          # iostat/dstat/mpstat/top/netstat/softirq 等
    │   ├── cpu/              # sysbench cpu 包装脚本（可选）
    │   ├── mem/              # sysbench memory 包装脚本（可选）
    │   ├── io/               # fio job 或包装脚本（建议输出 json）
    │   └── net/              # ping/iperf3 包装脚本
    └── fio/
        ├── randrw_8k_70r30w.fio
        ├── randread_8k.fio
        └── logwrite_4k_fsync1.fio
```

### CLI（建议）

- `--rules-file <path>`：外置规则文件；同 `id` 覆盖内置规则，新 `id` 追加
- `--rules-only`：仅执行规则，不执行内置的标准步骤（可选）
- `--list-rules`：打印当前合并后的规则列表（可选）

### 规则 YAML（建议 schema）

顶层：

- `version`: `"1"`
- `rules`: `[]rule`

每条 rule 字段建议包含：

- `id`：唯一标识（merge 覆盖的 key）
- `desc`：描述（ASCII/英文，终端输出用）
- `enabled`：是否启用
- `stage`：所属阶段（决定默认输出目录与执行顺序）
  - `deps` / `pre` / `runtime` / `cpu` / `mem` / `io` / `net` / `post`
- `type`：执行类型
  - `shell`：执行 shell（支持脚本 file/inline）
  - `fio`：执行 fio（建议走 file 方式 .fio 或包装脚本）
  - `sysbench`：执行 sysbench（可直接拼命令或走脚本）
- `source`：
  - `file`：从内置 `embed/scripts/...` 读取（或外置 file 路径）
  - `inline`：直接在 YAML 中写命令/脚本
- `path`：当 `source=file` 时脚本路径（相对 `scripts/` 或绝对路径）
- `content`：当 `source=inline` 时内容
- `timeout`：秒（0=用全局默认）
- `sudo`：是否用 sudo 执行（非 root 登录用户需要）
- `workdir`：远端工作目录（可选）
- `env`：环境变量键值（可选）
- `as_user`：以指定 OS 用户执行（可选；一般压测不需要，runtime 采集也不需要）
- `per_host`：是否按 host 执行（默认 true；若未来要做 cluster 汇总可设 false）
- `dest`：输出文件路径（相对于 `<host>/<stage>/`，支持子目录）

执行语义建议：

- 规则引擎按 `stage` 固定顺序执行（deps→pre→cpu/mem/io/net→runtime→post），同 stage 内按 YAML 顺序。
- `per_host=true`：每台 target host 都执行一遍（YAC 全节点默认）
- `per_host=false`：仅在首节点执行（极少用，主要用于汇总类）

### 输出归档约定

规则输出统一落到：

- `<output>/hosts/<host>/<stage>/<dest>`

同时记录：

- `hosts/<host>/rules/rules_applied.yaml`：实际合并后的规则（便于复盘）
- `hosts/<host>/rules/rules_report.json`：每条规则的 exit_code/duration/timeout 标记（便于汇总）

### 与现有内置步骤的关系

建议将“标准内置压测步骤”保持为主路径（可控、可读、默认安全），规则机制作为扩展层：

- 标准步骤负责：目录/安全检查/依赖安装/核心压测场景/清理/summary
- 规则机制负责：快速追加采集项或新增压测变体（无需改代码）

## 复用策略 / DRY 方案（建议）

目标：压测功能落地时尽量复用现有 `yinstall` 的基础设施，避免重复实现 SSH 执行、sudo、超时、ISO repo、归档与日志模式。

### 1) 直接复用（不新造轮子）

- **两阶段执行编排**：复用 `internal/cli/runner_host.go` 的 Phase 1（连通性）+ Phase 2（per-host）模式。
- **sudo / 非 root 登录用户模型**：复用 `internal/common/os/buildRunAsUserCommand` 及现有全局 `--sudo` 行为。
- **SSH session 级超时**：复用现有的 `ssh.Executor.ExecuteContext` + 适配器（collect 已验证的方案）。
- **上传通道**：统一走 `ctx.Executor.Upload(..., ctx.UploadContext())`（SFTP→SCP fallback 已实现），禁止在 stress 步骤里手写 scp。
- **ISO repo / 包管理器能力**：优先复用 OS 模块已有的：
  - ISO 挂载与本地 repo 配置（例如 `EnsureLocalISORepo`）
  - 包管理器探测与安装命令生成（`GetPkgManager` / `BuildInstallCmd` / `FilterUninstalledPackages`）
- **归档与 manifest/summary**：复用 collect 已有的“本地目录写文件 + manifest/summary 生成”的做法与结构。

### 2) 建议抽取成通用能力（避免 collect/stress 各写一套）

压测与采集都需要“配置驱动执行脚本并归档”的能力。为了 DRY，建议将目前 collect 的规则引擎演进为可复用组件：

#### 2.1 规则引擎抽取

将 `internal/steps/collect/rule_engine.go` 的核心能力抽到一个新包，例如：

- `internal/common/ruleengine/`

抽取内容：

- YAML schema 的通用字段（id/desc/enabled/type/source/path/content/timeout/sudo/per_host/dest）
- embedded rules + external rules 合并（同 id 覆盖、否则追加）
- 规则执行框架（逐条执行、记录 exit_code/duration/timeout、写 report）

保留在业务侧（collect/stress）：

- category/stage 到输出目录的映射（collect：db/os；stress：deps/pre/runtime/cpu/mem/io/net/post）
- 特定执行器（collect 有 sql/yasql；stress 有 fio/sysbench）

#### 2.2 “临时文件上传后执行”通用化

collect 已实现：

- SQL：本地写临时文件 → 上传 → 远端 `yasql -f` → 清理
- shell：本地写临时文件 → 上传 → 远端 `bash <file>` → 清理

建议抽到 `internal/common/os` 或 `internal/common/file` 下的通用函数，供 collect/stress 复用：

- `UploadTempAndRun(ctx, content, remotePrefix, runCmdTemplate, sudo, timeout)`
- 内部统一做：CreateTemp / Upload / chmod / Execute / cleanup / timeout exit-code 处理

这样 stress 的脚本执行、fio job 文件执行都能复用同一套逻辑。

### 3) stress 与 collect 的复用边界（避免耦合）

- **不复用**：collect 的 DB env 发现、yasql 执行逻辑（stress 默认不依赖 DB env）
- **可复用**：运行时采集脚本（iostat/dstat/mpstat/top/netstat/softirq）可直接在 stress 的 embed/scripts 中复用同一脚本内容或共享脚本目录
- **可复用**：日志与输出格式（英文终端输出、debug 详细复盘）

### 4) DRY 实施顺序（建议）

为降低改动风险，建议按顺序落地：

1. 先实现 stress 的最小可用版本：复用 runner_host + sudo + ISO repo + 上传执行（直接调用现有函数）
2. 当 stress 与 collect 都跑通后，再做“抽取 ruleengine/UploadTempAndRun”的重构（可控、可回归测试）


## Step 设计（建议）

建议新增 `internal/steps/stress/`（或 `internal/steps/osstress/`）目录，沿用 runner.Step 模型。

### Phase 1：连通性

- `S-01` Check connectivity

### Phase 2：逐主机步骤（YAC 全节点）

- `S-02` Init archive directory (per host)
- `S-03` Install dependencies (sysbench/fio/sysstat/dstat/numactl/iperf3)
- `S-04` Pre-snapshot collection (baseline)
  - `lscpu`, `free -m`, `uname -a`, `lsblk`, `df -h`, `mount`, `sysctl -a`（可筛选关键项）
- `S-05` CPU benchmark
  - sysbench 单线程 + 多线程（按文档口径）
- `S-06` MEM benchmark
  - sysbench memory（非绑核）+ 可选 numactl 场景
- `S-07` IO benchmark
  - fio 单任务/单队列 + 多任务/多队列（8k randrw 70/30）
  - 建议输出 JSON + 文本（优先 JSON，便于汇总 IOPS/BW/lat p95/p99）
  - 建议补充：8k randread、以及 log-write 型（4k/8k + fsync=1）测试
  - 测试文件应位于 `--io-dir`，执行前做空间检查，结束后默认清理（可 `--keep-io-files`）
- `S-08` NET benchmark（可选）
  - ping 延迟统计
  - iperf3（若启用则需 server/client 协同步骤）
- `S-09` Runtime metrics capture（建议与压测并行或前后采样）
  - `iostat -xk 1 4`
  - `dstat -cdlrgmnpsyp 1 3`
  - `top -b -n2 -d1`
  - `netstat -a -i -n`, `netstat -s`
  - `cat /proc/softirqs`
  - `cat /proc/net/softnet_stat`（按文档 awk 格式化）
  - 建议增加（数据库场景定位更有价值）：
    - CPU governor：`cpupower frequency-info` 或读取 `/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor`（尽力采集）
    - IO scheduler / queue：`cat /sys/block/*/queue/scheduler`、`nr_requests`、`read_ahead_kb`
    - THP：`/sys/kernel/mm/transparent_hugepage/enabled` + `/proc/meminfo` HugePages
    - NUMA：`numactl --hardware`（若存在）或 `/sys/devices/system/node/`
    - 网卡队列：`ethtool -l/-g`（若存在）与 `irqbalance` 状态（尽力采集）
- `S-10` Post-snapshot collection
- `S-11` Finalize (per host)

### Phase 3：本地汇总

生成：

- `manifest.json`：归档文件清单
- `summary.md`：按主机汇总关键指标

## 指标解析与结果汇总（建议）

### CPU（sysbench cpu）

从输出中提取：

- `events per second`
- `latency (avg/p95)`（如存在）

### MEM（sysbench memory）

从输出中提取：

- throughput（MiB/s）
- latency（如输出包含）

### IO（fio）

建议用 `--output-format=json`，从 JSON 提取：

- read/write IOPS
- read/write BW
- latency：p50/p95/p99（或 clat）
- util（如支持）

### NET（ping/iperf3）

- ping：min/avg/max/loss（按文档 awk 汇总）
- iperf3：bw、retrans、jitter（若 UDP）等（JSON）

### 运行时指标

保留原始输出文件，后续再迭代抽取：

- iostat：`r_await/w_await/%util` 等
- softirq：是否集中、是否过高（文档中 %soft>5% 风险提示）

## 归档目录结构（建议）

```
~/.yinstall/stress/<timestamp>/
├── manifest.json
├── summary.md
└── hosts/
    ├── <host1>/
    │   ├── deps/
    │   ├── pre/
    │   ├── runtime/
    │   ├── cpu/
    │   ├── mem/
    │   ├── io/
    │   └── net/
    └── <host2>/...
```

## 风险与安全

- 压测会对主机造成高负载，建议在压测前确认数据库/业务关闭或隔离
- fio 会写测试文件，必须：
  - 限制目录（`--io-dir`）
  - 明确 size
  - **压测前做可用空间检查并预留安全余量**（避免误把数据库数据盘打满）
  - 默认清理（可 `--keep-io-files` 保留以便复盘）
- iperf3 需要开放端口与双端协同，默认关闭，避免误操作

## 面向数据库压测环境的技术调整建议

本工具的目标环境主要是数据库压测机（TPCC 等）。为避免“压测结果好看但与数据库表现脱节”，建议做如下技术调整：

1. **IO 场景要覆盖 redo/commit 形态**
   - 仅做 8k randrw 70/30 不足以刻画 commit 延迟敏感路径
   - 建议增加 log-write 型（4k/8k、iodepth=1、fsync=1）并在 summary 中重点展示 p95/p99

2. **压测前后采集要能解释瓶颈**
   - 记录 CPU governor、THP、NUMA、IO scheduler、网卡队列/irqbalance 状态
   - 这些比单纯的 top 快照更利于解释“为什么性能低”

3. **避免 cache 影响与误操作**
   - fio 使用 `direct=1` 可减轻页缓存干扰；仍建议写明 “不要在生产/在线库执行”
   - 若后续要支持 drop_caches，必须做成显式开关（默认关闭）并打印高风险提示


## 实现路径（建议迭代）

### Iteration 1（最小可用）

- stress os 子命令
- deps install + CPU/MEM/IO 压测
- runtime 采集（iostat/mpstat/top/softirq 等）
- 本地归档 + summary.md（先做“原始输出汇总”，可后续再做结构化指标解析）

### Iteration 2（增强）

- fio JSON 解析与 summary 结构化指标输出
- ping + iperf3 支持（含 server/client 编排）
- 并发控制与更强的超时/中断处理

