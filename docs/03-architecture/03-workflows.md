# 工作流（Skills / Workflows）

## 目标
把“一键安装”从一次性命令执行，提升为 **可解释、可暂停、可恢复、可审计** 的状态机。

## 总体约束（所有工作流通用）
- **默认 precheck-first**：任何 apply 前必须完成一次 `--precheck` 或 `--dry-run`
- **确认与审批**：
  - 交互模式：用户显式确认
  - 非交互模式：必须提供 `approval_token`
- **失败默认不自动清理**：除非用户明确选择“清理现场”，并通过更高门禁
- **日志与审计**：必须输出 `run-id` 与日志路径，并落盘结构化审计 JSON

## Workflow 1：一键安装 YashanDB（单机/集群 DB）
### 输入
- targets / ssh 凭据
- 软件包路径（db package、可选 deps）
- 安装参数（路径、端口、密码等）

### 状态机
1. **Collect**（R0）
   - 调用 `yinstall_list_steps(db)`（可选）
   - 调用 `yinstall_db_precheck`（必须）
2. **Plan**（R0）
   - 输出将执行的阶段/steps（可解释）
   - 输出风险清单（是否涉及高危 flags / step tags）
   - 输出回滚/清理建议（例如失败后是否允许 `clean`）
3. **Confirm**（门禁）
   - 交互模式：用户输入 “确认执行”
   - 非交互：必须提供 `approval_token`
4. **Apply**（R2/R3）
   - 调用 `yinstall_db_apply`
   - 监控执行时长与中断（超时/网络断开）
5. **Verify**（R0/R1）
   - 汇总 `yinstall` 输出与日志路径
   - 可选：调用只读检测（端口、进程、简单连通）
6. **Report**（R0）
   - 产出结构化报告（成功/失败、耗时、日志、关键参数）
   - 落盘审计记录（JSON）

### 失败处理（Recovery）
- precheck 失败：阻塞 apply，输出缺失项与修复建议
- apply 失败：
  - 默认不自动清理（避免误删）
  - 提供两条路径：
    - Path A：保留现场，导出日志，进入“人工排查”
    - Path B：执行受控 `clean`（需要二次确认 + 更高审批）

## Workflow 2：创建备库（Standby）
### 关键差异
- 同一条命令会分别在 **主库** 与 **备库节点** 上执行不同步骤（见 `internal/cli/standby.go` 的多阶段流程）
- 默认 `skip_os=true`：备库侧 OS baseline 默认跳过（但仍会做连通性检查 B-001）

### 状态机
1. **Collect**（R0）
   - `yinstall_standby_list_steps`（可选）
   - `yinstall_standby_precheck`（必须）
2. **Plan**（R0）
   - 主库侧将执行的阶段：状态检查 → 归档/网络校验 → 扩容配置生成 → 安装软件 → 添加备库实例 → 同步校验
   - 备库侧将执行的阶段：连通性 →（可选 OS baseline）→ 环境变量/自启配置
3. **Confirm**（门禁）
4. **Apply**（R2/R3）
   - `yinstall_standby_apply`
5. **Verify**（R0）
   - 主库侧：集群状态/同步状态（对应 E-014/E-019 等）
6. **Report**（R0）

### 失败处理
- 若扩容中断：默认保留现场，提示可选的“失败清理步骤”（如 E-018 / clean）
- `standby_cleanup_on_failure=true` 视为高危：仅在更高审批下允许

## Workflow 3：安装 YMP
### 状态机
1. Collect：`yinstall_ymp_list_steps`（可选）+ `yinstall_ymp_precheck`（必须）
2. Plan：确认 OS 准备策略（skip-os / yum-mode / JDK 安装与版本）
3. Confirm
4. Apply：`yinstall_ymp_apply`
5. Verify：端口/进程与访问 URL（8090 默认）
6. Report

### 失败处理
- 默认不执行 `--ymp-cleanup`
- 若开启 `ymp_cleanup=true`：视为高危，必须二次确认

## Workflow 4：安装 YCM
### 状态机
1. Collect：`yinstall_ycm_list_steps`（可选）+ `yinstall_ycm_precheck`（必须）
2. Plan：确认后端数据库模式（sqlite3 / yashandb）与端口占用风险
3. Confirm
4. Apply：`yinstall_ycm_apply`
5. Verify：端口/进程与访问 URL（9060 默认）
6. Report

### 失败处理
- 若使用 `yashandb` 作为后端，失败优先保留现场并输出 deploy.yml 与日志定位

## Workflow 5：清理（Clean）
### 风险提示
清理属于破坏性操作（R3），默认应由人工确认，并且在 Plan 阶段必须显示：
- 将停止哪些进程
- 将删除哪些目录
- （若为 YAC）将清理哪些共享盘路径

### 状态机
1. **Collect**（R0）
   - `yinstall_clean_list_steps`（可选）
2. **Plan**（R0/R1）
   - 影响面评估：targets、目录、cluster-name、yac disk 列表
3. **Confirm（双确认）**
   - 交互：要求输入二次确认短语（例如 “DELETE-ENV”）
   - 非交互：更高等级 `approval_token`
4. **Apply**（R3）
   - `yinstall_clean_apply`
5. **Verify**（R0）
   - 目录不存在/进程停止（只读）
6. **Report**（R0）

