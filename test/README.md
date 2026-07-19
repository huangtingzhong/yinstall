# yinstall 测试目录

**通用** test/ 约定 → `~/.cursor/rules/global-agent-workflow.mdc` § 测试与验证

**yinstall 增量** → `.cursor/rules/testing.mdc`

## 命令

```bash
make check-test-layout
go test ./test/go/... -count=1
go test ./... -count=1
./test/run_all.sh
```

## 布局

| 路径 | 用途 |
|------|------|
| `test/go/<域>/` | Go 单测 |
| `test/go/legacy-allowlist.txt` | internal 历史测试白名单 |
| `test/cases/` | 真机冒烟 |
| `test/scratch/` | 一次性，验完删 |

示例：`test/go/ycm/layout_test.go`
