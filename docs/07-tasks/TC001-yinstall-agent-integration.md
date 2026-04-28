# Task Card：TC001 - yinstall 集成到 Agent（设计阶段）

## 目标
把现有 `yinstall`（一键安装引擎）集成到一个新的“运维安装 Agent CLI”中，以工具契约方式调用，支持 YashanDB 单机一键安装（MVP）。

## 范围（MVP）
- 支持命令（均通过工具契约调用 `yinstall`）：
  - `install db`（单机/YAC）
  - `install standby`（创建备库）
  - `install ymp`
  - `install ycm`
  - `clean`（删除环境，高危，默认双确认）
- 强制流程：precheck → plan → confirm → apply → report
- 审计：落盘 JSON（工具调用序列、脱敏后的命令、日志路径、结果摘要）

## 不在范围
- 自动化凭据托管/轮转
- 自动故障自愈
- 全量覆盖所有 `yinstall` 子命令参数（先做最小集，优先覆盖关键参数与门禁）

## 验收标准
- [ ] 对每个功能（db/standby/ymp/ycm/clean）都能：
  - 先执行一次 `--precheck` 或 `--dry-run`
  - 输出 plan（将执行的 steps/阶段、风险点、关键参数）
  - apply 前必须经过确认（交互）或审批 token（非交互）
  - 产出结构化 report
- [ ] 执行 `yinstall <subcmd> ...` 后，能输出：
  - session/debug 日志路径
  - 关键安装参数回显（脱敏）
  - 成功/失败摘要与建议动作
- [ ] 默认禁止高危参数：
  - `--force-delete-user`
  - `clean_yac_disks=auto`（除非更高审批或明确磁盘列表）
  - `--ymp-cleanup`（除非二次确认）
  - `--standby-cleanup-on-failure`（除非二次确认 + 更高审批）

## 设计输出（本次交付）
- [x] `PROJECT.md`
- [x] `AGENTS.md`
- [x] `docs/00-index.md`
- [x] `docs/01-background/README.md`
- [x] `docs/03-architecture/01-overview.md`
- [x] `docs/03-architecture/02-tool-contracts.md`
- [x] `docs/03-architecture/03-workflows.md`

## 下一步（进入实现阶段前需要补齐的信息）
- 从 `internal/cli/{db,standby,ymp,ycm,clean}.go` 抽取各子命令参数列表，固化为 tool schema（JSON）
- 明确 `yinstall` 日志文件命名与定位规则（便于工具返回精确路径）
- 定义审批 token 的来源与校验方式（本地交互/对接工单系统）

