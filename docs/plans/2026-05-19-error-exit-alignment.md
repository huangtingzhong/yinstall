# Error Exit 日志对齐实施计划

> **For agentic workers:** 按 Task 顺序实施；每完成一 Task 运行对应验证命令并勾选 checkbox。

**Goal:** 统一命令失败时终端/session 输出 `LogErrorExit` 完整块，避免仅一行 `Error: action failed`；同时避免重试/可恢复失败误报、避免密码泄露。

**Architecture:** 在 `logging` 层对 `LogErrorExit` 脱敏；在 `runner` 引入 `CommandExitError`（`Logged` 标记）替代子串判断；`common/os` 提供 `*Check`（打日志）与 `*CheckQuiet`（不打终端块）；yasql 在最终失败时显式打块；DB 安装步改用 `ExecuteAsUserWithCheck`。

**Tech Stack:** Go 1.x, `github.com/yinstall/internal/{logging,runner,common/os,common/sql,steps/...}`

---

## Task 1: LogErrorExit 脱敏

**Files:**
- Modify: `internal/logging/logger.go`
- Create: `internal/logging/redact_test.go`

- [x] `LogErrorExit` 对 command/stdout/stderr/errMsg 调用 `redact()`
- [x] 增加 `-p 'secret'` / ` -p ` 脱敏规则
- [x] 单元测试覆盖

**验证:** `go test ./internal/logging/... -v -run Redact`

---

## Task 2: CommandExitError + RunStep

**Files:**
- Create: `internal/runner/exec_error.go`
- Modify: `internal/runner/step.go`
- Create: `internal/runner/exec_error_test.go`

- [x] 定义 `CommandExitError` + `CommandExitLogged(err)`
- [x] `ExecuteWithCheck` 返回 `*CommandExitError`（`Logged: true`），errMsg 与 LogErrorExit 一致
- [x] `RunStep` Action 失败：仅当 `!CommandExitLogged(err)` 时补打简版 LogErrorExit

**验证:** `go test ./internal/runner/... -v -run CommandExit`

---

## Task 3: Quiet 执行 API

**Files:**
- Modify: `internal/common/os/exec.go`
- Modify: `internal/steps/standby/e011_gen_expansion_config.go`
- Modify: `internal/steps/standby/yasboot_run_as_user.go`

- [x] `reportCommandFailure(..., logToTerminal bool)`
- [x] `ExecuteAsUserWithEnvCheckQuiet` / `runYasbootOnPrimaryWithEnvFileQuiet`
- [x] E-011 首次 gen 与中间 status 用 Quiet；最终失败 `LogTerminalCommandFailure` 兜底

**验证:** `go test ./internal/steps/standby/... ./internal/common/os/...`

---

## Task 4: DB / yasql 对齐

**Files:**
- Modify: `internal/steps/db/c020_install_software.go`
- Modify: `internal/steps/db/c021_deploy_database.go`
- Modify: `internal/steps/db/c029_show_cluster_status.go`
- Modify: `internal/common/os/exec.go`（`reportCommandFailure` 返回 CommandExitError）
- Modify: `internal/common/sql/yasql.go`

- [x] C-020/C-021 改用 `ExecuteAsUserWithCheck`，删除无效 `Logger.Error`
- [x] C-029 改用 `ExecuteAsUserWithEnvCheck`
- [x] yasql：`ReportSQLFailure`；C-023 仅在非 already-exists 时打块；C-026 connectivity 打块

**验证:** `go test ./internal/steps/db/... ./internal/common/sql/...`

---

## Task 5: 全量回归

- [x] `go test ./...`
- [x] `go vet ./...`

---

## 明确不做（本计划范围外）

- C-026 status 软失败改硬失败（需产品决策）
- PostCheck 附带 LastExec（需 StepContext 设计）
- E-013 多轮重试仅保留最终一块（可选后续）
