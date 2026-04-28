# 总体架构

## 目标
把现有 `yinstall` 的“一键安装能力”以 **工具契约（Tool Contracts）** 的方式提供给 AI Agent，形成一个可控、可审计、可回放的运维安装 Agent CLI。

## 组件划分

### 1) Agent CLI（新）
对外入口，负责：
- 解析用户请求（自然语言/参数）
- 生成“执行计划”（Plan）
- 触发工具调用（Tools）
- 人工确认/审批门禁
- 生成结构化审计记录

### 2) Tool Adapter（新）
将 `yinstall` 子命令封装成稳定工具接口：
- **输入**：强类型参数（JSON schema）
- **执行**：构造 `yinstall ...` 命令并运行（禁止自由拼接）
- **输出**：结构化结果（exit code、日志路径、摘要、关键步骤状态）
- **安全**：按风险等级校验参数（例如禁用 `--force-delete-user`，或需审批 token）

### 3) `yinstall`（既有）
核心安装引擎（Go CLI），特性：
- 子命令：`os` / `db` / `standby` / `ycm` / `ymp` / `clean`
- step 模型：PreCheck / Action / PostCheck
- 执行控制：`--dry-run`、`--precheck`、include/exclude steps、tags、force
- 日志：session + debug（默认 `logs/`）

## 数据流（MVP：一键安装 DB）
1. 用户：`agent install yashandb --targets ...`
2. Agent：先调用 `tool.yinstall_list_steps(db)`（R0）获取步骤目录（可选）
3. Agent：调用 `tool.yinstall_precheck_db(...)`（R0）确保连通与前置条件满足
4. Agent：输出 Plan（将执行哪些 step、风险点、耗时预估、回滚建议）
5. 用户确认/审批通过
6. Agent：调用 `tool.yinstall_apply_db(...)`（R2/R3）执行安装
7. Agent：解析日志路径，汇总结果并输出“可复盘记录”

## 关键设计决策
- **以 `yinstall` 为唯一执行体**：Agent 不直接 SSH 执行命令
- **默认 precheck-first**：任何 apply 前必须有一次 `--precheck` 或 `--dry-run`
- **高危参数门禁**：`--force` / `--force-steps` / `--force-delete-user` 归为高危

