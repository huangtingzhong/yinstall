# docs/00-index.md

## 文档状态
- 📝Clarifying / 🎨Designing / ✅Design Complete / 🔨Developing / 🧪Testing / 🏁Complete
- 当前状态：🎨Designing

## 交付范围（MVP）
- **主目标**：Agent 通过工具契约调用 `yinstall`，覆盖以下运维安装能力：
  - `db`：安装数据库（单机/YAC）
  - `standby`：创建/扩容备库
  - `ymp`：安装 YMP
  - `ycm`：安装 YCM
  - `clean`：清理/删除环境（高危）
- **统一要求**：所有 apply 前必须完成一次 `--precheck` 或 `--dry-run`，并具备确认/审批门禁与审计输出。

## 文档索引
- `docs/01-background/README.md`：背景、场景、边界、术语
- `docs/03-architecture/01-overview.md`：总体架构
- `docs/03-architecture/02-tool-contracts.md`：工具契约（schema、安全门禁）
- `docs/03-architecture/03-workflows.md`：工作流/技能状态机
- `docs/07-tasks/TC001-yinstall-agent-integration.md`：MVP 任务卡与验收

