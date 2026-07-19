# yinstall（yasinstaller）

面向 YashanDB 生态的 **自动化安装与运维编排 CLI**。在控制端（macOS/Linux）通过 SSH 在目标 Linux 主机上，按预定义步骤执行 OS 基线、数据库安装、主备扩容、YCM/YMP 部署、环境清理、诊断采集与 OS 压测。

**开源仓库**：[https://github.com/huangtingzhong/yinstall](https://github.com/huangtingzhong/yinstall)

主程序名为 **`yinstall`**（早期版本曾用 `yasinstall`）。仓库目录名本地常为 `yasinstaller`，GitHub 仓库名为 `yinstall`。

---

## 功能概览

| 子命令 | 说明 | 步骤前缀 |
|--------|------|----------|
| `os` | OS 基线（用户、内核、依赖、存储、YAC 多路径等） | B- |
| `db` | YashanDB 单机 / YAC 集群安装 | C-（可选 B-） |
| `standby` | 向已有主库添加备库 | E- |
| `ycm` | 安装 YCM（云管） | G- |
| `ymp` | 安装 YMP（迁移平台） | H- |
| `clean` | 卸载清理 DB / YCM / YMP | CLEAN- |
| `collect` | 只读采集 OS/DB 环境并本地归档 | R- |
| `stressos` | CPU/MEM/IO/NET 压测并归档 | S- |

扩展能力（开发中/专项场景）：`mysql`、`mssql`（含 install / standby / mirror / ag 等子命令）。

每个子命令支持 `yinstall <cmd> -l` 查看步骤目录；`yinstall <cmd> --help` 查看完整参数。

---

## 构建

```bash
git clone https://github.com/huangtingzhong/yinstall.git
cd yinstall

make build-current          # 当前平台 → build/yinstall_<os>_<arch>
make build-all              # Linux / Windows / macOS 多架构
./build.sh --current        # 等同 make build-current

make check-debug-logging    # 安装步骤 debug 日志静态检查（需本地 scripts/）
```

输出示例：`build/yinstall_darwin_arm64`、`build/yinstall_linux_amd64`。

---

## 快速开始

### 单机数据库（跳过 OS，包放 `./software/`）

```bash
./build/yinstall_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m) db \
  -t 10.10.10.130 \
  --skip-os \
  --os-user yashan \
  --db-admin-password 'YourSaPassword' \
  --precheck
```

`--db-package` 可省略：C-007 会在 `-R`、`$HOME`、`-L`（默认 `./software`、`./pkg` 等）中按版本自动发现最新安装包。

### OS 基线 + 安装

```bash
yinstall os -t 10.10.10.130
yinstall db  -t 10.10.10.130 --os-user-password '...'
```

`os` / `db` 成功后默认 **`--archive`** 挂钩采集（可用 `--archive=false` 关闭）。

### 单步调试

```bash
yinstall db -t 10.10.10.130 --skip-os --precheck -s C-007
yinstall db -t 10.10.10.130 --dry-run -s C-014-C-021
```

### 诊断采集 / 压测

```bash
yinstall collect -t 10.10.10.130 --profile full -o ./output/collect
yinstall stressos -t 10.10.10.130 --cpu --mem --io -o ./output/stress
```

### YAC 主库扩 CE 备（`--yac`）

主库为 CE/YAC 时加 `--yac`，走 `config group gen -t ce` + `group add`（不是 SE 的 `node add`）。必填：`--yac-inter-cidr`、`--yac-systemdg`、`--yac-datadg`、`--yac-vips`（数量=备节点数）、`--db-admin-password`。`--yac-public-network` 可省略（E-002 可从主库 toml 继承）。

`--yac-systemdg` / `--yac-datadg` 为逗号分隔盘路径（如 `/dev/yfs/sys1,/dev/yfs/sys2`）；仍兼容旧 `role:/dev/...`。产品安装后 YFS 组名固定为 `SYSTEM` / `DG0`，工具不支持改名。

**生产注意**：CE 路径默认仍会写主库 `REPLICATION_ADDR` 到 **SPFILE**，但**不会**自动重启（`--standby-restart-primary` 默认 `false`）。落盘后需在维护窗口 `cluster stop/start`，再重跑 `yinstall standby`；实验/维护窗口可显式 `--standby-restart-primary` 让工具顺带重启。

```bash
yinstall standby \
  -t 10.10.10.182,10.10.10.183 \
  --primary-ip 10.10.10.172 \
  --yac \
  --yac-inter-cidr 10.10.234.0/24 \
  --yac-systemdg '/dev/yfs/sys1,/dev/yfs/sys2,/dev/yfs/sys3' \
  --yac-datadg '/dev/yfs/data1,/dev/yfs/data2' \
  --yac-vips 10.10.10.184/24,10.10.10.185/24 \
  --db-admin-password 'YourSysPassword' \
  --os-user yashan --os-user-password '...' \
  --precheck
```

失败时默认**报错并打印清理方案**；加 `--standby-cleanup-on-failure`（或全局 `-F`）才会自动安全清理（仅本次新增/失败 group，保护已有 open 备组与 ceg1）。E-014 按本次 `--targets` IP（及新 group 名）等待，不靠全集群 standby 计数。扩前会探测已有 ceg 并打印 expected next group。E-011 生成前备份并修补 `hosts_add.toml` / `<cluster>_add.toml`；探测主库 datafile/redo/arch 后：备侧 **data 盘组名对齐主库众数组**（CE 不支持把主 data 组改成异名再靠 CONVERT 建库）；主库若有额外 data 组则自动写 `DB_FILE_NAME_CONVERT` 映到众数组；redo 落点按 REDO→ARCH→data 回退并必要时写 `REDO_FILE_NAME_CONVERT`；同时写 `ARCHIVE_LOCAL_DEST`（探测 SQL 失败则硬失败）。运维细节见本地 `docs/plans/2026-07-18-yac-to-yac-standby.md`。

---

## 执行模型

每个步骤：**PreCheck → Action → PostCheck**

| 模式 | 行为 |
|------|------|
| 默认 | PreCheck 通过后执行 Action/PostCheck |
| `--precheck` | 仅 PreCheck |
| `--dry-run` | PreCheck 后跳过 Action/PostCheck |
| `-s` / `-e` | 包含 / 排除步骤（支持范围如 `B-001-B-010`） |
| `-F` / `-f` | 强制全部 / 指定步骤（可能删除已有资源） |

日志：Session 日志 + Debug 日志，默认目录 `--log-dir`（默认 `./logs`）。

---

## 常用全局参数

| 参数 | 简写 | 说明 |
|------|------|------|
| `--targets` | `-t` | 目标主机（逗号分隔）；未指定时本机执行 |
| `--ssh-user` | `-u` | SSH 用户（默认 `root`） |
| `--ssh-password` | `-P` | SSH 密码（未指定时可尝试密钥） |
| `--ssh-key-path` | | 私钥路径（默认 `~/.ssh/id_rsa`） |
| `--local-software-dirs` | `-L` | 控制端软件目录（默认 `./software`、`./pkg`、当前目录 `.`、`$HOME`，及存在时的 `~/Downloads/yashan`、`~/Downloads/oracle`） |
| `--remote-software-dir` | `-R` | 目标机软件目录（默认 `/data/yashan/soft`；查找/上传时另扫描 SSH 登录用户 `$HOME`） |
| `--include-steps` | `-s` | 只执行指定步骤 |
| `--exclude-steps` | `-e` | 排除步骤 |
| `--list-steps` | `-l` | 打印步骤列表后退出 |
| `--output` | `-o` | collect/stress 归档目录 |
| `--archive` | `-a` | 安装成功后自动 collect（os/db 默认开启） |

完整参数以 `yinstall <command> --help` 为准。

---

## 项目结构

```text
cmd/yinstall/          # 入口
internal/cli/          # 子命令与参数
internal/runner/       # 步骤编排
internal/steps/        # 各域步骤实现（os/db/ycm/ymp/...）
internal/ssh/          # SSH 执行与上传
internal/common/       # 公共逻辑
build/                 # 编译产物（不入库）
docs/                  # 本地文档（默认不入库）
tmp/ scripts/          # 本地临时/脚本（不入库）
```

---

## 文档（本地）

以下文件在 `.gitignore` 中，clone 后需在本机维护：

| 路径 | 说明 |
|------|------|
| `docs/02-product/01-product-manual.md` | 工具使用手册**总目录** |
| `docs/02-product/01-overview.md` 等分册 | 概述、模块、YAC、案例、参数等（见总目录） |
| `docs/02-product/02-step-logic.md` | 步骤 PreCheck/Action 参考 |
| `docs/installer.md` | 开发者 API、Params、排障 |

---

## 开发验证

```bash
go fmt ./...
go vet ./...
go test ./... -count=1
```

测试机参考（内网）：Linux 单机 `10.10.10.130` (aarch64)；YAC `10.10.10.125`、`10.10.10.126`；Windows `10.10.10.185` (x86_64)。

---

## 许可证与联系

构建信息见 `yinstall --version`（含构建时间、Git commit）。

参数与步骤以当前二进制 `yinstall <command> -h` / `-l` 为准。
