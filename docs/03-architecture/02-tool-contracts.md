# 工具契约（Tool Contracts）

## 设计目标
把 `yinstall` 的能力包装成“工具函数”，使模型只能在 **受控参数** 范围内调用安装能力，并能得到 **结构化输出** 用于后续决策与审计。

## 通用输入（所有 yinstall 工具共享）
对应 `internal/cli/root.go` 的全局参数（见仓库 `README.md` 2.2 节）。

### 目标与 SSH
- `targets`: string[]（必填；MVP 不开放 local 模式）
- `ssh_user`: string（默认 `root`）
- `ssh_port`: int（默认 22）
- `ssh_auth`: `"password" | "key"`（默认 `password`）
- `ssh_password`: string（可选；建议运行时注入，不落盘）
- `ssh_key_path`: string（可选）
- `sudo`: boolean（默认 true）

### 执行控制
- `dry_run`: boolean（默认 false）
- `precheck`: boolean（默认 false）
- `include_steps`: string[]（例如 `["B-001","C-010-C-020"]`）
- `exclude_steps`: string[]
- `force_all`: boolean（危险）
- `force_steps`: string[]（危险）
- `force_delete_user`: boolean（高危，默认禁止）
- `log_redact`: boolean（默认 false；为 true 时 session/debug 日志对 `-p`/password 等做 `***REDACTED***`）

### 目录与日志
- `local_software_dirs`: string[]（默认 `["./software","./pkg","~/Downloads/yashan(存在才加入)"]`）
- `remote_software_dir`: string（默认 `/data/yashan/soft`）
- `log_dir`: string（默认 `logs`）
- `run_id`: string（可选；建议由 Agent 生成并注入）

## 通用输出（所有工具返回）
- `ok`: boolean
- `exit_code`: int
- `command`: string（最终执行的 `yinstall ...`，用于审计；敏感字段需脱敏）
- `log_dir`: string
- `logs`: { `session_log`: string, `debug_log`: string }（尽力推断/定位）
- `summary`: string（人类可读摘要）
- `artifacts`: object（可选：解析出的关键结果，例如端口、安装路径等）

## 工具列表（MVP 先做 db）

### Tool: `yinstall_db_precheck`
**用途**：执行 `yinstall db --precheck`（或 `--dry-run`），用于安装前检查。  
**风险**：R0  
**强制约束**：
- 必须 `precheck=true`（或 `dry_run=true`）二选一
- 禁止 `force_all / force_steps / force_delete_user`

**映射命令（示意）**：
`yinstall db --precheck --targets ... [ssh flags...] [dirs/log flags...]`

### Tool: `yinstall_db_apply`
**用途**：执行 `yinstall db` 完整安装。  
**风险**：R2（如果包含磁盘/清理相关步骤，提升到 R3）  
**强制约束**：
- 需要 `approval_token`（由 Agent CLI 在交互模式下生成/校验）
- 默认 `force_delete_user=false` 且禁止开启
- 如果 `force_all=true` 或 `force_steps` 非空：必须二次确认（交互）或更高等级审批

**映射命令（示意）**：
`yinstall db --targets ... [db flags...] [ssh flags...]`

### Tool: `yinstall_list_steps`
**用途**：列出某子命令的步骤目录（`-l/--list-steps`）。  
**风险**：R0  
**说明**：用于生成“将执行哪些 step”的可解释计划。

## 工具列表（全功能）

> 说明：下面每个子命令都按统一模式拆成 3 类工具：
> - `*_list_steps`：列步骤目录（R0）
> - `*_precheck`：`--precheck` 或 `--dry-run`（R0）
> - `*_apply`：真实执行（R2/R3，需要审批/确认）

### 0) 通用工具：`yinstall_list_steps`
**用途**：`yinstall <subcmd> -l` 列出步骤目录，用于解释“将执行什么”。  
**风险**：R0

---

### 1) DB：`yinstall db`

#### Tool: `yinstall_db_list_steps`
**用途**：`yinstall db -l`  
**风险**：R0

#### Tool: `yinstall_db_precheck`
**用途**：`yinstall db --precheck`（或 `--dry-run`）  
**风险**：R0  
**强制约束**：禁止 `force_all / force_steps / force_delete_user`

#### Tool: `yinstall_db_apply`
**用途**：`yinstall db`  
**风险**：R2/R3  
**门禁**：需要 `approval_token`；`force_delete_user` 默认禁止

#### DB 参数（来自 `internal/cli/db.go`，Design 阶段固化）
- **OS 相关（仅当 `skip_os=false` 生效）**
  - `skip_os`(bool, default false) → `--skip-os`
  - `os_user`(string, default `yashan`) → `--os-user`
  - `os_user_password`(string, default `aaBB11@@33$$`) → `--os-user-password`（敏感）
  - `os_group`(string, default `yashan`) → `--os-group`
  - `os_ignore_install_errors`(bool) → `--os-ignore-install-errors`
  - `os_timezone`(string, default `Asia/Shanghai`) → `--os-timezone`
  - `os_ntp_server`(string, default `ntp.aliyun.com`) → `--os-ntp-server`
  - `os_yum_mode`(string, default `none`) → `--os-yum-mode`
  - `os_iso_device`(string, default `/dev/cdrom`) → `--os-iso-device`
  - `os_iso_mountpoint`(string, default `/media`) → `--os-iso-mountpoint`
  - `os_yum_repo_file`(string, default `/etc/yum.repos.d/local.repo`) → `--os-yum-repo-file`
  - `os_deps_db_packages`(string, default `libzstd zlib lz4 openssl openssl-devel libnsl libaio`) → `--os-deps-db-packages`
  - `os_deps_tools_packages`(string, default empty) → `--os-deps-tools-packages`
  - `os_firewall_mode`(string, default `disable`) → `--os-firewall-mode`
  - `os_firewall_ports`(string, default empty) → `--os-firewall-ports`
  - `os_hugepages_enable`(bool, default false) → `--os-hugepages-enable`
- **DB 基本参数**
  - `db_cluster_name`(string, default `yashandb`) → `--db-cluster-name`
  - `db_begin_port`(int, default 1688) → `--db-port`
  - `db_memory_percent`(int, default 50) → `--db-memory-percent`
  - `db_character_set`(string, default `utf8`) → `--db-character-set`
  - `db_use_native_type`(bool, default false) → `--db-use-native-type`
  - `db_sys_password`(string, default `Yashan1!`) → `--db-sys-password`（敏感）
  - `db_home_path`(string, default `/data/yashan/yasdb_home`) → `--db-home-path`
  - `db_data_path`(string, default `/data/yashan/yasdb_data`) → `--db-data-path`
  - `db_log_path`(string, default `/data/yashan/log`) → `--db-log-path`
  - `db_stage_dir`(string, default `/home/yashan/install`) → `--db-stage-dir`
  - `db_package`(string, required for真实安装) → `--db-package`
  - `db_deps_package`(string, optional) → `--db-deps-package`
  - `db_redo_file_num`(int, default 6) → `--db-redo-file-num`
  - `db_redo_file_size_mb`(string, default `128`) → `--db-redo-file-size`
  - `db_disable_archivelog`(bool, default false) → `--db-disable-archivelog`
  - `db_custom_sql_script`(string, optional) → `--db-custom-sql-script`
  - `yasboot_gen_extra_args`(string, optional) → `--yasboot-gen-extra-args`（`package se/ce gen`）
  - `yasboot_deploy_extra_args`(string, optional) → `--yasboot-deploy-extra-args`（`cluster deploy`）
- **YAC（多节点/集群）参数（当 `--yac` 或 targets>=2 时）**
  - `yac_systemdg`/`yac_datadg`/`yac_archdg` 等（磁盘组）
  - `yac_inter_cidr`、`yac_public_network`、`yac_access_mode(vip/scan)`、`yac_vips`、`yac_scanname`、`yac_scan_ips`
  - `yac_disk_found_path`、磁盘发现：`yac_disk_pattern`、`yac_exclude_disks`（**B-021** 与 **`db --skip-os` 的 C-001B** 均生效；支持 `/dev/yfs/data2`、`data2`、`/dev/sdX` 等）、`yac_systemdg_size_max`、`yac_auto_confirm`
  - **`--enable-branch` 单节点**：`datadg` 通常仅 1 块 data 盘，可用 `--yac-exclude-disks` 排除多余 `data*`
  - **C-001B（`db --skip-os`）udev 磁盘发现**：`yac_auto_discover_disks`（`--skip-os` 时默认 true）、`yac_discover_root`（默认 `/dev/yfs`）、`yac_discover_fallback_mapper`。支持单节点 `--yac`（仅校验本机 `/dev/yfs` 布局，无跨节点 alias 比对）。前置条件：目标机已有 udev 产出的 `sys*`/`data*`（通常在 `/dev/yfs/`）；不安装 multipath、不写 udev 规则。与 B-021 互斥（全量 OS 仍走 B-021）。
  - **产品用户密码与 SSH**：未显式 `--os-user-password` 且 `-u`/`--ssh-user` 与 `--os-user` 相同、非 `key` 认证且提供了 `--ssh-password` 时，`os_user_password` 自动与 SSH 登录密码一致（否则默认 `aaBB11@@33$$`）。
  - **C-001P（YAC 密码）**：`yac_ensure_os_password`（`--yac-ensure-os-password`，默认 true）在 `ce gen` 前从控制端校验各节点 `os_user` 密码 SSH；失败且具备 root/sudo 时重置为 `--os-user-password`；首节点另做 sshpass 网格探测（与 yasboot scan 一致）。`--precheck` 仅校验不改密。
  - **C-014 YAC `package ce gen`**：不传 `--memory-limit`、`--mode mysql`（与 `installer.md` YAC 示例一致）；`--db-memory-percent` 仅用于 C-014 前 kernel shm 校验。单机 `package se gen` 仍可使用 `--memory-limit` 与 `--db-mode mysql`。
  - YFS tuning：`yac_yfs_tune`、`yac_yfs_au_size`、`yac_redo_file_size`、`yac_redo_file_num`、`yac_shm_pool_size`、`yac_max_instances`

---

### 2) Standby：`yinstall standby`（创建备库）

#### Tool: `yinstall_standby_list_steps`
**用途**：`yinstall standby -l`  
**风险**：R0

#### Tool: `yinstall_standby_precheck`
**用途**：`yinstall standby --precheck`（或 `--dry-run`）用于校验主库连通性、参数、可执行步骤目录。  
**风险**：R0  
**强制约束**：禁止高危 `--standby-cleanup-on-failure` 默认开启。

#### Tool: `yinstall_standby_apply`
**用途**：`yinstall standby` 执行扩容（在主库与备库节点分别执行步骤）。  
**风险**：R2/R3（清理失败现场或强制动作时上升）  
**门禁**：需要 `approval_token`；若启用 `standby_cleanup_on_failure` 需二次确认 + 更高审批

#### Standby 关键参数（来自 `internal/cli/standby.go`）
- `primary_ip`(string, required) → `--primary-ip`
- `primary_os_user`(string, default `yashan`) → `--primary-os-user`
- `primary_env_file`(string, optional) → `--primary-env-file`
- `primary_ssh_user/password/key`（可继承全局）→ `--primary-ssh-user/--primary-ssh-password/--primary-ssh-key`（敏感）
- `targets`(string[]) → 备库节点 `--targets`
- `skip_os`(bool, default true) → `--skip-os`（备库侧 OS baseline）
- `os_user/os_user_password/os_group`（备库侧）→ `--os-user/--os-user-password/--os-group`（敏感）
- `db_cluster_name`(string, default `yashandb`) → `--db-cluster-name`
- `db_port`(int, default 1688；可从主库 LISTEN_ADDR 推导) → `--db-port`
- `db_home_path/db_data_path/db_log_path/db_stage_dir`（可选；默认与 db 一致）→ `--db-home-path/...`
- `db_deps_package`(optional) → `--db-deps-package`
- `yac_mode`(bool) → `--yac`（`db`/`os`；单节点须显式传 `--yac`）
- `standby_cleanup_on_failure`(bool, dangerous) → `--standby-cleanup-on-failure`
- `yasboot_gen_extra_args`(string) → `--yasboot-gen-extra-args`
- `yasboot_deploy_extra_args`(string) → `--yasboot-deploy-extra-args`

---

### 3) YMP：`yinstall ymp`

#### Tool: `yinstall_ymp_list_steps`
**用途**：`yinstall ymp -l`  
**风险**：R0

#### Tool: `yinstall_ymp_precheck`
**用途**：`yinstall ymp --precheck`（或 `--dry-run`）  
**风险**：R0

#### Tool: `yinstall_ymp_apply`
**用途**：`yinstall ymp` 安装 YMP  
**风险**：R2（涉及创建用户/安装软件/开端口）  
**门禁**：需要 `approval_token`；若启用 `ymp_cleanup` 需二次确认

#### YMP 关键参数（来自 `internal/cli/ymp.go`）
- `skip_os`(bool, default true) → `--skip-os`
- `os_yum_mode/os_iso_device/os_iso_mountpoint/os_yum_repo_file`（可选）→ `--os-yum-mode/...`
- `ymp_user/ymp_user_password` → `--ymp-user/--ymp-user-password`（敏感）
- `ymp_package`(string, optional auto-search) → `--ymp-package`
- `ymp_install_dir`(string, default `/opt/ymp`) → `--ymp-install-dir`
- `ymp_port`(int, default 8090) → `--ymp-port`
- `ymp_jdk_enable`(bool) → `--ymp-jdk-enable`
- `ymp_jdk_version`(string, default `17`) → `--ymp-jdk-version`
- `ymp_jdk_package`(string, required when enable) → `--ymp-jdk-package`
- `ymp_instantclient_basic/sqlplus` → `--ymp-instantclient-basic/--ymp-instantclient-sqlplus`
- `ymp_db_package` → `--ymp-db-package`
- `ymp_oracle_env_file` → `--ymp-oracle-env-file`
- `ymp_deps_packages`(string, default `libaio lsof`) → `--ymp-deps-packages`
- `ymp_db_mode`(string, default `yashandb`) → `--ymp-db-mode`
- `ymp_cleanup`(bool, dangerous) → `--ymp-cleanup`

---

### 4) YCM：`yinstall ycm`

#### Tool: `yinstall_ycm_list_steps`
**用途**：`yinstall ycm -l`  
**风险**：R0

#### Tool: `yinstall_ycm_precheck`
**用途**：`yinstall ycm --precheck`（或 `--dry-run`）  
**风险**：R0

#### Tool: `yinstall_ycm_apply`
**用途**：`yinstall ycm` 安装 YCM  
**风险**：R2  
**门禁**：需要 `approval_token`；当 driver=yashandb 时必须提供 DB 管理员密码（敏感）

#### YCM 关键参数（来自 `internal/cli/ycm.go`）
- `skip_os`(bool, default true) → `--skip-os`
- `os_user/os_user_password/os_group` → `--os-user/--os-user-password/--os-group`（敏感）
- `os_yum_mode/os_iso_device/os_iso_mountpoint/os_yum_repo_file`（可选）→ `--os-yum-mode/...`
- `ycm_package`(optional auto-search) → `--ycm-package`
- `ycm_install_dir`(default `/opt`) → `--ycm-install-dir`
- `ycm_deploy_file`(default `<install_dir>/ycm/etc/deploy.yml`) → `--ycm-deploy-file`
- ports：`ycm_port`(9060)、`ycm_prometheus_port`(9061)、`ycm_loki_http_port`(9062)、`ycm_loki_grpc_port`(9063)、`ycm_yasdb_exporter_port`(9064)
- backend：`ycm_db_driver`(sqlite3/yashandb)、`ycm_db_url`、`ycm_db_lib_path`、`ycm_db_admin_user`、`ycm_db_admin_password`（敏感）
- deps：`ycm_deps_packages`(default `libnsl`)

---

### 5) Clean：`yinstall clean`（删除环境）

#### Tool: `yinstall_clean_list_steps`
**用途**：`yinstall clean -l`  
**风险**：R0

#### Tool: `yinstall_clean_precheck`
**用途**：在 apply 前做“影响面评估”：目标主机、目录、将停止的进程、YAC 磁盘清理范围。  
**风险**：R0/R1（仅评估不执行）

#### Tool: `yinstall_clean_apply`
**用途**：执行 `yinstall clean`  
**风险**：R3（破坏性）  
**门禁**：
- 必须二次确认 + `approval_token`
- 默认不允许 `clean_yac_disks=auto`（容易扩大影响面），需要更高审批或明确磁盘列表

#### Clean 关键参数（来自 `internal/cli/clean.go`）
- `type`(db/ycm/ymp, default db) → `--type`
- `yasdb_home/yasdb_data/yasdb_log/cluster_name/os_user`（db 清理）→ `--yasdb-home/--yasdb-data/--yasdb-log/--cluster-name/--os-user`
- `yac_mode`(bool) → `--yac`（单节点 YAC 清理建议显式传入；与 `db` 一致）
- `clean_yac_disks`(auto 或显式列表) → `--clean-yac-disks`（`auto` 时 `ycsctl` 失败则回退 `/dev/yfs` 发现，支持单节点）
- DB 清理默认执行 `CLEAN-DB-001`～`CLEAN-DB-006`；`-s CLEAN-DB` 仍可单步跑聚合步骤 `CLEAN-DB`
- `ycm_home` → `--ycm-home`
- `ymp_home/ymp_user` → `--ymp-home/--ymp-user`

## YAC `--yac-access-mode`

| 值 | VIP（C-009） | SCAN（C-011/C-013） | `ce gen` |
|----|-------------|---------------------|----------|
| `vip`（默认） | 校验或自动生成 | 跳过 | 含 `--vips` |
| `scan` | 同上 | 执行 | 含 `--scanname`（及 VIP 前置逻辑） |
| `direct` | **跳过** | 跳过 | **不含** `--vips`（单节点/直连场景） |

## 终端步骤进度（`N of M`）

安装类子命令（`os` / `db`，含 `--archive` 挂钩 collect）共用 `runner.StepProgress`：

- **分母 M**：计划中会执行且计入进度的步骤数 = 非 `Optional` 的安装步 +（挂钩时）非 `Optional` 的 collect 步；挂钩 collect **不计** `R-001`（复用安装阶段 SSH）。
- **分子 N**：从 1 递增；仅在实际开始某步时占用一个序号（多节点同一步只递增一次）。
- **Optional**：`PreCheck` 判定跳过 → 终端 `skipped (not in progress total)`，不占 N/M；若实际执行则 **M 动态 +1** 并占一个序号。
- **独立 `collect` / `stressos`**：各自 `NewStepProgress`；未接安装进度时行为与上相同。

## 安全门禁规则（必须落实）
- **敏感字段脱敏**：默认日志为明文（便于排障）；生产/共享日志请加 `--log-redact` 对 password/`-p` 等脱敏
- **命令构造白名单**：只允许 `yinstall` + 固定子命令 + 已声明 flag
- **步进限制**：危险 step/tag（例如 `dangerous`）在 apply 前必须显式确认

