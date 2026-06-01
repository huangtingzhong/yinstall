# 安装与运行环境信息采集归档 — 需求说明

| 项目 | 内容 |
|------|------|
| 文档状态 | 🎨 Designing（需求澄清） |
| 版本 | v0.18 |
| 日期 | 2026-05-26 |
| 关联产品 | yinstall（YashanDB 安装自动化 CLI） |

---

## 1. 背景与目标

### 1.1 背景

使用 yinstall 完成 OS 基线、数据库（单机/YAC）、备库等安装后，现场会分散在：

- 控制端：session/debug 日志、`yinstall` 命令行参数；
- 目标机：sysctl/limits、防火墙、磁盘与网络、产品用户、yasboot 集群配置、数据库目录与进程、systemd 自启等。

运维交付、审计复盘、故障排查、扩容/迁移对照时，需要一份**结构化、可检索、可长期存档**的「安装与环境快照」，而不是仅依赖零散日志。

### 1.2 目标

1. **安装后自动归档（可选）**：`yinstall db`（及后续 `standby`）成功结束后，可选触发一次全量采集并落盘。
2. **独立采集（必选能力）**：对**已运行**的一套环境，不重新安装，仅执行 `yinstall collect` 完成采集。
3. **内容完整**：覆盖 OS 配置、安装路径与版本、集群/库参数、网络与存储、进程与端口、本次安装元数据。
4. **安全合规**：密码、密钥、token 等敏感信息**不得明文**写入归档包；与现有 `LogErrorExit` 脱敏规则一致。
5. **多节点**：YAC/多 target 场景按节点分目录汇总，并有一份集群级索引。
6. **时段日志**：支持 `--db-log-since/until` 按时间窗采集数据库侧日志（§5.16.B）。
7. **Profile 组合**：通过 `--profile` 按 **CAT 分类**预置或逗号组合采集范围（§3.2～§3.3）。
8. **Linux 兼容**：目标端 OS/架构探测与分发行版采集策略**与 `yinstall db` 安装一致**（§3.11）。
9. **编码规范**：实现前全量扫描现有代码、**DRY 复用**、**代码注释中文**、**终端/归档输出仅英文**、终端日志模式与 db 安装一致（§3.12、§12）。

### 1.3 范围边界

**纳入采集：**

- OS 基线相关配置（B 步骤产物）
- YashanDB 安装与运行信息（C 步骤 / `yinstall db`）
- 备库扩容相关信息（`yinstall standby`，二期 profile）

**明确不纳入：**

- **YMP**（`yinstall ymp` / H 步骤）
- **YCM**（`yinstall ycm` / G 步骤）

上述产品另有自身配置与交付方式，本需求不提供对应 `profile`，也不在 `full` 模式中探测或归档其目录、进程与端口。

### 1.4 非目标（首期可不实现）

- 替代监控/CMDB 的持续采集与告警。
- 修改目标机配置（采集为**只读**）。
- 将归档包自动上传对象存储（可作为二期集成点，首期仅本地目录）。
- YMP/YCM 信息采集（见 §1.3）。

---

## 2. 用户场景

| ID | 场景 | 触发方式 | 期望结果 |
|----|------|----------|----------|
| UC-01 | 数据库安装交付存档 | `yinstall db ... --archive-on-success` | 安装成功后在指定目录生成 `inventory-<run-id>/` |
| UC-02 | 历史环境补录 | `yinstall collect -t 10.10.10.130 -u root` | 自动发现 env/集群名/端口，不跑安装步骤，仅采集并落盘 |
| UC-03 | 安装前基线（可选二期） | `yinstall collect --profile baseline` | 仅 OS + 磁盘/网络，供前后对比 |
| UC-04 | 审计抽查 | 交付物为 JSON + 人类可读摘要 + 校验清单 | 审计员可对照「应采集项」检查完整性 |
| UC-05 | Agent/自动化 | 结构化 JSON schema 稳定 | 下游系统解析 `manifest.json` |
| UC-06 | YAC 集群交付存档 | `yinstall collect -t n1,n2,n3 -u root` | 各节点 `hosts/*` + 顶层 `yac/`、`cluster.json` |
| UC-07 | 故障时段日志取证 | `yinstall collect ... --db-log-since ... --db-log-until ...` | `hosts/*/db/logs/` 仅含指定时间段日志 |

---

## 3. 功能范围

### 3.1 子命令/参数（建议）

```
# 独立采集（顶层子命令 collect，不再使用 inventory collect）
yinstall collect -t <hosts> [-u root] [全局 SSH/日志参数]
  --profile <name>[,<name>...]         # 采集 profile，逗号组合取并集（见 §3.2～§3.3）；默认 full
  --os-user yashan                      # 产品用户（默认 yashan）；DB 相关步骤以该用户探测 env
  -o, --output-dir <dir>                # 归档根目录，默认 ./inventory/<timestamp>_<run-id>
  --include-logs                        # 是否打包本次 yinstall session/debug（控制端）
  --redact=strict|none                  # 默认 strict

  # 数据库日志采集（R-034；须指定时间窗，见 §5.16.B）
  --db-log-since <time>                 # 日志起始时间（含）；与 --db-log-until 至少传一个
  --db-log-until <time>                 # 日志结束时间（含）
  --db-log-timezone <tz>                # 解析上述时间的时区，默认 Asia/Shanghai
  --db-log-max-mb <n>                   # 单节点日志落盘上限（MB），默认 500，超出记 warnings

  # 以下为可选覆盖（独立采集通常不需要；见 §3.9 自动发现）
  --env-file <path>                     # 显式指定产品用户 env 包装文件（如 ~/.port3988）
  --cluster-name <name>                 # 覆盖自动解析的 yasboot 集群名
  --db-begin-port <port>                # 覆盖自动解析的起始端口（1688 等）

# 安装后挂钩（db 子命令扩展）
yinstall db ... --archive-on-success
yinstall db ... --archive-dir <dir>     # 与 --archive-on-success 联用
```

**典型独立采集（无需 cluster-name / db-begin-port）：**

```bash
yinstall collect -t 10.10.10.130 -u root
yinstall collect -t 10.10.10.130 -u root --profile db-core
yinstall collect -t 10.10.10.130 -u root --profile db-core,network
yinstall collect -t 10.10.10.130 -u root --profile baseline
yinstall collect -t h1,h2,h3 -u root --profile yac
# 故障时段库日志（profile db-logs 或 full + 时间参数）
yinstall collect -t 10.10.10.130 -u root --profile db-logs \
  --db-log-since "2026-05-18 09:00:00" --db-log-until "2026-05-18 18:00:00"
# YAC + 同时段日志
yinstall collect -t h1,h2,h3 -u root --profile yac \
  --db-log-since "2026-05-18T09:00:00+08:00" \
  --db-log-until "2026-05-18T18:00:00+08:00"
```

独立采集时，**R-004** 会在各目标机上自动定位产品用户的 env 包装文件（`~/.bashrc`、`~/.port<port>`、`~/.yasboot/.../conf/*.bashrc`），`source` 后解析 **集群名、起始端口、YASDB_HOME / 数据路径** 等，供 R-020～R-027 使用（与 standby 的 `GetPrimaryEnvFile` + `SyncPrimaryClusterNameFromEnvFile` 同源逻辑，collect 侧增强「零参数」多实例探测）。

**与现有能力关系：**

- 采集逻辑复用 `runner.Step` / `StepContext` / SSH 执行器，与 **B/C 安装步骤同构**（PreCheck → Action → PostCheck；只读 Action）。
- **目标端 Linux 兼容**：R-001 复用 **B-001** 的 `DetectOSInfo`；各 OS 采集步按 `ctx.OSInfo` 分发行版分支，与 B/C 安装共用 `internal/common/os`（§3.11）。
- **编码约束**：注释中文；终端/错误/归档文本仅 ASCII 英文；日志与 `yinstall db` 同模式（§3.12）；复用清单见 **§12**。
- 代码：`internal/cli/collect.go` + `internal/steps/collect/registry.go`（每步一文件，命名如 `r010_host_identity.go`）。
- 步骤 ID 前缀 **`R-`**（**R**ecord / collect **R**un），与 B/C/E/G/H 不冲突。
- 详细采集字段见 **§5**；**§3.2** 为采集**分类**；**§3.3** 为 **profile 组合**；**§3.4～§3.7** 为步骤模型与 R- 注册表。

全局 flag（collect 子命令支持的步骤管理参数；**不含** `--precheck`）：

```
-l, --list-steps              列出本 profile 下全部 R- 步骤
-s, --include-steps R-010-R-019   仅执行指定步骤（支持范围、逗号）
-e, --exclude-steps R-028     排除步骤（与 -s 同时出现时 exclude 优先）
     --dry-run                 跳过 Action/PostCheck，仅校验步骤计划（不读远端）
-f, --force-steps R-026       强制重采（覆盖已有归档片段，见各步说明）
-F, --force                   强制全部步骤
```

> **说明**：root 上的 `--precheck` 对 **`yinstall collect` 无效**（实现时忽略或报错提示）。采集为只读任务，无需「仅跑 PreCheck」模式；各 R- 步内部的 **PreCheck 函数**仍随正常执行调用（Optional 步失败 → skipped）。

示例：

```bash
yinstall collect -t 10.10.10.130 -u root --profile full -l
yinstall collect -t 10.10.10.130 -u root -s R-001,R-010-R-019,R-020-R-027
yinstall collect -t 10.10.10.130 -u root --profile os -e R-020-R-029
yinstall collect -t h1,h2 -u root -s R-023
```

### 3.2 采集项分类（CAT）

采集项按**业务域**划分为 **CAT-*** 分类（category）。每个 R- 步骤归属一个主分类；profile 由若干 **CAT** 或预置 **profile 名** 组合而成。`manifest.json` 写入 `categories[]` 与 `profile`（见 §6.1）。

| 分类 ID | 名称 | 包含 R- 步骤 | 主要归档位置 | 说明 |
|---------|------|-------------|-------------|------|
| **CAT-META** | 元数据与连通 | R-001, R-002, R-029 | 根目录、`hosts/*/meta.json` | SSH、目录初始化、manifest/summary |
| **CAT-HOOK** | 安装挂钩快照 | R-003 | `install-run.json`, `install-params.json` | 仅 `--archive-on-success` |
| **CAT-DB-ENV** | 产品环境发现 | R-004 | `db/env-discovery.json` | env/集群名/端口自动发现 |
| **CAT-HW** | 主机硬件 | R-010, R-011 | `os/identity/`, `os/dmidecode/` | 含 dmidecode（R-011 Optional） |
| **CAT-OS-USER** | 用户与 limits | R-012 | `os/user-limits.json` | 产品用户、组、limits |
| **CAT-KERNEL** | 内核参数 | R-013, R-031†, R-032† | `os/kernel/` | 全量 sysctl/proc-sys/grub |
| **CAT-OS-TIME** | 时间/NTP | R-014 | `os/time/` | Optional |
| **CAT-NET** | 网络与绑核 | R-015, R-016, R-033† | `os/network/` | 网卡/bond/team、路由、**IRQ/RPS/XPS** |
| **CAT-OS-SEC** | 防火墙 | R-017 | `os/firewall.txt` | Optional |
| **CAT-OS-PKG** | 软件包/YUM | R-018 | `os/packages-rpm.txt`, `os/yum-repos/` | Optional |
| **CAT-STORAGE** | 存储与共享盘 | R-019 | `os/storage/` | LVM、multipath、diskgroup、YAC 共享盘 |
| **CAT-DB-PATH** | DB 路径与版本 | R-020 | `db/paths.json` | home/data/log/stage |
| **CAT-DB-CONFIG** | DB 配置与漂移 | R-021, R-027 | `db/config/`, `db/config-drift.json` | toml、漂移（R-027 Optional） |
| **CAT-DB-DATA** | DB 文件系统资产 | R-022 | `db/filesystem/` | 数据文件/REDO/归档目录清单 |
| **CAT-DB-RUN** | DB 运行态 | R-023, R-024, R-025 | `db/cluster-status.txt`, `db/processes.txt`, … | 集群状态、进程/端口/绑核、自启 |
| **CAT-DB-SQL** | 库内 SQL | R-026 | `db/sql/` | sysdba 全量参数/数据文件视图（Optional） |
| **CAT-DB-LOG** | DB 时段日志 | R-034 | `db/logs/` | **须** `--db-log-since/until` |
| **CAT-YAC** | YAC 集群汇总 | R-030 | `yac/`, `cluster.json` | 非 YAC → skipped |
| **CAT-AUDIT** | 控制端日志 | R-028 | 根目录 logs/ | **须** `--include-logs` |
| **CAT-STANDBY** | 备库（二期） | R-401～R-403 | `hosts/*/standby/` | standby profile |

† R-031～R-033 为 Phase 2+ 可选增强，纳入 `full` 时可 `-s R-031` 显式启用。

**分类依赖（实现时 PreCheck 顺序提示）：**

```
CAT-META → CAT-DB-ENV → CAT-DB-* 
CAT-META → CAT-HW / CAT-KERNEL / CAT-NET / CAT-STORAGE（可并行）
CAT-DB-CONFIG + CAT-DB-DATA + CAT-DB-SQL → CAT-DB-CONFIG(R-027 漂移)
CAT-DB-* + 多节点 → CAT-YAC（R-030 汇总）
```

---

### 3.3 Profile 定义与组合

**Profile** 是面向场景的**预置步骤包**，每个 profile 对应一组 **CAT**（展开为 R- 步骤列表）。用户通过 `--profile` 选择采集范围，无需记忆 R- 编号。

#### 3.3.1 内置 Profile 一览

| Profile | 包含分类（CAT） | 展开 R- 步骤（摘要） | 典型场景 |
|---------|----------------|---------------------|----------|
| **`full`** | 除 CAT-HOOK、CAT-STANDBY 外**全部** | R-001～R-029 + R-030‡ + R-034§ | 安装交付、全量存档（**默认**） |
| **`os`** | META + HW + OS-USER + KERNEL + OS-TIME + NET + OS-SEC + OS-PKG + STORAGE | R-001,002,010～019,029 | OS 基线审计 |
| **`db`** | META + DB-ENV + HW‹› + OS-USER + STORAGE + 全部 CAT-DB-* + YAC‡ | R-001,002,004,010～012,019,020～027,030‡,034§,029 | DBA 关注库与必要 OS |
| **`baseline`** | META + HW + STORAGE + NET（不含 §5.8.2 绑核细节） | R-001,002,010,011,015,016,019,029 | UC-03 安装前基线 |
| **`network`** | META + NET | R-001,002,015,016,029 | 网络/bond/VIP 专项 |
| **`hardware`** | META + HW | R-001,002,010,011,029 | 资产/维保对照 |
| **`kernel`** | META + KERNEL | R-001,002,013,029 | 内核参数专项 |
| **`storage`** | META + STORAGE | R-001,002,019,029 | 磁盘/LVM/multipath 专项 |
| **`db-core`** | META + DB-ENV + DB-PATH + DB-CONFIG + DB-DATA + DB-SQL | R-001,002,004,020～022,026,027,029 | 核心资产（配置+文件+SQL） |
| **`db-runtime`** | META + DB-ENV + DB-RUN | R-001,002,004,023～025,029 | 运行健康检查 |
| **`db-logs`** | META + DB-ENV + DB-LOG | R-001,002,004,034§,029 | UC-07 故障时段日志 |
| **`yac`** | **`db`** 或 **`full`** + **CAT-YAC**（等价于 full 且强调 R-030） | 同 full + 确保 R-030 | YAC 集群交付 |
| **`minimal`** | META（仅连通+初始化+收尾） | R-001,002,029 | 连通性探测 |
| **`standby`** | **`db`** + CAT-STANDBY | db + R-401～403 | 备库环境（二期） |

‡ **R-030**：YAC 环境才执行，否则 skipped。  
§ **R-034**：须 `--db-log-since` 和/或 `--db-log-until`，否则 skipped。  
‹› **`db` profile OS 精简**：仅 CAT-HW 的 R-010～R-012 + CAT-STORAGE，**不含** CAT-KERNEL、CAT-OS-TIME、CAT-OS-SEC、CAT-OS-PKG 及 NET 的 R-016（路由/hosts 等）。

#### 3.3.2 Profile 组合规则

| 规则 | 说明 |
|------|------|
| **语法** | `--profile db-core,network` → 取各 profile 步骤集的**并集**（去重） |
| **顺序** | 并集后仍按 registry 默认顺序执行 |
| **与 `-s`/`-e`** | 先展开 profile 得基础集合 → 再 `-s` 收窄 → 再 `-e` 排除（exclude 优先） |
| **默认** | 未指定 `--profile` 时等价 **`full`** |
| **冲突** | `minimal,full` 等价 `full`（取并集，非交集） |
| **条件步骤** | R-030/R-034/R-028/R-003 仍按各自 PreCheck（YAC/时间窗/include-logs/挂钩）决定 skipped |
| **`-l`** | `yinstall collect -l --profile db-core,network` 列出并集后的步骤目录 |

**实现**：`internal/cli/collect_profiles.go` 定义 `profileDefinitions map[string][]string`（profile 名 → CAT ID 列表）与 `expandCategoriesToSteps(cats []string) []*Step`；`collect.go` 解析逗号分隔 profile 后合并。

#### 3.3.3 Profile 与分类对照（速查）

|  | META | HOOK | DB-ENV | HW | OS-USER | KERNEL | OS-TIME | NET | OS-SEC | OS-PKG | STORAGE | DB-PATH | DB-CFG | DB-DATA | DB-RUN | DB-SQL | DB-LOG | YAC | AUDIT | STBY |
|--|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **full** | ✓ | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ○§ | ○‡ | ○ | |
| **os** | ✓ | | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | | | | | | | | | |
| **db** | ✓ | | ✓ | ◑ | ✓ | | | ◑ | | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ○§ | ○‡ | | |
| **baseline** | ✓ | | | ✓ | | | | ◑ | | | ✓ | | | | | | | | | |
| **db-core** | ✓ | | ✓ | | | | | | | | | ✓ | ✓ | ✓ | | ✓ | | | | |
| **db-runtime** | ✓ | | ✓ | | | | | | | | | | | | ✓ | | | | | |
| **db-logs** | ✓ | | ✓ | | | | | | | | | | | | | | ○§ | | | |

◑ = 精简子集（见上表脚注）；○ = 条件触发；§ = 须时间参数；‡ = 须 YAC。

#### 3.3.4 示例

```bash
yinstall collect -l --profile full
yinstall collect -t 10.10.10.130 -u root --profile os
yinstall collect -t 10.10.10.130 -u root --profile db-core
yinstall collect -t h1,h2,h3 -u root --profile yac
yinstall collect -t 10.10.10.130 -u root --profile db-core,storage
yinstall collect -t 10.10.10.130 -u root --profile db-logs \
  --db-log-since "2026-05-18 09:00:00" --db-log-until "2026-05-18 18:00:00"
# profile 基础上再收窄
yinstall collect -t 10.10.10.130 -u root --profile full -e R-011,R-018
```

---

### 3.4 步骤模型（与 B/C 对齐）

每个 R- 步骤为 `*runner.Step`，字段语义与安装一致：

| 属性 | 说明 |
|------|------|
| `ID` | `R-0xx` / `R-4xx`，全局唯一 |
| `Name` / `Description` | 终端与 `-l` 展示 |
| `Tags` | 与 **CAT-*** 一致的主分类 tag（如 `cat-net`, `cat-db-sql`）+ 辅助 tag；profile 由 CAT 展开，**首期**以 profile 名与 R- ID 过滤，CAT tag 过滤二期 |
| `Optional` | `true` 时 PreCheck 失败 → **skipped**（非 failed），同 B-004/H-003 |
| `PerHost` | `all`：每个 `-t` 目标执行；`first`：仅首节点；`local`：仅控制端 |
| PreCheck | 只读：工具存在、目录可访问、库进程/yasql 可用等 |
| Action | 只读采集 + 写入 `--output-dir` 下文件 |
| PostCheck | 验证输出文件非空或 JSON schema |

**步骤结果**写入 `manifest.json` → `steps[]`：`{ "id": "R-015", "host": "...", "status": "success|skipped|failed", "duration_ms", "artifacts": [...] }`。

**挂钩安装**：`yinstall db ... --archive-on-success` 在 DB 步骤成功后追加执行 filter 后的 R- 列表，并启用 **R-003**（安装入参/步骤摘要）。

### 3.5 步骤注册表（R-001～R-029 + R-030 + R-034，Phase 1）

| ID | 分类 | 名称 | PerHost | Optional | 默认 Profile | 对应 §5 | 主要产物 |
|----|------|------|---------|----------|--------------|---------|----------|
| **R-001** | CAT-META | Check Connectivity | all | 否 | 全部 | — | SSH + **OS 探测**（同 B-001，`ctx.OSInfo`） |
| **R-002** | CAT-META | Init Host Archive Dir | all | 否 | 全部 | §5.1 | `meta.json` 骨架 |
| **R-003** | CAT-HOOK | Snapshot Install Run | local | **是** | 挂钩 | §5.2～§5.3 | `install-run.json` |
| **R-004** | CAT-DB-ENV | Discover Product Environment | all | 否 | full,db,… | §5.12.1 | `db/env-discovery.json` |
| **R-010** | CAT-HW | Host Identity | all | 否 | 含 HW 的 profile | §5.4 | `os/identity/` |
| **R-011** | CAT-HW | Collect DMI (dmidecode) | all | **是** | 含 HW 的 profile | §5.4.1 | `os/dmidecode/` |
| **R-012** | CAT-OS-USER | Product User & Limits | all | 否 | os,full,db | §5.5 | `os/user-limits.json` |
| **R-013** | CAT-KERNEL | Kernel Parameters Full | all | 否 | os,full,kernel | §5.6.1 | `os/kernel/` |
| **R-014** | CAT-OS-TIME | Time & NTP | all | **是** | os,full | §5.7 | `os/time/` |
| **R-015** | CAT-NET | Network Interfaces & Bonding | all | 否 | os,full,network,… | §5.8.1, §5.8.2 | `os/network/` |
| **R-016** | CAT-NET | Network Routes DNS Hosts Ports | all | 否 | os,full,network | §5.8 | routes, hosts, ports |
| **R-017** | CAT-OS-SEC | Firewall Status | all | **是** | os,full | §5.9 | `os/firewall.txt` |
| **R-018** | CAT-OS-PKG | Packages & YUM Repos | all | **是** | os,full | §5.10 | packages, yum-repos |
| **R-019** | CAT-STORAGE | Storage LVM Mounts Multipath | all | 否 | os,full,db,storage,… | §5.11 | `os/storage/` |
| **R-020** | CAT-DB-PATH | DB Paths Version Env | all | 否 | 含 DB 的 profile | §5.12 | `db/paths.json` |
| **R-021** | CAT-DB-CONFIG | DB Config Files | all | 否 | 含 DB 的 profile | §5.13.1.A | `db/config/` |
| **R-022** | CAT-DB-DATA | DB Filesystem Layout | all | 否 | 含 DB 的 profile | §5.13.1.B | `db/filesystem/` |
| **R-023** | CAT-DB-RUN | DB Cluster Status | first | 否 | 含 DB 的 profile | §5.13 | `cluster-status.txt` |
| **R-024** | CAT-DB-RUN | DB Processes & Ports | all | 否 | 含 DB 的 profile | §5.14, §5.14.1 | processes, 绑核 |
| **R-025** | CAT-DB-RUN | DB Autostart & ArchDG | all | **是** | 含 DB 的 profile | §5.13/§5.14 | autostart, archdg |
| **R-026** | CAT-DB-SQL | DB SQL Catalog (sysdba) | all | **是** | db-core,full,db | §5.13.1.C | `db/sql/` |
| **R-027** | CAT-DB-CONFIG | DB Config Drift Check | first | **是** | db-core,full,db | §5.13.1 | `config-drift.json` |
| **R-034** | CAT-DB-LOG | Collect DB Logs | all | **是** | db-logs,full† | §5.16.B | `db/logs/` |
| **R-030** | CAT-YAC | Collect YAC Cluster Info | local | **是** | yac,full,db‡ | §5.17 | `yac/`, `cluster.json` |
| **R-028** | CAT-AUDIT | Collect Session Logs | local | **是** | 全部◊ | §5.16.A | session/debug |
| **R-029** | CAT-META | Finalize Manifest & Summary | local | 否 | 全部 | §6 | `manifest.json`, `summary.md` |

† full 含 R-034 步骤位，无时间参数时 skipped。 ‡ YAC 时执行。 ◊ 须 `--include-logs`。

**执行顺序**：registry 切片顺序即默认顺序。**R-004** 须在 **R-020～R-027** 之前。**R-034** 在 R-027 之后（依赖 R-004/R-020 日志路径）。**R-030** 插在 **R-034 与 R-028** 之间。**R-029 必须最后**。

### 3.6 步骤注册表（备库，Phase 2）

| ID | 名称 | PerHost | Optional | Profile | 说明 |
|----|------|---------|----------|---------|------|
| **R-401** | Standby Primary Link Info | all | 否 | standby | §5.15 主库连接（脱敏） |
| **R-402** | Standby Expansion Config | all | 否 | standby | gen-config 产出清单 |
| **R-403** | Standby Sync Status | first | **是** | standby | 同步状态 SQL/ yasboot 摘要 |

### 3.7 步骤注册表（可选增强，Phase 2+）

| ID | 名称 | Optional | 说明 |
|----|------|----------|------|
| **R-031** | Kernel Module Parameters | 是 | §5.6.1 `sys-module-parameters/` |
| **R-032** | Kernel Compile Config | 是 | §5.6.1 `/boot/config-*` |
| **R-033** | Network Topology Graph | 是 | §5.8.1 `network/topology.json` |
| **R-035** | Systemd Journal Snippet | 是 | §5.16 journalctl 无时间窗时的固定行数片段（**R-034 已含带时间窗 journal**） |

### 3.8 步骤管理能力（与 B/C 对齐）

采集步骤复用 `internal/cli/steps_util.go` 的 `filterSteps` / `parseStepRanges`，以及 `runner.RunStep` 的 skip/optional/dry-run/force 语义（**不含** root `--precheck` 模式）。

| 全局 flag | collect 行为 | 示例 |
|-----------|--------------|------|
| `-l` / `--list-steps` | 按 profile 打印 R- 步骤目录（ID、顺序、Optional、Description、Tags） | `yinstall collect -l --profile full` |
| `-s` / `--include-steps` | 仅执行 listed ID 或范围 | `-s R-020-R-027` 仅 DB 采集 |
| `-e` / `--exclude-steps` | 从 profile 默认列表中排除；**与 `-s` 同时指定同一 ID 时 exclude 优先** | `-e R-026` 跳过库内 SQL |
| `--dry-run` | 跳过 Action/PostCheck，打印将执行的步骤与目标节点 | 预览采集计划 |
| `-f` / `--force-steps` | 指定步强制重采（覆盖 `--output-dir` 下已有片段） | `-f R-021` 重读 toml |
| `-F` / `--force` | 全部步骤 force | 整包重采 |

**Optional 步骤**（R-003、R-011、R-014～R-018、R-025～R-028、**R-034**、**R-030**、R-031～R-035、R-403）：PreCheck 失败 → 状态 **`skipped`**，不导致整次 collect 失败（除非 `--strict`，二期）。

**PerHost 语义**：

| PerHost | 含义 | 示例 |
|---------|------|------|
| `all` | 每个 `-t` 目标各执行一次 | R-004、R-010～R-026、**R-034** |
| `first` | 仅首个 target（YAC 集群级命令） | R-023、R-027 |
| `local` | 仅控制端，不 SSH | R-003、R-028、**R-030**、R-029 |

**实现约定**（与 B/C 同构）：

- 注册：`internal/steps/collect/registry.go` → `GetAllSteps()` 含 R-034（R-027 后）、R-030（R-034 后）、R-028、R-029。
- 单步文件：`internal/steps/collect/r010_host_identity.go`（ID 与文件名前缀一致）。
- CLI：`internal/cli/collect.go` + `collect_profiles.go` 解析 `--profile` → 展开 CAT → `filterSteps` → `RunStep`。
- `PrintCollectStepCatalog(profiles []string)`：按 profile 并集列出步骤；见 `internal/cli/step_catalog.go`。
- Profile 展开：`internal/cli/collect_profiles.go`。

**编号分段（便于记忆与 `-s` 范围）**：

| 段 | ID 范围 | 分类 |
|----|---------|------|
| 元数据 / 连通 | R-001～R-004 | SSH、归档目录、安装挂钩快照、**产品 env 自动发现** |
| OS / 主机 | R-010～R-019 | 身份、DMI、内核、网络、存储… |
| 数据库 | R-020～R-027 | 路径、配置、文件系统、集群、SQL、漂移 |
| YAC 汇总 | **R-030** | 集群级 VIP/SCAN/diskgroup/YFS/一致性（§5.17） |
| DB 日志 | **R-034** | 按时间窗采集库/yasboot/systemd 日志（§5.16.B） |
| 收尾 | R-028～R-029 | yinstall session 日志、manifest |
| 可选增强 | R-031～R-035 | 内核/网络/日志补充 |
| 备库 | R-401～R-403 | standby profile |

### 3.9 产品环境自动发现（独立采集零参数）

**目标**：`yinstall collect -t 10.10.10.130 -u root` 即可采集已安装库，**不必**手工传 `--cluster-name`、`--db-begin-port`；与现场运维习惯一致——登录产品用户后 `source` 的 env 文件即权威来源。

**执行步骤**：**R-004 Discover Product Environment**（profile 为 `full` / `db` / `standby` 时纳入；`os` profile 跳过）。

**探测顺序**（每节点、以 `--os-user` 家目录为根；复用并扩展 `standby.GetPrimaryEnvFile` / `ClusterNameFromEnvFileContent`）：

| 优先级 | 条件 | 使用的 env 文件 |
|--------|------|-----------------|
| 1 | CLI 指定 `--env-file` | 绝对路径，或相对产品用户家目录 |
| 2 | CLI 指定 `--cluster-name` | `~/.yasboot/<cluster>_yasdb_home/conf/<cluster>.bashrc`（存在则用） |
| 3 | CLI 指定 `--db-begin-port` | `DetermineEnvFile(home, port)` → `~/.bashrc` 或 `~/.port<port>` |
| 4 | **自动（默认）** | 见下表 |

**自动探测（优先级 4 细则）**：

1. 枚举 `~/.port*` 包装文件；解析每文件 `source ...yasboot...bashrc` 行，得到候选 `(env_wrapper, cluster_name, port_hint)`。
2. 检查 `~/.bashrc` 是否含 yasboot `source` 行（默认端口 1688 场景）。
3. 扫描 `~/.yasboot/*/conf/*.bashrc` 直连集群 env。
4. **多实例消歧**（同一主机多套库）：
   - 优先：存在 **运行中** `yasdb`/`yasagent` 且其环境/监听端口与候选一致；
   - 其次：仅 **一个** 有效候选 → 采用；
   - 否则：**失败**并列出候选，提示 `--env-file` / `--db-begin-port` / `--cluster-name`。
5. 对选定 **包装文件** 执行（与 C-031/C-033 相同执行模型）：
   ```bash
   su - <os_user> -c 'source <env_wrapper> && env'
   ```
   采集 `YASDB_HOME`、`YASCS_HOME`、`YASOM_HOME`、`PATH` 等（**不含密码**）。
6. 从包装文件内容 **反解析** `db_cluster_name`（如 `yashandb_3988`）；`db_begin_port` 取自：
   - 包装文件名 `~/.port3988` → `3988`；
   - 或集群名后缀 `_3988`；
   - 或 `source` 后 env / `yashandb.toml` 中的 `begin_port`；
   - 否则默认 `1688`。
7. 将 `env_file`（包装文件路径）、`db_cluster_name`、`db_begin_port` 及推导出的 `db_home_path` / `db_data_path` / `db_log_path` / `db_stage_dir` 写入：
   - `ctx.Results["env_file"]`（供 R-026 等下游，同 `resolveDBEnvFile` 约定）；
   - `ctx.Params[...]`（供 R-020～R-027）；
   - `hosts/<host>/db/env-discovery.json`（审计用，含探测路径与消歧理由）。

**CLI 覆盖语义**（均为可选，非必填）：

| 参数 | 作用 |
|------|------|
| `--env-file` | 跳过自动枚举，固定包装文件；解析失败则 R-004 **failed** |
| `--cluster-name` | 仅在使用优先级 2 或辅助消歧时使用；未指定时由 env 内容反解析 |
| `--db-begin-port` | 仅在使用优先级 3 或辅助消歧时使用；未指定时由文件名/集群名/env 反解析 |

**挂钩安装**（`yinstall db ... --archive-on-success`）：若安装 run 已写入 `Results["env_file"]` 与 `Params`（C-024 等），R-004 **优先采用安装产物**，并与远端现状校验；不一致时在 `env-discovery.json` 记 `drift_from_install`。

**下游约定**：R-020 及以后凡需 `yasboot -c`、路径、`yasql` 的步骤，**不得**再假设 CLI 必传 cluster/port；统一读 R-004 写入的 `ctx.Results` / `ctx.Params`。

### 3.10 YAC 环境识别与多节点采集

**YAC 判定**（满足任一即 `yac_mode: true`，启用 **R-030** 及 §5.17；否则 R-030 **skipped**）：

| 信号 | 说明 |
|------|------|
| CLI 多 target | `-t h1,h2,...` 节点数 ≥ 2 |
| tom l / 集群状态 | R-021 中 `nodes.length > 1`，或 R-023 `yasboot cluster status` 显示多实例 |
| 挂钩安装 | `install-params.json` 中 `yac_mode: true` |

**采集方式**：

1. **逐节点**：R-001～R-027 对每个 `-t` 各执行（`PerHost: all` 的步骤），产物在 `hosts/<host>/`。
2. **集群级汇总**：**R-030** 在控制端合并各节点产物 + R-023 状态，写入 `yac/` 与 **`cluster.json`**。
3. **无需 YAC 专用 CLI 参数**：VIP/SCAN/diskgroup/互联网段等从 tom l、`/etc/hosts`、`yasboot`、§5.11 存储采集**反解析**；挂钩安装时与 `install-params.json` 对照。

```bash
yinstall collect -t 192.168.1.11,192.168.1.12,192.168.1.13 -u root
```

`summary.md` labels environment as `YAC (N nodes)` or `Standalone` (English only; see section 3.12.3).

### 3.11 目标端 Linux 兼容性（与 yinstall db 一致）

采集在**已安装 YashanDB 的常见 Linux 目标机**上运行，OS 探测、分发行版分支与容错原则与 **`yinstall db` / `yinstall os` 安装链路相同**，复用现有 `internal/common/os` 与 **B-001** 逻辑，**不单独维护**一套 OS 适配表。

#### 3.11.1 支持范围（与 installer.md / B-001 对齐）

| 维度 | 支持项 | 说明 |
|------|--------|------|
| **发行版** | RHEL 7.x；RHEL / OL / CentOS / Rocky / Alma **8+**；**麒麟 V10**；**统信 UOS 20** | 与 `DetectOSType()` 及 installer 已验证矩阵一致 |
| **架构** | `x86_64`、`aarch64`（ARM） | `uname -m`；与 DB 安装包架构一致 |
| **包管理** | `yum`、`dnf`、`apt`（探测，采集只读） | `ctx.OSInfo.PkgManager` |
| **权限** | root 或具备 **免密 sudo** 的 SSH 用户 | 同 B-001：无 root/免密 sudo 时记 `warnings[]`，部分项 skipped |
| **控制端** | macOS / Linux / Windows 运行 `yinstall collect` | 仅控制端；**目标端必须是 Linux**（SSH 远端） |

未在官方兼容性矩阵内的发行版：**不阻断**整次 collect；R-001 写入 `os.family: other`，各步按「通用 Linux」尽力采集，缺失项记 `errors[]`/`skipped`。

#### 3.11.2 OS 探测（R-001 = B-001 同构）

**R-001** 与 **B-001 Check Connectivity** 对齐（可复用 `StepB001CheckConnectivity` 的 PreCheck 或抽取共享函数）：

1. SSH 连通 + 基础工具链（`cat`/`grep`/`awk`/`sed`）。
2. **`commonos.DetectOSInfo(ctx)`** → 填充 `ctx.OSInfo`（`/etc/os-release`、`uname -r`、`uname -m`、`DetectOSType`、`detectPkgManager`）。
3. root / `sudo -n` 探测（同 B-001 `PC.OS.SUDO` 警告语义）。
4. 写入 `hosts/<host>/meta.json`：

```json
{
  "os": {
    "name": "Oracle Linux Server",
    "id": "ol",
    "version_id": "8.8",
    "kernel": "5.15.0-...",
    "arch": "aarch64",
    "family": "rhel8",
    "is_rhel7": false,
    "is_rhel8": true,
    "is_kylin": false,
    "is_uos": false,
    "pkg_manager": "dnf"
  }
}
```

`family` 枚举：`rhel7` | `rhel8` | `kylin` | `uos` | `other`（与安装步骤分支一致）。

**下游约定**：凡 CAT-OS / CAT-DB 步骤通过 `ctx.OSInfo` / `ctx.ForHost` 子上下文读取 OS 信息，**禁止**在 collect 内硬编码「仅 RHEL8」而不走 `IsRHEL7`/`IsRHEL8`/`IsKylin`/`IsUOS` 判断。

#### 3.11.3 分发行版采集策略（与 B/C 步骤对齐）

各采集步 Action 按 OS 家族选择命令/路径（**只读**），与安装步骤使用**同一套分支**：

| 采集步 / CAT | RHEL7 | RHEL8 / OL8+ / 麒麟 V10 | UOS | 通用 / 缺失时 |
|--------------|-------|-------------------------|-----|----------------|
| **R-013** 内核/grub | `grubby --info=ALL` | `grub2-editenv list`、`/etc/default/grub` | 同 RHEL8 分支 | 跳过缺失文件，记 `collection_notes` |
| **R-015/016** 网络 | `network-scripts/ifcfg-*`、`/proc/net/bonding/*` | **优先** `nmcli` + `NetworkManager`；兼容 ifcfg | 同 RHEL8 | `ip link`/`ip addr` 全量仍执行（不依赖 NM） |
| **R-017** 防火墙 | `firewall-cmd` 或 `iptables -L` | `firewall-cmd --list-all` | 同左 | 服务不存在 → Optional skip |
| **R-018** 软件包 | `rpm -qa`、`yum repolist` | `rpm -qa`、`dnf repolist` | `rpm -qa` | `apt` 系：`dpkg -l`（若 `PkgManager=apt`） |
| **R-019** 存储/multipath | `multipath -ll`、`/etc/udev/rules.d/` | 同左；device-mapper 路径 | 同左 | multipath 未装 → 跳过 multipath 段 |
| **R-004～R-027** DB | 产品用户、`source env`、`yasboot`、`yasql` | **与 OS 无关**（与 C 步相同） | 同左 | 库未装/未起 → Optional skip |
| **R-034** journal | `journalctl`（systemd） | 同左 | 同左 | 无 systemd → 仅文件日志 |

**Bond/Team**：RHEL7 以 **bond + ifcfg** 为主；RHEL8+ 以 **NetworkManager / teamdctl** 为主——与 §5.8.1 及 **B-021/B-022** 安装文档一致，采集脚本按 `ctx.OSInfo` 选择优先路径，**两种配置均尝试**（失败不阻断）。

**YAC 共享盘 / multipath**：采集逻辑与 **C-012 / B-024** 相同数据源（`multipath -ll`、diskgroup 参数），不按 OS 分叉。

#### 3.11.4 容错与验证（同安装）

| 原则 | collect 行为 | 对照安装 |
|------|-------------|----------|
| 单项命令失败 | 记 `errors[]`，继续下一项 | 同 Optional 步 / 非致命 PreCheck |
| 工具不存在（dmidecode、nmcli、teamdctl） | 对应 R- 步 **skipped** 或子文件缺失 | 同 B-004 Optional |
| 未知 OS | R-001 success + `family: other`；OS 中性项仍采 | B-001 仍完成连通 |
| 架构不匹配 | 不属 collect 职责；R-020 记录实际 `uname -m` | 安装包选择在 db install |
| 多节点 YAC | 每节点独立 `ctx.OSInfo`（允许混部同构集群） | 同 db YAC |

#### 3.11.5 实现约束

| 项 | 要求 |
|----|------|
| 代码复用 | `internal/common/os/os.go`（`DetectOSInfo`、`DetectOSType`、`IsRHEL*`、`IsKylin`、`IsUOS`、`GetPkgManager`、`GetArch`） |
| R-001 | 复用或镜像 `internal/steps/os/b001_check_connectivity.go` |
| 分支参考 | `b008_write_sysctl.go`、`b014_write_yum_repo.go`、`b015_install_deps.go`、`b024_install_multipath.go`、`precheck_network_validate.go` |
| DB 采集 | `ExecuteAsUserWithEnvCheck`、`resolveDBEnvFile`、`FindLatest*` 等与 **C 步**相同 |
| 测试 | 至少在 **OL8 x86_64、OL8 aarch64、麒麟 V10** 各一例集成采集（与安装验证矩阵一致） |

### 3.12 编码前约束：DRY、输出语言、终端日志

> **编码前必须先完成 §12 全量代码扫描**，确认可复用模块与禁止重复实现的边界；collect 不得另起 SSH/步骤/日志/脱敏栈。

#### 3.12.1 DRY 原则

| 层级 | 要求 |
|------|------|
| **步骤框架** | 复用 `runner.Step` / `StepContext` / `RunStep`；**禁止**自建 collect 专用执行循环 |
| **CLI 编排** | 镜像 `internal/cli/db.go` / `os.go` 两阶段：Phase 1 连通（R-001）→ Phase 2 其余 R- 步；复用 `createExecutor`、`HostInfo`、`runnerExecAdapter`、`filterSteps` |
| **R-001** | **直接注册** `ossteps.StepB001CheckConnectivity()`（ID 仍为 B-001 或 collect registry 包装为 R-001 别名指向同一 Step 函数），**禁止**复制 PreCheck 逻辑 |
| **OS 分支** | 仅调用 `internal/common/os` 已有 helper（`DetectOSInfo`、`IsRHEL*`、`GetPkgManager`、`ExecuteAsUser*` 等） |
| **DB/env** | R-004 复用 `standby.GetPrimaryEnvFile`、`ClusterNameFromEnvFileContent`、`common/os.GetUserHomeDir` / `DetermineEnvFile`；yasql 复用 `common/sql` |
| **Profile/过滤** | 复用 `parseStepRanges` / `filterSteps`；profile 展开逻辑放 `collect_profiles.go`，**不**复制 range 解析 |
| **步骤目录** | `PrintCollectStepCatalog` 复用 `step_catalog.go` 的 `printStepSection` |
| **新增代码** | 仅允许：`internal/cli/collect.go`、`collect_profiles.go`、`internal/steps/collect/*.go`（Action 写归档文件）；共享逻辑若 ≥2 处使用则上提到 `common/` 或复用现有 B/C 包 |

#### 3.12.2 注释语言（源码）

| 项 | 规则 |
|----|------|
| Go 文件头、函数/类型注释、非显而易见业务说明 | **中文**（与现有 `internal/steps/os`、`internal/cli` 风格一致） |
| 导出符号 `godoc` | 中文为主；若需 IDE 英文提示可一行英文摘要 + 中文详述 |
| **禁止** | 在终端/错误/归档输出中使用中文注释作为用户可见文案 |

#### 3.12.3 输出语言（用户可见 = 仅英文）

以下**全部**使用 **ASCII 可打印英文**（`[A-Za-z0-9 .,:;!?_\-/=+*@#]` 等常见符号）；**禁止中文、全角标点、emoji 及其它非 ASCII 字符**：

| 输出面 | 范围 | 示例 |
|--------|------|------|
| **终端 stdout/stderr** | 步骤进度、Phase 标题、`ConsoleNotice`、`LogErrorExit` 块、CLI `fmt.Errorf` 返回 | `R-004: env file not found` |
| **session / debug 日志** | 与终端镜像的 session 行；debug 中 `msg=` 等人类可读字段 | 同终端 |
| **归档文本** | `summary.md`、`manifest.json` 中 `message`/`warnings[]`/`errors[]`/`description` | `status: success`, `warning: nmcli not found, skipped` |
| **步骤元数据** | R- 步 `Name`、`Description`（`-l` 目录与日志中 `'...'` 引号内） | `Check Connectivity`, `Collect DB config files` |
| **CLI flag help** | collect 子命令新增 flag 的 Short/Long/Help | 英文（与现有 `db`/`os` 一致） |

**例外（允许非英文，但须可控）：**

| 例外 | 处理 |
|------|------|
| 远端命令原始 stdout 落盘（`hosts/*/os/*.txt` 等） | 原样保存；**不**改写为英文 |
| `/etc/os-release`、`hostname` 等系统返回值 | 原样写入 JSON 字段（如 `os.name`） |
| 密码/密钥 | 脱敏为 `***REDACTED***`（ASCII） |

`summary.md` 由中文改为 **English-only** 一页摘要（字段标签英文，如 `Environment: YAC (3 nodes)`）。

#### 3.12.4 终端日志模式（与 yinstall db 一致）

collect **必须**使用与 `yinstall db` **相同**的日志基础设施与步骤进度格式，**禁止**自建 `fmt.Printf` 进度条或 JSON 行流式输出。

| 能力 | 复用点 | collect 行为 |
|------|--------|--------------|
| 日志初始化 | `logging.NewLogger(runID, logDir, ...)` | runID 默认 `collect-<timestamp>`；session/debug 命名 `yinstall_<type>_<timestamp>.log` / `yinstall_<type>_debug_<timestamp>.log` |
| 步骤进度 | `Logger.ConsoleStep(id, name, index, total, phase, duration)` | phase：`start` / `success` / `fail` / `skip`；**与 db 相同行格式** |
| 步骤内说明 | `Logger.ConsoleNotice(stepID, message)` | 英文 message |
| 命令失败 | `StepContext.ExecuteWithCheck` → `LogErrorExit` | 含 Host / Step / Command / Exit Code / Stdout / Stderr / Error 块 |
| 命令明细 | `LogCommandStart` / `LogCommandResult` | 仅 debug 日志 |
| 脱敏 | `logging.redact` | 与安装相同规则 |
| 步骤生命周期 | `runner.RunStep` | PreCheck → Action → PostCheck；Optional skip；DryRun 跳过 Action |
| Phase 分隔 | `logger.Info("======== Phase N: ... ========")` | Phase 1: Connectivity；Phase 2: Executing steps（英文标题） |
| **不支持** | `--precheck` | collect 忽略该 flag（§3.1）；不输出 precheck 专用 JSON 行 |

**ConsoleStep 文案**：当前 `ConsoleStep` 固定为 `Executing installation step`。collect 实现时二选一（**优先 a**）：

- **a)** 为 `ConsoleStep` 增加可选参数 `stepKind string`（默认 `"installation"`；collect 传 `"collection"`）→ 输出 `Executing collection step N of M`；
- **b)** 保持 `"installation step"` 字面不变（仅日志模式一致，语义略宽）。

**禁止**：collect 专用 Logger 类型、禁止跳过 `RunStep` 直接 `Execute` 且不记 debug 日志（Optional 探测可用已有 `*Quiet` 变体，见 `common/os/exec.go`）。

---

## 4. 归档包结构（建议）

```
<output-dir>/
  manifest.json              # 索引：版本、时间、节点列表、文件清单、校验和
  summary.md                 # English-only human summary (non-sensitive; see section 3.12.3)
  install-run.json           # 若为挂钩安装：命令行(脱敏)、run-id、步骤结果摘要
  hosts/
    <host-ip-or-name>/
      meta.json              # 该节点角色、profile、**os.family/arch**（§3.11）
      os/                    # OS 类原始与解析结果
      db/                    # 数据库类（若适用）
        logs/                # R-034：按时间窗过滤后的库日志（见 §5.16.B）
      standby/               # 备库类（二期，若适用）
      raw/                   # 可选：原始命令 stdout 文件
  yac/                       # YAC 集群级汇总（R-030；非 YAC 时不创建）
    yac-summary.json
    network.json             # VIP/SCAN/互联/公网网段
    diskgroups.json          # systemdg/datadg/archdg + multipath
    yfs-config.json
    hosts-consistency.json   # 各节点 /etc/hosts VIP/SCAN 一致性
    nodes-aggregate.json     # 节点 ↔ 实例 ↔ 路径
  cluster.json               # 顶层索引：集群名、节点列表、健康状态（R-030 产出）
  checksums.sha256           # 可选：文件完整性
```

---

## 5. 采集清单（详细）

以下按**类别**展开各步 **Action** 应写入的文件与命令；**步骤 ID 以 §3.4 为准**。实现时每个 §5 小节对应一个或多个 `R-0xx` 步骤的 Action 实现。

### 5.0 连通性预检（P0）→ **R-001**

| 采集项 | 说明 |
|--------|------|
| SSH 可达 | 同 **B-001**：对每个 `-t` 执行连通与基础工具链检查 |
| **OS 探测** | **`DetectOSInfo`**：发行版、版本、内核、**架构**、`PkgManager`、RHEL7/8/麒麟/UOS 标志 → `meta.json`（§3.11） |
| 认证 | 复用 root 全局 SSH 参数；非 root 时探测 `sudo -n`（同 B-001 警告） |
| 产物 | 成功后在 `manifest.json` → `steps[]` 记录 `R-001` success |
| 步骤管理 | 非 Optional；失败则整次 collect 终止 |

### 5.1 采集元数据（P0）→ **R-002**, **R-029**

| 字段 | 说明 | 来源 |
|------|------|------|
| `collector_version` | yinstall 版本、构建时间、git commit | 控制端 `version.go` |
| `collected_at` | UTC/本地时间戳 | 控制端 |
| `run_id` | 与安装 run 一致或独立生成 | 全局 flag |
| `profile` | full/os/db/... | CLI |
| `targets` | 目标 IP/主机名列表 | CLI |
| `collection_duration_sec` | 各节点耗时 | 采集器 |
| `errors[]` | 单项采集失败不阻断整体，记入 errors | 采集器 |

### 5.2 安装入参快照（P0，挂钩安装时）→ **R-003**

从 `buildOSParams` / `buildDBParams` 等写入 `install-params.json`（**脱敏后**）：

| 分类 | 参数示例（与现有 CLI 对齐） |
|------|-----------------------------|
| 全局 | targets、ssh-port、local、skip-os、include/exclude steps |
| OS 用户/组 | `os-user`, `os-user-uid`, `os-group`, `os-group-gid`, `os-dba-group`, `os-dba-group-gid`, `os-user-shell` |
| OS 基线 | timezone, ntp, sysctl-file, limits, hugepages, yum-mode, iso, deps packages, firewall-mode/ports, kernel-args |
| YAC OS | multipath, udev, disk-pattern, exclude-disks, systemdg/datadg/archdg |
| DB | cluster-name, db-port, memory-percent, charset, paths(home/data/log/stage), package, redo, archivelog, spfile-params, unified-audit, autostart 相关 |
| YAC DB | inter-cidr, public-network, access-mode, vips, scanname, scan-ips, yfs 参数 |
| Collect 日志 | `db-log-since`, `db-log-until`, `db-log-timezone`, `db-log-max-mb`（若指定） |
| 敏感项 | `os-user-password`, `db-sys-password` 等 → **仅记录 `"redacted": true` 或 SHA256 指纹** |

### 5.3 安装执行摘要（P0，挂钩安装时）→ **R-003**

| 字段 | 说明 |
|------|------|
| `steps[]` | step_id, name, host, status(success/skip/fail), duration_ms |
| `log_paths` | session.log、debug.log 相对路径或拷贝 |
| `precheck_issues[]` | 若有 `ReportPrecheckIssue` 记录 |

### 5.4 主机与 OS 身份（P0）→ **R-010**

| 采集项 | 采集方式 |
|--------|----------|
| 主机名 | `hostname`, `/etc/hostname` |
| OS 发行版/版本 | `/etc/os-release`, `uname -a` |
| 内核 | `uname -r`, `/proc/cmdline` |
| CPU/内存 | `lscpu`, `free -h`, `/proc/meminfo`（摘要） |
| 虚拟化/云 | `systemd-detect-virt`（若有） |
| 运行时间 | `uptime` |
| SELinux | `getenforce`（若存在） |
| **DMI/SMBIOS（dmidecode）** | 见 **§5.4.1**（**P0**） |

#### 5.4.1 主机硬件标识 — dmidecode（P0）→ **R-011**

采集目的：交付与审计时固定**物理机/虚拟机资产标识**（厂商、型号、序列号、UUID、BIOS 版本等），便于与合同机架、维保、扩容记录对照。

| 要求项 | 说明 |
|--------|------|
| 工具 | `dmidecode`（来自 `dmidecode` RPM/包）；未安装时记入 `errors[]`，`summary.md` 标注「DMI 未采集」 |
| 权限 | 读 DMI 表通常需 **root**；采集以 SSH `root` 或 `sudo` 执行（与 yinstall 安装一致） |
| 落盘路径 | `hosts/<host>/os/dmidecode/` |

**建议采集命令与文件：**

| 输出文件（建议名） | 命令 | 用途 |
|-------------------|------|------|
| `dmidecode-full.txt` | `dmidecode` | 完整原始输出（归档备查） |
| `system.txt` | `dmidecode -t system` | 厂商、产品名、序列号、UUID |
| `bios.txt` | `dmidecode -t bios` | BIOS 版本、发布日期 |
| `baseboard.txt` | `dmidecode -t baseboard` | 主板厂商/型号/序列号 |
| `chassis.txt` | `dmidecode -t chassis` | 机箱类型、资产标签（若有） |
| `processor.txt` | `dmidecode -t processor` | CPU 型号、频率、插槽（可与 `lscpu` 交叉） |
| `memory.txt` | `dmidecode -t memory` | 内存条数量、容量、速率（审计内存规格） |

**结构化摘要（P0，写入 `hosts/<host>/os/hardware-dmi.json`）：**

从上述输出解析（或调用 `dmidecode -s` 键值）至少包含：

| JSON 字段（示例） | dmidecode -s 键（示例） |
|-------------------|-------------------------|
| `manufacturer` | `system-manufacturer` |
| `product_name` | `system-product-name` |
| `serial_number` | `system-serial-number` |
| `uuid` | `system-uuid` |
| `bios_vendor` | `bios-vendor` |
| `bios_version` | `bios-version` |
| `bios_date` | `bios-release-date` |
| `baseboard_manufacturer` | `baseboard-manufacturer` |
| `baseboard_product` | `baseboard-product-name` |
| `baseboard_serial` | `baseboard-serial-number` |
| `chassis_type` | `chassis-type`（可选） |
| `asset_tag` | `chassis-asset-tag`（可选，常为空） |

实现时可执行：

```bash
dmidecode -s system-manufacturer
dmidecode -s system-product-name
# ... 其余键同上表
```

**行为约定：**

- 虚拟机/云主机可能返回通用字符串（如 `QEMU`、`Amazon EC2`）；仍采集，不做过滤。
- 某项 `-s` 返回 `Not Specified` / 空 → JSON 中记 `null` 或空字符串。
- **不属于敏感凭据**，可明文写入归档；若客户策略禁止出厂序列号出网，二期可增加 `--omit-dmi-serial`（本期不做）。
- `summary.md` 中展示一行硬件摘要：`厂商 / 型号 / SN / UUID`（SN 可按策略省略）。

### 5.5 用户、组与权限（P0）→ **R-012**

| 采集项 | 说明 |
|--------|------|
| 产品用户 | `id <os-user>`, `/etc/passwd` 中该行 |
| 主组/附属组 | `id`, `groups` |
| DBA 组 | `getent group YASDBA`（或参数化组名） |
| sudoers | 是否配置免密（**不采 sudoers 全文若含其他用户敏感规则**，可仅 `grep ^<user>` ） |
| limits | `/etc/security/limits.conf` 中与产品用户相关行 |
| umask 配置 | 与 B-006 相关文件片段 |

### 5.6 内核与资源参数（P0）→ **R-013**（**R-031** / **R-032** 可选增强）

| 采集项 | 说明 |
|--------|------|
| **内核全量参数** | 见 **§5.6.1**（**P0**：运行时 sysctl、`/proc/sys`、启动参数、持久化配置等） |
| limits / hugepages | 用户 limits 仍在 §5.5；大页**运行时值**包含在 §5.6.1 的 `proc-sys` / `sysctl -a` 中；B-011 写入的**配置文件**片段可另附 |

#### 5.6.1 内核参数全量采集（P0）

采集目的：存档时保留**当前内核可见的全部可调参数**（运行时 + 持久化 + 启动项），便于与 yinstall 写入的 `yashandb.conf` 等对照、复现环境或审计「是否被后续改动」。

| 要求项 | 说明 |
|--------|------|
| 原则 | **不做 key 白名单过滤**；以全量导出为主，yinstall 相关项（shmmax、vm.*、net.*）仅用于 `summary.md` 高亮，不替代全量 |
| 落盘路径 | `hosts/<host>/os/kernel/` |
| 权限 | 读 `/proc`、`/sys`、sysctl 配置需 root/`sudo` |
| 体积 | `sysctl -a`、`proc-sys` 可能数 MB；允许 gzip 压缩落盘（`*.txt.gz`），`manifest.json` 记录是否压缩 |

**运行时 — sysctl 与 /proc/sys（P0）**

| 文件（建议名） | 命令 / 来源 | 说明 |
|----------------|-------------|------|
| `sysctl-a.txt` | `sysctl -a` 或 `sysctl --all` | **全部** sysctl 键值（IPv6 等含多行项保留原样） |
| `sysctl-a-binary.txt` | `sysctl -a --binary`（若系统支持） | 含二进制/不可打印项的完整转储（可选，失败则 skip） |
| `proc-sys.tar` 或 `proc-sys/` | `tar -cC /proc sys` 或脚本遍历 `/proc/sys` 逐文件 `cat` | **`/proc/sys` 树全量**（与 `sysctl -a` 互备；部分内核项仅出现在 procfs） |
| `proc-cmdline.txt` | `cat /proc/cmdline` | 当前生效的内核启动参数 |
| `proc-version.txt` | `cat /proc/version` | 内核构建版本字符串 |
| `proc-modules.txt` | `cat /proc/modules` 或 `lsmod` | 已加载模块列表 |
| `uname.txt` | `uname -a` | 机器与内核发行标识 |

**启动与引导配置（P0）**

| 文件（建议名） | 来源 | 说明 |
|----------------|------|------|
| `grub-cmdline.txt` | `grubby --info=ALL` 或 `grub2-editenv list`（视 OS） | 各引导项 cmdline |
| `etc-default-grub` | `/etc/default/grub` 拷贝 | GRUB2 模板（含 `GRUB_CMDLINE_LINUX`） |
| `grub-cfg-fragment/` | `/etc/grub.d/` 列表 + 关键脚本摘要（不必全量可执行脚本） | 引导片段 |
| `boot-loader-entries/` | `/boot/loader/entries/*.conf`（UEFI/systemd-boot，若存在） | 新式引导项 |

与 B-012 一致：采集结果中应能还原 `transparent_hugepage`、`elevator`、`LANG` 等 yinstall 关心的启动参数，但**不限制**仅采这些键。

**持久化 sysctl 配置（P0）**

| 文件（建议名） | 来源 |
|----------------|------|
| `etc-sysctl.conf` | `/etc/sysctl.conf` |
| `etc-sysctl.d/` | `/etc/sysctl.d/` 下全部 `*.conf`（含 `yashandb.conf` 或 `--os-sysctl-file` 指定路径） |
| `usr-lib-sysctl.d/` | `/usr/lib/sysctl.d/`（若存在，只读打包） |
| `run-sysctl.d/` | `/run/sysctl.d/`（若存在，反映运行时覆盖） |

**透明大页 / 内存子系统（P0，含在全量中，summary 额外摘录）**

| 文件（建议名） | 来源 |
|----------------|------|
| `transparent-hugepage.txt` | `/sys/kernel/mm/transparent_hugepage/enabled`、`.../defrag`、`.../shmem_enabled` 等可读节点 |
| `vm-hugepages-summary.txt` | `/proc/meminfo` 中与 HugePages 相关行 + `/proc/sys/vm/*huge*` 键列表（可由脚本从 `proc-sys` 提取，单独文件便于查阅） |

**内核模块可调参数（P1，建议首期纳入）**

| 文件（建议名） | 来源 | 说明 |
|----------------|------|------|
| `sys-module-parameters/` | 遍历 `/sys/module/*/parameters/*` 可读文件 | 各模块 `parameters` 当前值；模块很多时体积较大，**P1**；若超时则按模块列表分批或仅采已加载模块（`lsmod` 与 `/proc/modules` 交集） |

**内核编译配置（P2，可选）**

| 文件 | 来源 |
|------|------|
| `config-uname-r` | `/boot/config-$(uname -r)` 或 `/proc/config.gz`（若存在） | 编译期 CONFIG_*，体积大，二期可选 |

**结构化摘要（P0，`kernel-summary.json`）**

不全文解析 `sysctl -a`，仅抽取**便于检索**的字段（全量仍以文本/tar 为准）：

| 字段 | 来源示例 |
|------|----------|
| `kernel_release` | `uname -r` |
| `cmdline` | `/proc/cmdline` |
| `shmmax`, `shmall`, `shmmni` | sysctl vm.* |
| `nr_hugepages`, `hugetlb_shm_group` | sysctl vm.* |
| `tcp_*` / `net.core.*` 等 | 可选列常用 net 项前 20 条，**非完整列表** |
| `yinstall_sysctl_files[]` | 本次安装写入的 sysctl 文件路径列表 |
| `collection_notes` | 如 `proc-sys` 是否 tar.gz、是否跳过 module parameters |

**行为约定：**

1. `sysctl` / 读 `/proc/sys` 单项失败（权限、已知不存在）→ 记日志，继续下一项；整体不失败。
2. `summary.md` 用表格列出：**内核版本**、**cmdline 一行**、**shmmax/shmall/nr_hugepages**、**THP 策略**；并注明「全量见 `os/kernel/sysctl-a.txt`」。
3. 与 §5.8.1 网络 sysctl 不重复采集逻辑：网络**接口**在 network 目录；**net.* / net.ipv4.*** 内核参数仅在 §5.6.1 全量中出现一次。

### 5.7 时间与时钟（P1）→ **R-014**

| 采集项 | 说明 |
|--------|------|
| 时区 | `timedatectl` 或 `/etc/localtime` |
| chrony/ntp | `/etc/chrony.conf` 摘要、`chronyc sources`（若可用） |

### 5.8 网络（P0）→ **R-015**, **R-016**

| 采集项 | 说明 |
|--------|------|
| **网卡与绑定** | 见 **§5.8.1**（**P0**，全量网卡 + bond/team/bridge） |
| **网卡绑核** | 见 **§5.8.2**（**P0**，IRQ / RPS / XPS 详细采集） |
| IP 地址 | `ip -4 addr`, `ip -6 addr`（可选） |
| 路由 | `ip route`、`ip -6 route`（若启用 IPv6） |
| DNS | `/etc/resolv.conf` |
| 监听端口 | `ss -tlnp` 或 `netstat`（与 DB/YAC 端口对照） |
| `/etc/hosts` | 全文（常含 VIP/SCAN，**无密码**） |
| YAC 专用 | 参数中的 inter-cidr、public-network、vip、scan-ips 与 hosts 中条目交叉校验 |

#### 5.8.1 网卡、聚合与绑定（P0）→ **R-015**（**R-033** topology 可选）

采集目的：交付与排障时还原**全部物理/虚拟网卡**、**IP 配置**、**bond/team/bridge/VLAN** 拓扑及持久化配置（与 `installer.md` §11 bond/team 场景一致）。

| 要求项 | 说明 |
|--------|------|
| 范围 | **所有**网卡（含 `DOWN`、`NO-CARRIER`、未配置 IP 的口），不限于有地址的接口 |
| 落盘路径 | `hosts/<host>/os/network/` |
| 权限 | 读配置与 `/sys/class/net` 仅需 root 或 `sudo`（与安装一致） |
| 敏感信息 | 一般无密码；**不采集** WPA/WiFi PSK、VPN 密钥文件内容（若存在仅记路径） |

**建议原始输出文件：**

| 文件（建议名） | 命令 / 来源 | 用途 |
|----------------|-------------|------|
| `ip-link.txt` | `ip -d link show` | 全接口列表、MAC、master 关系、状态、驱动信息 |
| `ip-addr-v4.txt` | `ip -4 addr show` | IPv4 地址、前缀、scope |
| `ip-addr-v6.txt` | `ip -6 addr show` | IPv6（可选） |
| `ip-route-v4.txt` | `ip -4 route show table all` | 路由（含多表） |
| `ip-rule.txt` | `ip rule list` | 策略路由（若有） |
| `ethtool-all.txt` | 对每个接口 `ethtool -i <dev>`；对已 UP 口 `ethtool <dev>`（可合并脚本输出） | 驱动、固件、链路协商、环回 offload 等 |
| `proc-net-bonding/` | `/proc/net/bonding/*`（存在则逐文件拷贝） | bond 模式、slave、MII 状态 |
| `teamdctl.txt` | `teamdctl <team-dev> state view`（对每个 team 设备执行，存在则采） | team runner、slave 状态 |
| `nmcli-devices.txt` | `nmcli -f all device show` | NetworkManager 设备视图（RHEL8+ 常见） |
| `nmcli-connections.txt` | `nmcli -f all connection show` | 连接配置摘要 |
| `network-scripts/` | `/etc/sysconfig/network-scripts/ifcfg-*`（存在则打包或逐文件拷贝） | 传统 ifcfg（bond/team slave 的 `MASTER=`/`TEAM_MASTER=`） |
| `NetworkManager-system-connections/` | `/etc/NetworkManager/system-connections/*`（**仅拷贝连接名列表 + 非密钥字段**；含 `802-1x`/`psk` 的文件只记文件名并 redact） | NM 持久化连接 |
| `netplan/` | `/etc/netplan/*.yaml`（Ubuntu 等，若存在） | 云/新系统 |
| `udev-net.rules` | `/etc/udev/rules.d/*net*`、`/etc/udev/rules.d/*persistent-net*`（若存在） | 网卡命名持久化 |

**结构化摘要（P0，写入 `hosts/<host>/os/network/interfaces.json`）：**

`interfaces` 数组，每个元素一条逻辑或物理接口，建议字段：

| 字段 | 说明 |
|------|------|
| `name` | 接口名（`eth0`、`bond0`、`team0` 等） |
| `index` | `ifindex` |
| `mac` | 链路层地址 |
| `state` | `UP`/`DOWN`/`UNKNOWN` |
| `mtu` | MTU |
| `master` | 所属 bond/team/bridge 名（无则 `null`） |
| `kind` | `physical` / `bond` / `team` / `bridge` / `vlan` / `other`（由 `ip -d link` 的 `link/...` 推断） |
| `ipv4` | `[{ "addr", "prefix" }]` |
| `ipv6` | 同上（可选） |
| `bond` | 若 kind=bond：`{ "mode", "miimon", "slaves": ["eth0","eth1"] }`（来自 `/proc/net/bonding/` 或 ifcfg） |
| `team` | 若 kind=team：`{ "runner", "slaves": [...] }`（来自 teamdctl 或 nmcli） |
| `bridge` | 若 kind=bridge：`{ "ports": [...] }` |
| `vlan_id` | 若为 VLAN：ID |
| `driver` | 来自 `ethtool -i` |
| `speed_duplex` | 来自 `ethtool`（链路 UP 时） |
| `config_source` | `ifcfg` / `nmcli` / `netplan` / `unknown` |
| `cpu_affinity_summary` | 可选：IRQ/RPS/XPS 绑核摘要（详见 `nic-cpu-affinity.json`） |

**绑定关系图（P1，可选 `network/topology.json`）：**

- 节点：物理口 → bond/team → bridge → VLAN
- 边：`slave_of` / `member_of`
- 便于 YAC 对照「公网 bond0、私网 bond1」类文档

**采集逻辑约定：**

1. 先 `ip -o link show` 枚举全部 `ifname`，再对每个名字补采 `ethtool -i`；避免只采「有 IP 的口」。
2. **bond**：读 `/proc/net/bonding/*` + 解析 `ifcfg-bond*` / `ifcfg-*` 中 `MASTER=bondN`、`BONDING_OPTS`。
3. **team**：优先 `teamdctl`；若无则 `nmcli connection show --active` 中 `team` 类型及 `team-port` slave。
4. **bridge / VLAN**：`ip -d link` 中 `master` 与 `vlan protocol`；`bridge link`（`bridge -c link show br0` 若存在）。
5. 命令或文件不存在 → 该项 `skipped`，记入 `errors[]`，不阻断整包。
6. `summary.md` 列出：接口总数、bond/team 数量、各 bond/team 名称与模式、带 IP 的接口列表（一行表）；**若存在网卡绑核**（§5.8.2），摘要各口 IRQ/RPS/XPS 绑核范围。

#### 5.8.2 网卡绑核 — IRQ / RPS / XPS（P0）→ **R-015**

采集目的：与 §5.8.1 网卡清单配套，**详细存档网卡与 CPU 的亲和/绑核配置**（现场常称「网卡绑核」），便于与 `isolcpus`、DB 进程绑核（§5.14.1）对照审计。

| 要求项 | 说明 |
|--------|------|
| 范围 | §5.8.1 枚举的**全部**接口（含 bond/team slave、DOWN 口）；每个接口及其关联 IRQ |
| 落盘路径 | `hosts/<host>/os/network/` |
| 原则 | **全量采集**当前运行时绑核；持久化脚本/服务配置一并存档（若存在） |
| 与内核参数 | `/proc/cmdline` 中 `isolcpus`、`nohz_full`、`rcu_nocbs` 等已在 §5.6.1；本节专注**网卡 IRQ 与队列** |

**建议原始输出文件：**

| 文件（建议名） | 命令 / 来源 | 说明 |
|----------------|-------------|------|
| `proc-interrupts.txt` | `cat /proc/interrupts` | 全量中断统计，用于 IRQ ↔ 接口名映射 |
| `irq-affinity/` | 遍历 `/proc/irq/*/` 下 `smp_affinity`、`smp_affinity_list`（存在则拷贝） | 各 IRQ 绑核掩码/ CPU 列表 |
| `irq-default-smp-affinity.txt` | `/proc/irq/default_smp_affinity` | 默认 IRQ 亲和 |
| `sys-net-queues/` | 对每个 `<dev>`：`/sys/class/net/<dev>/queues/rx-*/rps_cpus`、`.../tx-*/xps_cpus`、`rps_flow_cnt`（若存在） | **RPS/XPS** 队列级绑核 |
| `sys-net-rps-sock-flow.txt` | `/proc/sys/net/core/rps_sock_flow_entries` | RFS 全局项 |
| `ethtool-channels.txt` | 对每个接口 `ethtool -l <dev>`（支持则采） | 队列/channel 数，与 RPS 队列对照 |
| `ethtool-coalesce.txt` | 对每个 UP 口 `ethtool -c <dev>`（可选） | 中断合并，辅助排障 |
| `irqbalance-status.txt` | `systemctl status irqbalance`、`ps aux \| grep irqbalance` | irqbalance 是否运行 |
| `etc-sysconfig-irqbalance` | `/etc/sysconfig/irqbalance` 或 `/etc/default/irqbalance`（若存在） | irqbalance 持久化配置 |
| `tuned-active-profile.txt` | `tuned-adm active`（若安装 tuned） | 网络相关 profile 可能影响 IRQ 策略 |

**IRQ ↔ 网卡映射（Action 内脚本逻辑）：**

1. 从 `/proc/interrupts` 解析含接口名（如 `eth0-TxRx-0`、`mlx5_comp0@pci`）的行，提取 IRQ 号。
2. 读取对应 `/proc/irq/<N>/smp_affinity_list`（优先）或 `smp_affinity`。
3. bond/team：分别采集 **bond 主设备**与各 **slave** 的 IRQ；slave 中断在 `/proc/interrupts` 中通常仍带物理口名。

**结构化摘要（P0，`nic-cpu-affinity.json`）：**

```json
{
  "isolcpus_from_cmdline": "0-1",
  "irqbalance_running": true,
  "interfaces": [
    {
      "name": "eth0",
      "irqs": [{ "irq": 128, "smp_affinity_list": "4-7", "hint": "eth0-TxRx-0" }],
      "rps": [{ "queue": "rx-0", "rps_cpus": "ffff" }],
      "xps": [{ "queue": "tx-0", "xps_cpus": "000f" }],
      "channels": { "combined": 8, "rx": 0, "tx": 0 }
    }
  ]
}
```

| 字段 | 说明 |
|------|------|
| `binding_configured` | 任一接口 IRQ/RPS/XPS 非默认全 CPU 时为 `true` |
| `interfaces[].irqs` | 该网卡相关 IRQ 及 `smp_affinity_list` |
| `interfaces[].rps` / `xps` | 各队列 `rps_cpus` / `xps_cpus` 十六进制掩码 |
| `collection_notes` | 如某口无 RPS 节点、ethtool 不支持等 |

**行为约定：**

1. 读 `/sys` / `/proc/irq` 单项失败 → 记 `errors[]`，继续下一接口。
2. **无绑核**（全部为系统默认、全 CPU 掩码）时仍落盘，并设 `binding_configured: false`。
3. `interfaces.json`（§5.8.1）可选冗余字段 `cpu_affinity_summary` 指向 `nic-cpu-affinity.json` 中该口摘要。

### 5.9 防火墙（P1）→ **R-017**

| 采集项 | 说明 |
|--------|------|
| firewalld/iptables 状态 | `firewall-cmd --list-all` 或 `iptables -L -n` |
| 与安装策略对照 | disable / open-ports 与开放端口列表 |

### 5.10 软件包与 YUM（P1）→ **R-018**

| 采集项 | 说明 |
|--------|------|
| 已装 RPM/DEB | 与 `os-deps-db-packages` 相关包是否安装（`rpm -q`） |
| YUM repo | `/etc/yum.repos.d/` 中 local.repo 等（B-014） |
| 可选 ISO 挂载 | 挂载点与 fstab 片段 |

### 5.11 磁盘、LVM 与挂载（P0）→ **R-019**

| 采集项 | 说明 |
|--------|------|
| 块设备 | `lsblk -f` |
| 分区/挂载 | `df -hT`, `/etc/fstab` |
| LVM | `vgs`, `lvs`, `pvs`（若 B-020 使用） |
| 目录占用 | 安装路径、数据路径、日志路径 `du -sh` |
| YAC 共享盘 | multipath 状态、`multipath -ll`（若启用）、udev rules；**YAC 时**各节点映射一致性见 §5.17.C |
| diskgroup 配置 | systemdg/datadg/archdg 设备列表；`/dev/yfs/sys*`、`data*`、`arch*` 符号链接（B-029） |

### 5.12 数据库 — 路径、版本与布局（P0）→ **R-020**

> **前置**：**R-004** 已完成 env 自动发现（§3.9）；本节 Action 读取其写入的 `env_file` 与路径 Params，不再要求 CLI 传 `--cluster-name` / `--db-begin-port`。

| 采集项 | 说明 |
|--------|------|
| 路径 | `db-home-path`, `db-data-path`, `db-log-path`, `db-stage-dir` 是否存在、属主、`du -sh`（优先 R-004 解析值，其次 tom l） |
| 安装包版本 | 解压目录下 `version`、`database` 包 manifest（与 C-007 一致） |
| 环境变量文件 | R-004 确定的包装文件路径；`source` 后的 `YASDB_HOME`、`YASCS_HOME` 等（**不含密码**） |
| yasboot 目录 | `~/.yasboot/` 下 `*.env` 文件名列表，**不采 env 内密码** |
| **核心资产详情** | 数据文件 / REDO / 参数等见 **§5.13.1**（**P0**） |

#### 5.12.1 产品环境发现（P0）→ **R-004**

| 采集项 | 说明 |
|--------|------|
| env 包装文件 | 最终采用的 `~/.bashrc` / `~/.port<port>` 等路径 |
| 集群名 | 反解析的 `db_cluster_name`（如 `yashandb_3988`） |
| 起始端口 | `db_begin_port`（来自 `.port` 文件名、集群后缀或 tom l） |
| source 后变量 | `YASDB_HOME`、`YASCS_HOME`、`YASOM_HOME` 等键值 |
| 消歧记录 | 多实例时的候选列表与选中理由 |
| 产物 | `hosts/<host>/db/env-discovery.json` |

实现复用：`internal/steps/standby/primary_env.go`（`GetPrimaryEnvFile`、`SyncPrimaryClusterNameFromEnvFile`）、`internal/common/os/env.go`（`DetermineEnvFile`、`GetUserHomeDir`）、`internal/steps/db/c032_configure_unified_audit.go`（`resolveDBEnvFile` 结果写入约定）。

### 5.13 数据库 — 集群配置与运行状态（P0）→ **R-023**, **R-025**

| 采集项 | 说明 |
|--------|------|
| 集群名、节点数 | R-004 解析的 cluster + `yasboot cluster status -c <name> -d`（C-029 同款）；**YAC 汇总**见 §5.17 / R-030 |
| yasboot 其它只读命令 | 视版本支持：`yasboot cluster list`、节点列表等（失败记入 errors） |
| 进程 / 端口 | 见 §5.14 |
| 自启动 | systemd unit（C-028）、profile 脚本（C-027） |
| ArchDG | C-023：diskgroup 名、挂载点、与 datadg/archdg 参数对照 |
| **配置与库内参数** | 见 **§5.13.1** |

#### 5.13.1 数据库核心资产 — 数据文件、REDO、参数（P0）→ **R-021**, **R-022**, **R-026**, **R-027**

采集目的：交付存档必须能回答「库装在哪、数据/redo/归档落在哪些文件、规划参数与实际参数各是多少」。分三层：**安装配置文件**（不连库）、**文件系统实物**（不连库）、**库内字典视图**（需 yasql，可选但强烈建议）。

| 要求项 | 说明 |
|--------|------|
| 落盘路径 | `hosts/<host>/db/`（单机）或每节点各自目录 + 汇总 `cluster.json`（YAC） |
| **连库方式** | **不提供、不要求 `--db-sys-password`**。与 DB 安装后步骤（C-031/C-033 等）一致：以 **产品用户**（`--os-user`）**source R-004 确定的 env 包装文件**后，执行 **`yasql -S / as sysdba`**（复用 `commonsql.ExecuteSQLAsSysdbaCtx` / `ExecuteSQLAsSysdbaInstallLayoutCtx`），**本地 OS 认证，无需 SYS 密码** |
| 前置条件 | 库进程已启动；**R-004** 已成功解析 `env_file`；`YASDB_HOME` 等路径有效。不满足时 R-026 → **skipped**（Optional），A+B 仍完成 |
| profile | `full` / `db` 必须包含本节；`os` profile 跳过 |

---

**A. 安装期配置（文件，P0 — 不连库）**

| 文件（建议路径） | 内容 |
|------------------|------|
| `config/yashandb.toml` | stage 目录及已部署路径下的 **全文拷贝**（`$db-stage-dir/yashandb.toml` 等） |
| `config/yashandb-parsed.json` | 自 tom l 解析的结构化字段（示例键见下表） |
| `config/gen-config-manifest.txt` | `yasboot package se gen` / `ce gen` 产出目录文件列表 |

**`yashandb-parsed.json` 至少解析字段（与 C-014～C-019 对齐）：**

| 字段组 | 示例键 |
|--------|--------|
| 集群/实例 | `cluster_name`, `nodes`, `begin_port`, `CHARACTER_SET`, `ISARCHIVELOG`, `USE_NATIVE_TYPE` |
| 路径 | `data_path`, `home_path`, `log_path`（或 tom l 中等价项） |
| **REDO（规划）** | `REDO_FILE_NUM`, `REDO_FILE_SIZE` |
| 内存 | `MEMORY_PERCENT` / 与 `--db-memory-percent` 相关项 |
| YAC/YFS | `REDO_FILE_SIZE`/`REDO_FILE_NUM`（C-019）、YFS AU、shm 等 tom l 片段 |
| 归档 | `ISARCHIVELOG`、归档目的地配置项（若有） |
| 安装入参对照 | `install_params.db_redo_file_num`, `db_redo_file_size`, `db_disable_archivelog`, `db_spfile_params`（脱敏） |

另拷贝：`--db-custom-sql-script` 路径与 SHA256（不重复全文除非无敏感）。

---

**B. 文件系统 — 数据文件 / REDO / 日志目录（P0 — 不连库）**

在 **`db-data-path`**（及 tom l 中出现的实际路径）上采集：

| 文件（建议路径） | 命令 / 说明 |
|------------------|-------------|
| `filesystem/data-dir-tree.txt` | `find <data_path> -xdev -printf '%p %s\n'` 或 `du -ah`（大目录可限制深度并 gzip） |
| `filesystem/datafile-list.txt` | 按扩展名/目录规则枚举数据文件：如 `find ... \( -name '*.dbf' -o -name '*.data' -o -path '*/dbfiles/*' \)`（以现场布局为准，脚本可配置 glob） |
| `filesystem/redo-list.txt` | 枚举 REDO 成员文件：常见路径 `*/dbfiles/redo*`、`*/log/redo*`；**每文件路径 + 字节大小** |
| `filesystem/controlfile-list.txt` | 控制文件路径与大小（若可从目录识别） |
| `filesystem/archive-area.txt` | 归档目录占用、`find` 归档日志文件数量与总大小（若 `ISARCHIVELOG=true`） |
| `filesystem/temp-undo-summary.txt` | TEMP/UNDO 相关子目录 `du -sh`（与 installer §5.2 表空间布局对照） |
| `filesystem/db-log-dir.txt` | `db-log-path` 下目录树与大小（**清单**；**正文**见 R-034 §5.16.B） |
| `filesystem/yasdb-home-snapshot.txt` | `db-home-path` 顶层结构（bin、admin 等，不含巨大 trace 内容） |

**结构化摘要（P0，`storage-layout.json`）：**

```json
{
  "data_path": "/data/yashan/yasdb_data",
  "total_bytes": 0,
  "datafiles": [{ "path": "...", "size_bytes": 0 }],
  "redo_members": [{ "path": "...", "size_bytes": 0, "group": null }],
  "controlfiles": ["..."],
  "archive_dest_bytes": 0
}
```

由脚本扫描填充；与 §5.13.1.C 库内视图 **交叉校验**（路径不一致时写入 `warnings[]`）。

---

**C. 库内字典 — 参数 / 数据文件 / REDO（P0，库已启动且 yasql 可用时）**

以产品用户 + env 文件执行 **yasql `/ as sysdba`**（与安装默认一致，**无 SYS 密码**）：

```text
# 与 internal/common/sql/yasql.go 一致
source <env_file>
yasql -S / as sysdba <<EOF
SELECT ...
EOF
```

单机亦可按安装布局设置 `YASDB_HOME`/`YASCS_HOME` 后执行（同 `buildInstallLayoutYasqlCommand`）。归档包中**不得**出现 SYS 密码或 `sys/password@cluster` 连接串。

| 输出文件 | SQL / 说明 |
|----------|------------|
| `sql/database-info.txt` | 库名、open_mode、role、版本（视可用视图：`v$database` 或等价） |
| `sql/parameters-all.txt` | `SELECT name, value, display_value FROM v$parameter ORDER BY name`（**全量参数**；若仅支持 `SHOW PARAMETER`，则脚本批量导出） |
| `sql/parameters-core.json` | 从全量中另存核心类：`redo_*`, `undo_*`, `arch_*`, `db_*`, `memory_*`, `checkpoint_*`, `max_sessions` 等（便于 summary） |
| `sql/datafiles.txt` | 数据文件：路径、表空间、大小、状态（`v$datafile` / `dba_data_files` 等等价） |
| `sql/redo-logs.txt` | REDO 组/成员：组号、线程、路径、大小、状态（`v$log`, `v$logfile` 等等价） |
| `sql/tablespaces.txt` | 表空间名、类型、状态、占用 |
| `sql/controlfiles.txt` | 控制文件路径列表 |
| `sql/archive-status.txt` | 归档模式、当前/arch dest、最近归档（若归档开启） |
| `sql/spfile-params-from-install.txt` | 仅当安装时使用了 `--db-spfile-params`：查询对应 name 的当前值，与安装意图对照 |

**与安装配置对照（P0，`config-drift.json`）：**

| 检查项 | 说明 |
|--------|------|
| REDO | tom l 中 `REDO_FILE_NUM`/`REDO_FILE_SIZE` vs `sql/redo-logs.txt` vs `filesystem/redo-list.txt` |
| 归档 | `ISARCHIVELOG` vs `sql/archive-status.txt` |
| 字符集 | tom l `CHARACTER_SET` vs 库内字符集相关参数 |
| 自定义 SPFILE | `install_params.db_spfile_params` 各项 vs `v$parameter` 当前值 |

不一致写入 `warnings[]`，在 `summary.md` 用表格列出。

---

**D. YAC 逐节点补充（P0 若 YAC）**

| 采集项 | 负责步骤 | 说明 |
|--------|----------|------|
| 每节点 tom l / 实例路径 | R-021 | 各节点 stage 或已部署路径下 `yashandb.toml` |
| 共享盘 / multipath / diskgroup | R-019, R-025 | §5.11 + `db/archdg.json`；YFS 符号链接 `/dev/yfs/*` |
| 互联/公网网卡 | R-015, R-016 | 与 tom l 或路由推断的 CIDR 对照 |
| 节点 `/etc/hosts` | R-016 | VIP/SCAN 条目（C-010 写入块） |

**集群级 YAC 汇总**见 **§5.17**（**R-030**）。

### 5.14 数据库 — 进程与监听（P0）→ **R-024**

| 采集项 | 说明 |
|--------|------|
| 进程 | `ps` 过滤 yasdb/yasom/yasagent |
| 端口 | R-004 解析的 `db_begin_port` 推算 listener（yasdb、yasom、yasagent） |
| 实例/库状态 | 已由 §5.13 `cluster status` 覆盖；库内 role/open_mode 见 §5.13.1.C |
| **进程绑核** | 见 **§5.14.1**（**P0**：若现场已配置 DB 绑核则必须采集） |

#### 5.14.1 数据库进程绑核 — CPU / NUMA 亲和（P0）→ **R-024**

采集目的：与 §5.8.2 网卡绑核同级，存档 **YashanDB 相关进程**（yasdb、yasagent、yasom 及 yasboot 派生子进程）的 **CPU 亲和、cpuset、systemd 绑核、NUMA 内存策略**；仅当未绑核时标记 `binding_configured: false`，**仍采集**默认亲和以便前后对比。

| 要求项 | 说明 |
|--------|------|
| 范围 | 本节点全部 yasdb / yasagent / yasom / yasboot 相关进程（由 R-024 `ps` 结果得 PID 列表） |
| 落盘路径 | `hosts/<host>/db/` |
| 前置 | R-004 已解析产品用户；读 `/proc/<pid>/` 需 root 或等价权限 |
| 与库内参数 | R-026 全量参数中 CPU/线程类项（如 `cpu_count`、`cpu_limit` 等）写入 `sql/parameters-core.json`；本节侧重 **OS 运行时绑核** |

**建议原始输出文件：**

| 文件（建议名） | 命令 / 来源 | 说明 |
|----------------|-------------|------|
| `process-cpu-affinity.txt` | 对每个 PID：`taskset -pc <pid>`；失败则读 `/proc/<pid>/status` 中 `Cpus_allowed_list` | 进程级 CPU 亲和 |
| `process-numa-maps/` | 对每个 yasdb 主进程：`/proc/<pid>/numa_maps` 摘要（文件大时可 head + 关键行） | NUMA 内存绑定 |
| `process-cgroup-cpuset.txt` | `cat /proc/<pid>/cgroup` + 对应 cgroup 下 `cpuset.cpus`、`cpuset.mems`（若存在） | cgroup v1/v2 cpuset |
| `systemd-cpu-affinity.txt` | `systemctl show <unit> -p CPUAffinity,AllowedCPUs,CPUAccounting`（unit 名来自 R-025 自启配置，如 `yashandb.service`） | systemd 单元级绑核 |
| `numactl-hardware.txt` | `numactl --hardware`（若命令存在） | NUMA 拓扑，对照绑核 |
| `db-cpu-params-from-config.txt` | tom l / spfile 中与 CPU、线程、绑核相关的配置片段（**R-021** 产物交叉引用） | 规划层绑核意图 |

**结构化摘要（P0，`process-cpu-affinity.json`）：**

```json
{
  "binding_configured": true,
  "numa_nodes": 2,
  "processes": [
    {
      "pid": 12345,
      "comm": "yasdb",
      "cpus_allowed_list": "4-15",
      "numa_policy": "bind:1",
      "systemd_unit": "yashandb.service",
      "systemd_cpu_affinity": "4-15"
    }
  ],
  "cross_check": {
    "isolcpus_from_cmdline": "0-3",
    "nic_binding_overlaps": []
  }
}
```

| 字段 | 说明 |
|------|------|
| `binding_configured` | 任一 DB 相关进程 `cpus_allowed_list` 窄于全 CPU，或 systemd/cgroup 显式绑核时为 `true` |
| `processes[]` | PID、comm、亲和列表、NUMA 策略摘要 |
| `cross_check.nic_binding_overlaps` | 与 §5.8.2 `nic-cpu-affinity.json` 对比：网卡 IRQ 与 DB 进程是否争用同一 CPU（写入 `warnings[]`） |

**判定「已绑核」的典型信号（满足任一即 `binding_configured: true`）：**

1. `Cpus_allowed_list` 非全核（如非 `0-<N-1>` 全范围）。
2. systemd unit 设置 `CPUAffinity=` / `AllowedCPUs=`。
3. cgroup `cpuset.cpus` 限制为子集。
4. tom l / spfile / `v$parameter` 中存在显式 CPU 绑核或 `cpu_limit` 类限制且与进程亲和一致。

**行为约定：**

1. 无 yasdb 进程 → R-024 进程段 skipped；绑核小节记 `binding_configured: false`，原因 `no_db_process`。
2. `taskset` 不可用 → 仅读 `/proc/<pid>/status`，记入 `collection_notes`。
3. `summary.md`：若 `binding_configured: true`，表格列出进程名、PID、CPU 列表、systemd 单元；并提示是否与 §5.8.2 网卡绑核冲突。

### 5.17 YAC 集群环境采集（P0 若 YAC）→ **R-030**

采集目的：在 **YAC（Yashan Active Cluster）** 场景下，除各节点 `hosts/<host>/` 逐机采集外，必须在归档包顶层提供**集群级、可对照 installer / yinstall db YAC 参数**的完整视图（VIP/SCAN、互联/公网、共享盘与 diskgroup、YFS、节点映射、跨节点一致性）。

| 要求项 | 说明 |
|--------|------|
| 触发 | §3.10 判定 `yac_mode: true`；否则 R-030 **skipped** |
| 执行位置 | 控制端 **local**（合并各节点已落盘产物 + R-023 输出） |
| 落盘路径 | `<output-dir>/yac/` + 更新 `<output-dir>/cluster.json` |
| 对照基准 | 与 `yinstall db` YAC 参数（`--yac-*`）、C-010/C-012/C-014/C-019/C-023 行为对齐 |
| 敏感项 | `*.env` 内密码、SYS 密码 → **不采集**；仅路径与键名 |

---

**A. 集群拓扑与运行状态（P0）**

| 文件（建议名） | 来源 | 说明 |
|----------------|------|------|
| `yac/cluster-status.txt` | R-023 复制或再执行 `yasboot cluster status -c <cluster> -d` | 与 C-029 相同全文 |
| `yac/cluster-list.txt` | `yasboot cluster list`（若支持） | 集群列表 |
| `yac/node-status/` | `yasboot node status` / 版本等价命令（若支持） | 各节点 agent/实例摘要 |
| `cluster.json` | R-030 汇总 | 见下方 schema |

**`cluster.json` 至少字段：**

```json
{
  "deployment_type": "yac",
  "cluster_name": "yashandb",
  "begin_port": 1688,
  "node_count": 3,
  "access_mode": "vip",
  "health_summary": "OK",
  "nodes": [
    {
      "host": "192.168.1.11",
      "hostname": "node1",
      "instance_id": "1-1",
      "role": "primary",
      "data_path": "/data/yashan/yasdb_data",
      "home_path": "/data/yashan/yasdb_home",
      "vip": "192.168.1.101",
      "status": "running"
    }
  ],
  "vips": ["192.168.1.101", "192.168.1.102", "192.168.1.103"],
  "scan": { "mode": "local", "name": "yashandb-scan", "ips": [] }
}
```

字段从 R-023 输出解析 + 各节点 `hosts/*/db/` 合并；解析失败时保留原始文本于 `cluster-status.txt`。

---

**B. 网络 — VIP / SCAN / 互联 / 公网（P0）**

与 C-010、C-001 网络校验、`--yac-inter-cidr` / `--yac-public-network` / `--yac-access-mode` 对照。

| 文件（建议名） | 采集内容 |
|----------------|----------|
| `yac/network.json` | 结构化：`access_mode`（vip/scan）、`inter_cidr`、`public_network`、`vips[]`、`scanname`、`scan_ips[]`、`scan_mode`（local/dns） |
| `yac/vip-scan-from-hosts.json` | 从各节点 R-016 `/etc/hosts` 提取 `*-vip`、SCAN 行；**跨节点必须一致** |
| `yac/network-interfaces-by-role.json` | 每节点：落在 inter_cidr / public_network 上的接口名、IP、bond/team（来自 R-015 `interfaces.json`） |

**反解析优先级**：

1. tom l / `yashandb-parsed.json`（R-021）
2. 各节点 `/etc/hosts` 中 yinstall 管理块（C-010 `ReadManagedHostsEntries` 规则）
3. 挂钩 `install-params.json` 中 `yac_*` 键（脱敏对照）
4. `ip route` + 节点 IP 推断 CIDR（最后手段，记入 `inferred: true`）

**一致性检查（P0，`yac/hosts-consistency.json`）**：

| 检查项 | 规则 |
|--------|------|
| `/etc/hosts` VIP/SCAN 块 | 所有 YAC 节点条目集合相同（允许空白行差异） |
| VIP 数量 | 与节点数一致（vip 模式） |
| SCAN IP | scan 模式下与 tom l / hosts 一致 |

不一致 → `warnings[]` + `summary.md` 高亮。

---

**C. 共享存储 — diskgroup / multipath / YFS（P0）**

与 C-012、B-021/B-022/B-029、C-023、C-019 对齐。

| 文件（建议名） | 采集内容 |
|----------------|----------|
| `yac/diskgroups.json` | `systemdg`、`datadg`、`archdg` 设备列表（WWID/路径）；来自 tom l 或 R-021 解析 |
| `yac/multipath-consistency.json` | 各节点 R-019 `multipath -ll` 摘要：**同一 WWID → 映射名在各节点一致**（同 C-012） |
| `yac/yfs-devices.txt` | 各节点 `/dev/yfs/sys*`、`data*`、`arch*` 符号链接与指向（B-029） |
| `yac/shared-disk-visibility.json` | 每节点 diskgroup 块设备是否可见、`lsblk` 路径 |
| `yac/yfs-config.json` | C-019 相关：`yfs_au_size`、`redo_file_size/num`、`shm_pool_size`、`max_instances` 等（tom l + 安装入参） |
| `yac/gen-config-manifest.txt` | stage 下 `yasboot package ce gen` / `se gen` 产出目录文件列表（R-021 交叉引用） |

---

**D. 逐节点聚合（P0）**

| 文件（建议名） | 说明 |
|----------------|------|
| `yac/nodes-aggregate.json` | 合并各 `hosts/<host>/meta.json`、`db/paths.json`、`db/env-discovery.json`、R-023 解析的 instance 行 |
| `yac/per-node-toml-diff.json` | 各节点 `yashandb.toml` 差异摘要（仅列节点相关字段，如 `nodes[i]`、本地路径） |

每节点 `hosts/<host>/meta.json` 增加：

| 字段 | 说明 |
|------|------|
| `deployment_type` | `yac-node` / `standalone` |
| `cluster_name` | R-004 解析 |
| `node_index` | 在集群中的序号（从 cluster status 或 tom l 推断） |

---

**E. yasboot 集群配置（P0）**

| 文件（建议名） | 来源 |
|----------------|------|
| `yac/yasboot-env-files.txt` | 各节点 `~/.yasboot/<cluster>.env` **文件名列表**（不读密码） |
| `yac/yasboot-home-links.txt` | `<cluster>_yasdb_home` 符号链接目标 |
| `yac/deploy-artifacts.txt` | 各节点 stage / deploy 目录关键文件清单 |

---

**F. 结构化摘要（P0，`yac/yac-summary.json`）**

供 `summary.md` 与 Agent 快速读取：

| 字段 | 说明 |
|------|------|
| `cluster_name`, `node_count`, `access_mode` | 基本标识 |
| `all_nodes_healthy` | 由 cluster status 推断 |
| `vip_scan_ok` | hosts 一致性检查 |
| `multipath_ok` | 跨节点 multipath 一致 |
| `diskgroups` | systemdg/datadg/archdg 设备数摘要 |
| `warnings_count` | 来自一致性/漂移检查 |

**行为约定**：

1. 单机环境：R-030 PreCheck → skipped，不创建 `yac/` 目录（可选保留最小 `cluster.json` 标注 `deployment_type: standalone`）。
2. YAC 但某节点 SSH 失败：已采集节点仍汇总，`nodes[]` 标记缺失节点，`errors[]` 记录。
3. `summary.md` YAC 段：表格列出节点、VIP、SCAN、diskgroup、集群状态首行；链到 `yac/` 与 `cluster.json`。
4. R-029 生成 manifest 时注册 `yac/` 下全部文件。

**实现参考**：`internal/steps/db/c010_write_hosts.go`、`c012_disk_check.go`、`c019_tune_yfs_params.go`、`precheck_network_validate.go`、`internal/cli/db.go` `buildDBParams`（`yac_*` 键）。

### 5.15 备库（standby profile，P2）→ **R-401～R-403**

| 采集项 | 说明 |
|--------|------|
| 主库连接信息 | 参数中的 primary host（脱敏密码） |
| 备库 expansion 配置 | gen-config 产出路径摘要 |
| 同步状态 | 与 E-014 类似查询结果摘要 |

### 5.16 日志与证据链 → **R-028**（A）、**R-034**（B）、**R-035**（可选）

#### 5.16.A 控制端 session/debug（P1）→ **R-028**

| 采集项 | 说明 |
|--------|------|
| 本次 yinstall 日志 | `--include-logs` 时拷贝控制端 session/debug 到归档根目录 |
| 与库日志关系 | **独立**于 R-034；不替代数据库侧 alert/trace |

#### 5.16.B 数据库日志 — 按时间窗采集（P0）→ **R-034**

采集目的：故障复盘、审计取证时归档 **YashanDB / yasboot / yasagent / yasom** 相关日志，且**仅保留指定时间段**内容，避免整文件拷贝导致归档过大。

| 要求项 | 说明 |
|--------|------|
| 触发 | CLI 传入 **`--db-log-since` 和/或 `--db-log-until` 至少一项**；均未传 → R-034 **skipped**（R-022 仍仅输出路径清单 `filesystem/db-log-dir.txt`） |
| Profile | `full` / `db`；`os` profile 不含 R-034 |
| PerHost | 每个 `-t` 节点独立采集 |
| 落盘路径 | `hosts/<host>/db/logs/` |
| 时区 | `--db-log-timezone`（默认 `Asia/Shanghai`）解析无时区后缀的时间字符串 |
| 体积 | `--db-log-max-mb`（默认 **500** MB/节点）；超出截断并记 `warnings[]` |
| 脱敏 | 落盘前走与 session log 相同的 `redact` 规则（password/token 等） |

**CLI 时间参数：**

| 参数 | 说明 | 示例 |
|------|------|------|
| `--db-log-since` | 窗口起点（**含**） | `2026-05-18 09:00:00`、`2026-05-18T09:00:00+08:00` |
| `--db-log-until` | 窗口终点（**含**） | `2026-05-18 18:00:00` |
| 仅 since | 从起点至**采集执行时刻** | `--db-log-since "2026-05-18 09:00:00"` |
| 仅 until | 从**最早可读日志**至终点 | `--db-log-until "2026-05-18 18:00:00"` |

**日志源（按 R-004 / R-020 解析路径自动发现）：**

| 类别 | 典型路径 / 模式 | 说明 |
|------|-----------------|------|
| 库日志目录 | `db_log_path`（如 `/data/yashan/log`） | `alert*.log`、`*.trc`、`trace/`、listener 等 |
| 产品 Home | `$YASDB_HOME/log`、`admin/`、`diag/`（视版本布局） | env `source` 后解析 |
| yasboot | `~/.yasboot/<cluster>_*/log/`、stage 下 install 日志 | 文件名列表 + 时间窗内内容 |
| yasagent / yasom | 与集群名相关的 `log/`、`run/` 目录 | `ps` + 路径推断（R-024） |
| systemd | `yashandb.service` 等（R-025 `autostart.json`） | `journalctl -u <unit> --since ... --until ... --no-pager` |
| 安装 trace | `db-log-path` 与 data 下 `log/` | 与 §5.13.1.B 清单交叉，R-034 采**内容** |

**时间过滤策略（按优先级）：**

1. **行级时间戳**（首选）：对 alert/trace 等按行前缀时间解析（常见 `YYYY-MM-DD HH:MM:SS` 或 ISO），仅输出 `[since, until]` 内行；文件头尾各保留若干上下文行（可选）。
2. **journalctl 原生窗口**：`journalctl --since "$since" --until "$until"`（systemd 时间语法与 CLI 参数互转）。
3. **文件 mtime 回退**：无法行级解析时，仅打包 **mtime ∈ [since, until]** 的文件；整文件纳入时在 `logs/manifest.json` 标记 `"filter_mode": "mtime"`。
4. **压缩归档**：单文件过滤后仍 >10MB 可 gzip（`*.log.gz`），manifest 记录。

**建议落盘结构：**

```
hosts/<host>/db/logs/
  manifest.json           # 时间窗、filter_mode、文件列表、bytes、truncated、warnings
  alert/                  # 过滤后的 alert 类日志
  trace/                  # trace / *.trc
  yasboot/                # yasboot 安装/运行日志
  agent/                  # yasagent/yasom
  journal/                # systemd unit 日志文本
  skipped-files.json      # 因权限/超大/无时间戳而跳过的文件
```

**`logs/manifest.json` 示例：**

```json
{
  "time_window": {
    "since": "2026-05-18T09:00:00+08:00",
    "until": "2026-05-18T18:00:00+08:00",
    "timezone": "Asia/Shanghai"
  },
  "total_bytes": 12345678,
  "max_mb": 500,
  "truncated": false,
  "files": [
    {
      "source_path": "/data/yashan/log/alert_yashandb.log",
      "archive_path": "alert/alert_yashandb.log",
      "filter_mode": "line_timestamp",
      "lines_kept": 4200,
      "bytes": 890000
    }
  ]
}
```

**行为约定：**

1. PreCheck：`since`/`until` 均未设置 → **skipped**（非 failed）；仅一项设置时合法。
2. `since > until` → R-034 **failed**，提示调整参数。
3. 路径不存在 → 记 `skipped-files.json`，不阻断其它日志源。
4. `summary.md`：若 R-034 执行，列出时间窗、文件数、总大小、是否 truncated。
5. 挂钩 `--archive-on-success`：若安装 CLI 传入 `--db-log-since/until`，一并写入 `install-params.json` 并触发 R-034。

**实现参考**：远端过滤可用 `awk`/Python 一行脚本 SSH 执行；journal 用 `journalctl`；路径来自 R-004 `db_log_path` 与 `source env` 后的 `YASDB_HOME`。

#### 5.16.C systemd 固定行数片段（P2，可选）→ **R-035**

无时间窗时的兜底：`journalctl -u <db-service> -n 200`（**R-034 已覆盖带时间窗 journal**，R-035 仅作可选增强）。

---

## 6. 输出格式要求

### 6.1 manifest.json（P0）

- JSON Schema 版本字段 `schema_version: "1.0"`。
- **`profile`**：本次请求的 profile 名（组合时为逗号拼接，如 `db-core,network`）。
- **`categories[]`**：本次实际执行的 **CAT-*** 分类列表（由已执行 R- 步骤反推）。
- **`profiles_expanded`**（可选）：各 profile 展开后的 R- ID 列表，便于审计「组合是否生效」。
- 节点列表、每节点 `meta.json` 相对路径、采集项完成度（`completed` / `failed` / `skipped`）。
- `steps[]` 每项含 `id`, `category`, `host`, `status`, `duration_ms`, `artifacts`。
- 敏感字段统一不出现明文。

### 6.2 summary.md（P0）

- 一页纸概览（**English only**，§3.12.3）：环境类型（Standalone/YAC）、版本、端口、路径、DB 核心、YAC 摘要（若适用）、硬件/网络/内核/绑核摘要；**DB logs**（若 R-034：time window、file count、size）；集群状态与告警项。

### 6.3 机器可读（P1）

- 可选导出 `inventory.csv` 关键 KPI 表，便于 Excel。

---

## 7. 安全与脱敏

| 规则 | 说明 |
|------|------|
| 默认脱敏 | 与 `internal/logging/redact` 规则对齐：password、secret、token、`-p` 等 |
| 配置文件 | yasboot `*.env` 等含密码的键 → 掩码（**不采集** YMP `application.properties`、YCM 配置） |
| 命令行回放 | `install-run.json` 中 `argv` 脱敏 |
| 权限 | 归档目录建议 `0700`；文档注明勿提交 git |
| 可选 `--redact=none` | 仅调试环境，帮助文档强警告 |

---

## 8. 行为与非功能需求

| 类别 | 要求 |
|------|------|
| 只读 | 采集步骤不得调用安装/删除/格式化；仅读文件与查询命令 |
| **Linux 兼容** | 目标端 OS/架构/包管理器与 **db 安装**同模型（§3.11）；分发行版命令分支复用 `common/os` |
| **输出语言** | 终端、session/debug 可读字段、manifest/summary 用户字段 **仅 ASCII 英文**（§3.12.3） |
| **终端日志** | 与 **yinstall db** 相同：`NewLogger` + `ConsoleStep` + `LogErrorExit` + `RunStep`（§3.12.4） |
| **DRY** | 编码前 §12 扫描；禁止重复 B-001/SSH/步骤过滤/脱敏实现（§3.12.1） |
| 容错 | 单节点、单项失败记录 `errors[]`，整体 exit code 可为 0（部分成功）或 1（可配置 `--strict`） |
| 性能 | 单节点采集目标 < 5min（**不含 R-034 日志**）；带 `--db-log-since/until` 时视日志量可至 15min；单节点日志默认上限 **500MB** |
| 幂等 | 重复采集覆盖或版本化子目录 `inventory/<run-id>-<n>` |
| 离线 | 控制端可后续打包 `tar.gz`（`yinstall collect pack -i <dir>` 或独立 `yinstall pack`，二期再定） |
| 测试 | 单元测试：脱敏、manifest 生成；集成测试：mock SSH 或 10.10.10.130 |

---

## 9. 实施分期建议

### Phase 1（MVP）

**编码前**：完成 **§12** 代码扫描 checklist；确认 R-001 复用 B-001、CLI 编排复用 db/os 模式。

- 顶层子命令 `yinstall collect`；`internal/steps/collect/registry.go` 注册 **R-001～R-029 + R-030 + R-034**（R-034 在 R-027 后，R-030 在 R-034 后）。
- 支持 `-l/-s/-e/--dry-run/-f/-F`；**不支持** `--precheck`（collect 专用，见 §3.1）。
- `PrintCollectStepCatalog(profile)`；`yinstall db --archive-on-success` 挂钩执行 R-001～R-029 + **R-003**。
- 输出 `manifest.json`（含逐步 `steps[]`）+ **`summary.md`（English only）** + `hosts/<host>/{os,db}/`。
- 源码注释中文；终端/归档用户可见字符串英文（§3.12）。

### Phase 2

- **R-401～R-403**（standby）；**R-031～R-035** 可选增强步骤。
- checksum；归档打包子命令。

### Phase 3

- 安装前后 diff（UC-03）；上传 S3/MinIO；Web 报告模板。

---

## 10. 验收标准（草案）

1. `yinstall collect -l --profile full` 与 `--profile db-core,network` 列出步骤集不同且后者为并集。
2. `manifest.json` 含 `profile`、`categories[]` 与每步 `category` 字段。
2. **`--db-log-since` + `--db-log-until`**：R-034 success，`hosts/<ip>/db/logs/manifest.json` 中 `time_window` 正确，文件内容不含窗口外行（行级过滤）或 manifest 标注 `filter_mode: mtime`。
3. 未传 `--db-log-since/until`：R-034 **skipped**，R-022 仍有 `db-log-dir.txt` 清单。
4. **`yinstall collect -t <ip> -u root`（无 cluster-name/port）**：R-004 成功。
5. **YAC 三节点** 产出 `yac/` + `cluster.json`（R-030）。
6. 归档包无 SYS 密码明文。
7. 单节点 full **< 5min**（不含 R-028/R-034）；带 R-034 且 500MB 内 **< 15min**。
8. 单机 collect 时 R-030 skipped。
9. **R-001** 在 OL8 / 麒麟 V10 / UOS 目标上写入正确 `meta.json` → `os.family` 与 `arch`；R-013/R-015 按 OS 家族选用对应 grub/网络采集路径（见 §3.11.3）。
10. 终端输出与 `yinstall db` 同格式（`ConsoleStep` start/success/fail/skip、`LogErrorExit` 块）；session/debug 双日志落盘。
11. `summary.md` 与 `manifest.json` 的 `warnings[]`/`errors[]` 无中文/非 ASCII；R- 步 Name/Description 为英文。

---

## 11. 开放问题（待产品确认）

1. ~~子命令命名~~：**已定为顶层 `yinstall collect`**（归档输出目录仍可用 `./inventory/` 命名，与 CLI 子命令名无关）。
2. ~~独立采集时 SYS 密码~~：**已定为 `yasql / as sysdba`（产品用户 + env 文件），与 DB 安装 C-031/C-033 相同，collect 不提供 `--db-sys-password` 专用于采集**。
3. ~~独立采集 cluster/port~~：**R-004 自动发现 env；`--cluster-name` / `--db-begin-port` / `--env-file` 仅作可选覆盖**（§3.9）。
4. 归档包是否纳入 **合规保留年限** 与命名规范（项目名/环境/日期）？
5. 是否与现有 `clean` 子命令联动（清理前自动 collect）？
6. **installer.md** 是否同步维护「交付物清单」章节？

---

## 12. 参考（代码锚点）

| 能力 | 代码位置 |
|------|----------|
| OS 步骤全集 | `internal/steps/os/registry.go`（B-001～B-031） |
| DB 步骤全集 | `internal/steps/db/registry.go`（C-001～C-033） |
| OS 探测与分支 | `internal/common/os/os.go`；`internal/steps/os/b001_check_connectivity.go`（R-001 对齐 B-001） |
| CLI / Profile | `internal/cli/collect.go`, `collect_profiles.go` |
| 集群状态展示 | `internal/steps/db/c029_show_cluster_status.go` |
| env 自动发现 | `internal/steps/standby/primary_env.go`；collect **R-004** `r004_discover_product_env.go` |
| env 文件路径规则 | `internal/common/os/env.go` `DetermineEnvFile` |
| resolveDBEnvFile | `internal/steps/db/c032_configure_unified_audit.go` |
| yasql sysdba（无密码） | `internal/common/sql/yasql.go`（`ExecuteSQLAsSysdbaCtx`、`/ as sysdba`） |
| 日志脱敏 | `internal/logging/logger.go` |
| 编码约束 | §3.12（DRY / 英文输出 / 终端日志）；§12（复用清单） |

---

## 12. 编码前代码扫描与 DRY 复用清单

> 以下为 **2026-05-26 对当前仓库的全量扫描结论**（`internal/` + `cmd/`）。实现 collect 时按表 **直接 import/调用**，不得平行实现。

### 12.1 CLI 与编排层

| 模块 | 路径 | collect 用法 |
|------|------|--------------|
| 根命令 / 全局 flag | `internal/cli/root.go` | `-t/-u/-p`、`--log-dir`、`--run-id`、`-s/-e/-f/-F`、`--dry-run`；collect **不注册** `--precheck` |
| 步骤过滤 | `internal/cli/steps_util.go` | `filterSteps`、`parseStepRanges`、`normalizeStepID` |
| 步骤目录打印 | `internal/cli/step_catalog.go` | `printStepSection`；新增 `PrintCollectStepCatalog` |
| SSH 执行器 | `internal/cli/os.go` | `createExecutor`、`HostInfo`、`runnerExecAdapter` |
| OS flag 共享 | `internal/cli/os_flags.go` | collect 注册 `--os-user` 等（R-004/R-020+）；`registerAllOSFlags(cmd, {forDB:false})` 或最小子集 |
| DB 路径默认 | `internal/cli/db_paths.go` | 挂钩安装时路径推导参考 |
| 参数校验 | `internal/cli/validate.go` | 端口等校验复用 |
| **编排模板** | `internal/cli/db.go`、`os.go` | Phase1 R-001 逐 host → Phase2 按 Global/PerHost 执行；YAC 时 `TargetHosts` |

### 12.2 步骤运行时

| 模块 | 路径 | collect 用法 |
|------|------|--------------|
| Step 定义与执行 | `internal/runner/step.go` | `Step`、`StepContext`、`RunStep`、`ExecuteWithCheck`、`ForHost`、`HostsToRun` |
| 命令失败类型 | `internal/runner/exec_error.go` | `CommandExitError`、`CommandExitLogged` |
| 日志 | `internal/logging/logger.go` | `NewLogger`、`ConsoleStep`、`ConsoleNotice`、`LogErrorExit`、`LogCommand*` |
| SSH | `internal/ssh/executor.go`、`upload.go` | 远端执行；collect 只读不上传（R-028 拷贝控制端日志除外） |

### 12.3 OS 探测与分发行版（§3.11）

| 模块 | 路径 | collect 用法 |
|------|------|--------------|
| OS 检测 | `internal/common/os/os.go` | `DetectOSInfo`、`ParseOSRelease`、`DetectOSType`、`IsRHEL*`、`IsKylin`、`IsUOS`、`GetPkgManager`、`GetArch` |
| 远端执行封装 | `internal/common/os/exec.go` | `ExecuteAsUserWithCheck`、`ExecuteAsUserWithEnvCheck`、`*Quiet` 变体 |
| 用户/家目录 | `internal/common/os/env.go`、`user.go` | `GetUserHomeDir`、`DetermineEnvFile` |
| 网络 | `internal/common/os/net.go` | IP/CIDR/ping（YAC 网络采集） |
| 磁盘 | `internal/common/os/disk.go` | 磁盘/LVM 只读命令参考 |
| 包管理 | `internal/common/os/pkg.go` | `rpm -qa` / repolist 分支参考 |
| sysctl | `internal/common/os/sysctl_shm.go` | R-013 内核参数采集参考 |
| **B-001 连通** | `internal/steps/os/b001_check_connectivity.go` | **R-001 直接复用**（或 registry 别名） |
| 分支参考（只读） | `b008_write_sysctl.go`、`b014_write_yum_repo.go`、`b015_install_deps.go`、`b024_install_multipath.go` | 命令选择，**不**调用 Action 写配置 |

### 12.4 DB / env / SQL

| 模块 | 路径 | collect 用法 |
|------|------|--------------|
| env 发现 | `internal/steps/standby/primary_env.go` | `GetPrimaryEnvFile`、`ClusterNameFromEnvFileContent`、`SyncPrimaryClusterNameFromEnvFile` |
| yasql | `internal/common/sql/yasql.go` | `ReportSQLFailure`、sysdba 连接模式 |
| env 文件解析 | `internal/steps/db/c032_configure_unified_audit.go` | `resolveDBEnvFile` 模式 |
| 集群状态 | `internal/steps/db/c029_show_cluster_status.go` | R-023 参考 |
| tom/hosts | `internal/steps/db/c010_write_hosts.go`、`c012_disk_check.go` | YAC 存储/网络采集参考 |
| 网络预检 | `internal/steps/db/precheck_network_validate.go` | YAC 网段逻辑只读参考 |
| yasboot 以用户执行 | `internal/steps/standby/yasboot_run_as_user.go` | `runYasbootOnPrimaryWithEnvFile*` |

### 12.5 禁止重复实现（红线）

| 禁止 | 应改为 |
|------|--------|
| 新建 `collectSSHClient` | `createExecutor` + `runnerExecAdapter` |
| 复制 B-001 PreCheck 到 R-001 | 注册 `StepB001CheckConnectivity` 或共享 PreCheck 函数 |
| 自建步骤进度 `fmt.Println` | `Logger.ConsoleStep` |
| 自建错误打印 | `LogErrorExit` / `ExecuteWithCheck` |
| 自建 step range 解析 | `filterSteps` |
| 自建 OS 类型判断字符串匹配 | `ctx.OSInfo` + `common/os` helpers |
| 中文终端提示 | 英文 `error` / `ConsoleNotice` |
| collect 专用脱敏 | `logging.redact` |

### 12.6 建议新增文件（Phase 1 唯一增量）

```
internal/cli/collect.go           # 子命令 RunE，镜像 db.go 编排
internal/cli/collect_profiles.go  # profile -> categories -> step IDs
internal/steps/collect/registry.go
internal/steps/collect/r002_*.go  # 每步一文件（仅 Action + 归档写盘）
...
internal/cli/step_catalog.go      # +PrintCollectStepCatalog
internal/logging/logger.go        # 可选：ConsoleStep stepKind 参数（§3.12.4-a）
```

**可选重构（非 MVP 阻塞）**：从 `os.go`/`db.go` 抽取 `runConnectivityPhase` / `runPerHostSteps` 到 `internal/cli/runner_host.go` 供 collect 三处共用——若抽取则 collect **必须**调用抽取函数，不得再复制第三份循环。

### 12.7 编码前 Checklist

- [ ] 阅读 §3.12、§12 全文
- [ ] R-001 确认复用 B-001
- [ ] `collect.go` 使用 `NewLogger` + `RunStep` + `filterSteps`
- [ ] 所有 R- `Name`/`Description`/错误信息英文
- [ ] Go 注释中文
- [ ] 单元测试：`collect_profiles` 展开、英文 message 无 CJK 字符（可用 `\p{Han}` 检测）
| Collect 步骤注册 | `internal/steps/collect/registry.go`（含 **R-034** `r034_collect_db_logs.go`） |
| YAC 安装参考 | `internal/steps/db/c010_write_hosts.go`、`c012_disk_check.go`、`c019_tune_yfs_params.go`、`precheck_network_validate.go` |
| 步骤列表 CLI | `internal/cli/step_catalog.go` `PrintCollectStepCatalog` |
| 步骤过滤 | `internal/cli/steps_util.go`（与 B/C 共用 `parseStepRanges`） |
| Step 执行 | `internal/runner/step.go` `RunStep`（skip/optional/dry-run；collect 不传 precheck 模式） |

---

## 修订记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1 | 2026-05-26 | 初稿：采集范围、归档结构、分期与验收 |
| v0.2 | 2026-05-26 | 明确排除 YMP/YCM 采集；profile 仅 full/os/db/standby |
| v0.3 | 2026-05-26 | 新增 §5.4.1：主机 dmidecode（P0）采集项与落盘结构 |
| v0.4 | 2026-05-26 | 新增 §5.8.1：全量网卡与 bond/team/bridge 采集（P0） |
| v0.5 | 2026-05-26 | 新增 §5.6.1：内核参数全量采集（sysctl -a、/proc/sys、启动与持久化配置） |
| v0.6 | 2026-05-26 | CLI 定为顶层子命令 `yinstall collect`（取消 `inventory collect`） |
| v0.7 | 2026-05-26 | 新增 §5.13.1：DB 核心（数据文件、REDO、参数全量/对照）采集 |
| v0.8 | 2026-05-26 | §5.13.1.C 改为 `yasql / as sysdba`，不要求 SYS 密码（与 DB 安装一致） |
| v0.9 | 2026-05-26 | 新增 §3.3～§3.6：R-001～R-403 步骤注册表；与 B/C 同构的步骤管理能力 |
| v0.10 | 2026-05-26 | 新增 §3.7 步骤管理对照表、§5.0 R-001；§5 各节与 R- 编号一一映射 |
| v0.11 | 2026-05-26 | `yinstall collect` 明确不支持 `--precheck` CLI；示例与 Phase 1 范围同步 |
| v0.12 | 2026-05-26 | 新增 **R-004** 与 §3.8：独立采集自动发现 env/集群名/端口；CLI 覆盖项改为可选 |
| v0.13 | 2026-05-26 | 新增 §5.8.2 网卡绑核（IRQ/RPS/XPS）、§5.14.1 DB 进程绑核；R-015/R-024 扩展 |
| v0.14 | 2026-05-26 | 新增 §3.9、§5.17 与 **R-030**：YAC 集群级采集（VIP/SCAN/diskgroup/YFS/一致性）；`yac/` 目录 |
| v0.15 | 2026-05-26 | 新增 **R-034** 与 §5.16.B：数据库日志按 `--db-log-since/until` 时间窗采集；`db/logs/` |
| v0.16 | 2026-05-26 | 新增 §3.2 **CAT 分类**、§3.3 **profile 组合**（含 baseline/db-core/db-logs 等）；注册表增加分类列 |
| v0.17 | 2026-05-26 | 新增 §3.11：目标端 Linux 兼容性与 db 安装对齐（OS 探测、分发行版采集、容错） |
| v0.18 | 2026-05-26 | 新增 §3.12 编码约束（DRY、注释中文、输出仅英文、终端日志同 db）与 **§12 全量代码复用清单** |
