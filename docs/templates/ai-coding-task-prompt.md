# yinstall AI 编码任务提示词模板

复制对应章节到 Cursor 对话，替换 `【】` 占位符。须遵守仓库 `.cursor/rules/yinstall-development.mdc`：**先方案、你同意后再改代码**。

---

## 共用验收块（可粘贴到任意模板末尾）

```text
## 验收与测试（必须执行，未完成不得声称「已完成」）

### 代码级（控制端，必须贴命令输出摘要）
1. go fmt ./...
2. go vet ./...
3. go test ./... -count=1
4. 临时验证测试写在仓库根 tmp/（如 tmp/feature_test.go），禁止在 internal/ 下新建临时 *_test.go；参考 internal/steps/db/db_test_helpers_test.go 的 mock 写法
5. go test ./tmp/... -count=1（有 tmp 测试时）
6. 全部通过后：【默认删除 tmp/ 内本次测试文件 / 保留并提交到 internal/（仅当用户明确要求长期回归）】

### 功能级（yinstall，按改动选最小集合）
- 编译：make build-current
- 探测：--dry-run 或 --precheck
- 单步：-s 【步骤ID，如 C-007、B-008、R-029】
- 目标机：【单机 10.10.10.130 / YAC 10.10.10.125,10.10.10.126 / 本机不传 -t】
- SSH：-u 【root】 -t 【IP】
- 示例命令：
  ./build/yinstall_$(uname -s | tr A-Z a-z)_$(uname -m) 【子命令】 -t 【IP】 --precheck -s 【步骤】

### 交付说明
- 改动文件列表 + 为何这样改
- 测试命令与结果（exit code / 关键日志路径：~/.yinstall/logs 或 output/）
- 风险与回滚方式
- 未覆盖项（若有）及原因
```

---

## 模板 A：功能新增

```text
【yinstall 功能新增】

## 背景与目标
- 需求：【一句话描述用户要什么】
- 涉及域：【os / db / standby / ycm / ymp / collect / stressos / clean】
- 步骤 ID（若已知）：【如 C-031；新建则说明插入位置】

## 约束
- 最小 diff，不顺手重构无关代码
- 复用现有能力：ctx.Execute*、Upload+UploadContext、commonsql、commonos、file.FindAndDistribute（见 yinstall-development 规范 DRY 表）
- debug：多行脚本须 LogScriptPreview；多子任务须 plan + op-start/done
- CLI 参数只在 internal/cli/【cmd】.go 定义，经 build*Params 写入 ctx.Params

## 请先输出（不要写代码）
1. 方案：改动文件、registry 顺序、Params 键名、影响步骤
2. 风险分级：R0–R3 及是否需 --force
3. 测试计划：单测测什么 + 130/125+126 上跑哪条 yinstall 命令

我回复「同意实现」后再写代码。

## 验收与测试
（粘贴上文「共用验收块」，并按测试计划填写【】）
```

---

## 模板 B：BUG 修复

```text
【yinstall BUG 修复】

## 现象
- 命令：【完整 yinstall 命令，含 flags】
- 失败步骤：【如 C-020】
- 期望 vs 实际：【各一行】
- 日志片段：【session/debug 路径或关键 stderr，可脱敏】

## 范围
- 怀疑模块：【internal/steps/db/... 或 cli/...】
- 是否回归：【相关步骤 ID】

## 请先输出（不要写代码）
1. 根因分析（含代码位置引用）
2. 修复方案与影响面
3. 复现/回归：先写失败用例或表驱动 case 的思路，再改实现

我回复「同意实现」后再改代码。

## 验收与测试
（粘贴「共用验收块」）
- 单测须覆盖：【复现条件 → 修复后通过】
- 集成：在【10.10.10.130】上 --precheck -s 【步骤】确认不再失败
```

---

## 模板 C：单步骤 / CLI 参数改动

```text
【yinstall 单点改动】

## 改动点
- 步骤：【B-008 / C-033 / S-007】或 CLI：【如 db.go --db-spfile-params】
- 行为变更：【改前 → 改后】

## 请先给方案（1 屏内），我同意后再实现。

## 验收（精简）
- go fmt && go vet && go test ./... -count=1
- 临时测试在 tmp/ 覆盖：【解析函数名 / PreCheck 分支】
- 集成（必选其一）：
  - --precheck -s 【步骤ID】 -t 【10.10.10.130】
  - 或 --dry-run -s 【步骤ID】
- 贴 debug 里 phase=plan 与一条 >>> cmd 证明可复盘

默认测试通过后删除 tmp/ 内本次测试文件。
```

---

## 模板 D：clean / 删路径 / 危险操作

```text
【yinstall 清理/危险改动 — 谨慎】

## 目标
- clean 子命令 / 步骤：【如 clean db / 某 clean 步骤】
- 删除范围：【路径、用户、是否 --force / --force-delete-user】

## 强制要求
1. 必须先方案：列出将执行的删除命令与 ValidateDeletePath 校验点
2. 禁止在未确认环境下直接 Action；优先 --dry-run / --precheck
3. 无 -f/-F 时不得删已有数据

## 验收
- 代码：go test（若有路径解析逻辑）
- 集成顺序：
  1) --dry-run -s 【步骤】
  2) --precheck
  3) 仅在【测试机 IP】且我明确说「可执行 apply」时才真删
- 说明回滚与误删防护

我回复「同意且可在【IP】执行 apply」前不要执行破坏性 Action。
```

---

## 模板 E：collect / stressos 归档类

```text
【yinstall collect/stressos】

## 目标
- 子命令：【collect / stressos】
- 步骤：【R-0xx / S-0xx】或规则 YAML 变更
- profile/flags：【--profile minimal / --net / --install-deps】

## 约束
- 多行 shell：stressRunShell / collectRunShell，禁止裸 heredoc 单行 Execute
- 归档：output/collect|stress/<timestamp>/；debug 用 collectLogPhase / stressLogPhase

## 验收
- go test（pack、规则解析等若有）
- 130 单机：-s 【R-001,R-002,R-029】或 stressos -s S-01,S-11
- YAC 网络：125,126 + --net 时注明 ping 网格 / iperf3 角色

（粘贴「共用验收块」中功能级示例）
```

---

## 短句追加（已有对话时贴在最后）

```text
按 yinstall-development 规范：先方案等我同意 → 实现 → tmp/ 临时测试 → go fmt/vet/test 贴输出 → 【删 tmp/ 测试 / 留 internal 回归】→ 130 上 --precheck -s 【ID】。未完成测试不要说 done。
```

---

## English quick block (optional)

```text
[yinstall task] Scope: 【domain/step IDs】. First: root cause + plan + test plan only — wait for my "go ahead". Then: minimal diff, DRY helpers, debug phases. Verify: go fmt/vet/test ./... -count=1, temp tests in tmp/ only 【delete tmp after pass / keep in internal if asked】, build + --precheck -s 【ID】 on 【10.10.10.130】. Paste command outputs; do not claim done without evidence.
```

---

## 占位符速查

| 占位符 | 示例 |
|--------|------|
| 子命令 | `db`, `os`, `clean`, `collect`, `stressos` |
| 单机测试机 | `10.10.10.130` |
| YAC 双节点 | `10.10.10.125,10.10.10.126` |
| 步骤过滤 | `-s C-007` 或 `-s B-001-B-005` |
| 日志目录 | `~/.yinstall/logs` 或 `--log-dir` |
