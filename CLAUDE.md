# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Maintaining These Rules (Required)

Development rules exist in **two copies**—keep them identical:

| File | Audience |
|------|----------|
| **`CLAUDE.md`** § Development Workflow (Mandatory) | Claude Code; committed to repo |
| **`.cursor/rules/yinstall-development.mdc`** | Cursor **project** rules (`alwaysApply` when this repo is open) |

**Project-only, not global:** These rules live in **this repository** under `.cursor/rules/` (tracked by git). **Do not** copy them to `~/.cursor/rules` or Cursor user-global Rules—that causes drift and cross-project conflicts.

**Any change to workflow, testing, file layout, test hosts, etc. must update BOTH files.** Updating only one copy is incomplete.

See also `.cursor/rules/README.md`.

---

## Project Overview

**yinstaller** is a Go-based CLI tool for automating YashanDB installation across multiple target hosts via SSH. It orchestrates complex multi-step installation workflows for OS baseline preparation, database installation (single/YAC cluster), standby database setup, and YCM/YMP deployment.

The main binary is named `yinstall` (previously `yasinstall`).

## Development Workflow (Mandatory)

### BUG fixes / new features

1. **Plan first, then code**: root cause, scope, affected steps, risks, verification. If the user message already contains an explicit implementation directive (e.g. “implement B1+B2”, “merge into logger.go”), treat that as approval and skip a separate plan step.
2. **Verify before claiming done**:
   ```bash
   go fmt ./...
   go vet ./...
   go test ./... -count=1          # existing tests under internal/, etc.
   go test ./tmp/... -count=1      # ad-hoc tests in tmp/ (omit if none)
   ```
3. **Testing policy**:
   - Write tests for pure logic / parsing / step mocks when useful; skip trivial assertions.
   - **Ad-hoc verification**: put test files only under repo root **`tmp/`** (e.g. `tmp/c031_profile_test.go`), `package xxx_test` + `import "github.com/yinstall/..."`. **Do not** add temporary `*_test.go` under `internal/` or `cmd/`.
   - `tmp/` is **gitignored**; after `go test ./tmp/...` passes, **delete those test files** (keep `tmp/.gitkeep`).
   - **`internal/*_test.go`**: add or keep only when the user explicitly wants long-term regression tests—not for one-off verification.
4. **E2E**: validate with `build/yinstall_*` using `-s` single-step or minimal path on real hosts (see Test environments below).
5. **Git**: do not commit, open PRs, or commit `build/` binaries unless the user asks.

### Scope & file organization

- **Minimal diff**; no drive-by edits.
- **DRY**: one implementation; extract to `internal/common/*` or an existing domain util only when a **second** call site exists.

**When to create new `.go` files**

| Allowed | Forbidden |
|---------|-----------|
| **New step**: `c0xx_*.go` / `b0xx_*.go` + `registry.go` | One-off bugfix/small feature files like `*_helpers.go`, `*_extra.go` |
| **Shared by 2+ steps or packages** → `internal/common/*` or domain `*_util.go` | ≤30-line helpers used in one file → **inline** |
| **Refactor split** (see file size) | One-off fragments left after task → **merge back** into domain file |

When extracting for DRY/debug, prefer **existing** files (`yasboot_cmd.go`, `logger.go`, `*_util.go`, `debug_log.go`) before creating new ones.

**File size**

| Lines | Strategy |
|-------|----------|
| **<500** | Edit in place |
| **500–800** | OK to edit; if adding **>50** lines, consider same-domain util |
| **≥800** | Prefer small patches; large additions → **refactor/split** first (not “small feature new file”) |
| **~1000** | Do not keep stacking; split then edit |

### Checklist (new work)

- [ ] Plan approved or user gave explicit implement directive
- [ ] Step IDs / registry / CLI Params correct
- [ ] No new `.go` for small one-off changes (except new steps, 2+ reuse, refactor split)
- [ ] Reused common/runner/domain utils; reasonable file size
- [ ] Upload, SQL, debug phases per conventions below
- [ ] `go fmt` / `go vet` / `go test ./...` (and `./tmp/...` if used) passed; ad-hoc tests removed from `tmp/`
- [ ] Real host smoke test: single-node **130**; YAC changes also **125/126** when applicable

### Coding constraints (summary)

| Scenario | Use |
|----------|-----|
| Remote commands | `ctx.Execute` / `ctx.ExecuteWithCheck` |
| Product user | `commonos.ExecuteAsUserWithCheck` / `ExecuteAsUserWithEnvCheckCtx` |
| SQL sysdba | `commonsql.ExecuteSQLAsSysdbaCtx` |
| Upload | `ctx.Executor.Upload(..., ctx.UploadContext())`; packages `file.FindAndDistribute` |
| Safe delete | `commonos.ValidateDeletePath` + `DeletePathUnder` |
| Multi-line shell/SQL | collect/stressos `*RunShell`/`*RunSQL`; install domain Execute + `LogScriptPreview` |

**Forbidden in steps**: hand-written scp, duplicated yasql wiring, raw `Executor.Execute` (except framework), `fmt.Print` instead of Logger.

**Debug logging (changed steps)**: commands via Execute* (`>>> cmd`, exit, stdout/stderr); `LogScriptPreview` before multi-line scripts/SQL; multi-subtask steps use `phase=plan` + `*-start`/`*-done`; terminal shows step summary only—not full stdout or phase bodies; redact secrets via `logging.redact`.

**Optional steps**: PreCheck error + `Optional: true`; missing upstream in dry-run/precheck → `runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing`.

### Test environments

Control machine macOS/Linux → SSH targets; users `root` / `yashan` (key-based); product user default `--os-user yashan`.

| Scenario | Hosts | Notes |
|----------|-------|-------|
| Single-node | `10.10.10.130` (aarch64) | `db`/`os`/`ycm`/`ymp`/`collect`/`stressos` `-t 10.10.10.130` |
| YAC multi-node | `10.10.10.125`, `10.10.10.126` | Multi `-t`; `stressos --net` ping mesh + iperf3 |
| YAC single-node | e.g. `10.10.10.125` + `--yac` | `--yac-access-mode direct`, `--yac-exclude-disks`, etc. |

```bash
make build-current
./build/yinstall_$(uname -s | tr A-Z a-z)_$(uname -m) db -t 10.10.10.130 --precheck -s C-007
```

Logs: `--log-dir` default `~/.yinstall/logs`; files `yinstall_<type>_<ts>.log` / `yinstall_<type>_debug_<ts>.log`. collect/stressos output: `./output/<collect|stress>/<timestamp>/`.

Detailed API tables, Params keys, and phase naming: **`installer.md`** (appendix debug troubleshooting).

## Build & Development Commands

### Build
- **Current platform**: `make build-current` or `./build.sh --current`
- **All platforms**: `make build-all` or `./build.sh --all`
- **Specific platform**: `make build-linux`, `make build-windows`, `make build-darwin`
- **Clean build**: `make clean` or `./build.sh --clean`

Output binaries go to `build/` directory with naming convention: `yinstall_<os>_<arch>[.exe]`

The binary is named `yinstall` (changed from `yasinstall` in v0.1.0+). All references in code, documentation, and build scripts have been updated accordingly.

### Run Tests
- **Regression**: `go test ./... -count=1`
- **Ad-hoc (tmp/)**: `go test ./tmp/... -count=1` — tests live only under repo root `tmp/`; delete after pass (see Development Workflow)
- **Single package**: `go test ./internal/cli -run TestParseStepRanges -v`
- **Verbose**: `go test -v ./...`

### Lint & Format
- **Format code**: `go fmt ./...`
- **Vet code**: `go vet ./...`
- **Debug logging static check**: `make check-debug-logging`

## Architecture & Key Concepts

### Step-Based Execution Model
The entire installation process is decomposed into discrete **steps**, each with:
- **ID**: Unique identifier (e.g., `B-001`, `C-015`, `G-002`)
- **PreCheck**: Validation before execution (optional)
- **Action**: Main execution logic
- **PostCheck**: Verification after execution (optional)

Steps can be:
- **Optional**: Skipped if precheck fails
- **Dangerous**: Destructive operations (e.g., disk formatting)
- **Tagged**: Grouped by category (e.g., `os`, `db`, `yac`, `ycm`, `ymp`)

### Step Execution Flow
1. **PreCheck** → validates prerequisites; if optional step fails here, it's skipped
2. **DryRun/Precheck modes** → skip Action and PostCheck
3. **Action** → performs the actual work
4. **PostCheck** → verifies the result
5. **Logging** → all steps log to session and debug logs

### Step Context (`runner.StepContext`)
Passed to every step's PreCheck/Action/PostCheck functions. Contains:
- `Executor`: SSH/local command executor
- `Logger`: Logging interface
- `Params`: Step-specific parameters from CLI flags
- `Results`: Map for storing step outputs (used by downstream steps)
- `OSInfo`: Detected OS information (populated by B-000)
- `TargetHosts`: For multi-node scenarios (YAC); steps iterate over hosts as needed

### Multi-Host Execution (YAC)
- When multiple targets are specified, `TargetHosts` is populated
- Steps can use `ctx.HostsToRun()` to get the list of hosts to execute on
- Use `ctx.ForHost(targetHost)` to create a sub-context for a specific host
- Single-host steps automatically work with the single executor

### Step Registries
Each installation type has a registry function that returns ordered steps:
- `internal/steps/os/registry.go` → OS baseline steps (B-000 to B-029)
- `internal/steps/db/registry.go` → Database steps (C-000 to C-021)
- `internal/steps/ycm/registry.go` → YCM steps (G-001 to G-010)
- `internal/steps/standby/` → Standby database steps (E-000 to E-009)
- `internal/steps/clean/` → Cleanup steps

### CLI Structure
- **Root command**: `internal/cli/root.go` defines global flags (SSH, execution control, logging)
- **Subcommands**: `os.go`, `db.go`, `ycm.go`, `ymp.go`, `standby.go`, `clean.go`, `collect.go`, `stressos.go`
- Each subcommand:
  - Defines its own flags
  - Builds a step list from the registry
  - Filters steps based on `--include-steps` (`-s`), `--exclude-steps` (`-e`)
  - Executes steps sequentially with error handling

### SSH & Local Execution
- **SSH Executor** (`internal/ssh/executor.go`): Handles remote command execution, file upload/download
- **Local Executor**: Used when `--local` flag is set
- Both implement the `Executor` interface
- Supports password and key-based authentication
- Handles sudo elevation for privileged operations
- **Authentication Fallback** (`NewExecutorWithFallback`): When no password is provided, automatically tries:
  1. SSH key-based authentication (from `~/.ssh/id_rsa` or `--ssh-key-path`)
  2. Default password (if provided)
  3. Returns detailed error message if all methods fail, guiding user to provide credentials

### Logging
- **Session log**: Mirrors terminal output (human-readable)
- **Debug log**: Detailed logs including all commands, stdout, stderr, exit codes
- Both logs are created in `--log-dir` (default: `~/.yinstall/logs`)
- Logs are named: `yinstall_<type>_<timestamp>.log` and `yinstall_<type>_debug_<timestamp>.log` (e.g. `yinstall_db_20260528222915.log`)

## Common Development Tasks

### Adding a New Step
1. Create a new file in the appropriate `internal/steps/<type>/` directory (e.g., `b030_new_step.go`)
2. Implement a function returning `*runner.Step` with PreCheck/Action/PostCheck
3. Add the step to the registry function in that directory
4. Use `ctx.ExecuteWithCheck()` for commands that must succeed, or `ctx.Execute()` for optional commands
5. Store results in `ctx.SetResult()` for downstream steps to access

### Adding a New CLI Flag
1. Define the flag variable in the subcommand file (e.g., `internal/cli/os.go`)
2. Register it in the `init()` function using `cmd.Flags().StringVar()`, etc.
3. Access it via `GetGlobalFlags()` or directly from the variable
4. Pass it to steps via `ctx.Params` or `ctx.GetParam*()`

### Filtering Steps
- `-s` / `--include-steps B-001,B-002` or `B-001-B-005` (range syntax)
- `-e` / `--exclude-steps B-010-B-015`
- `-F` / `--force` (all steps) or `-f` / `--force-steps B-001,B-002` (force re-execute, deletes existing resources)

### Debugging
- Use `--dry-run` to see what would execute without making changes
- Use `--precheck` to only run PreCheck phases
- Use `--log-dir /tmp/debug` to write logs to a specific location
- Check debug log for full command output and exit codes
- Use `--include-steps` to isolate specific steps

## Key Files & Patterns

| File | Purpose |
|------|---------|
| `cmd/yinstall/main.go` | Entry point |
| `internal/cli/root.go` | Global flags and subcommand registration |
| `internal/cli/{os,db,ycm,ymp,standby}.go` | Subcommand implementations |
| `internal/runner/step.go` | Step definition, execution, and context |
| `internal/ssh/executor.go` | SSH/local command execution |
| `internal/logging/logger.go` | Logging infrastructure |
| `internal/steps/{os,db,ycm,standby,clean}/` | Step implementations |
| `internal/common/os/` | OS detection, package management, user/group operations |
| `internal/common/file/` | File operations |
| `internal/common/sql/` | SQL execution via yasql |

## Important Patterns

### Error Handling in Steps
- Return error from PreCheck/Action/PostCheck to fail the step
- Use `ctx.ExecuteWithCheck()` for commands that must succeed (auto-logs errors)
- Use `ctx.Execute()` for optional commands, check exit code manually
- Errors are logged and execution stops unless step is optional

### Parameter Passing
- CLI flags → `GetGlobalFlags()` or direct variable access
- Subcommand-specific flags → stored in module-level variables
- Step parameters → passed via `ctx.Params` map
- Step outputs → stored in `ctx.Results` map for downstream steps

### OS Detection
- B-000 step detects OS and populates `ctx.OSInfo`
- Downstream steps check `ctx.OSInfo.IsRHEL7`, `ctx.OSInfo.IsRHEL8`, `ctx.OSInfo.IsKylin`, etc.
- Package manager is auto-detected: `yum`, `dnf`, or `apt`

### Multi-Node Coordination
- For YAC deployments, steps may need to run on all nodes or specific nodes
- Use `ctx.HostsToRun()` to get the list of hosts
- Loop over hosts and use `ctx.ForHost(host)` to create a sub-context
- Some steps (like C-000 connectivity check) run as global precheck before per-host execution

## Testing

- **Long-term tests** live next to code under `internal/` (e.g. `internal/cli/steps_util_test.go` for `parseStepRanges()`).
- **Ad-hoc verification** during development: **`tmp/*_test.go` only** — never add temporary tests under `internal/`; see **Development Workflow (Mandatory)**.
- Example ad-hoc test location: `tmp/feature_test.go` with `package tmp_test` importing `github.com/yinstall/...`.

## Version & Build Info

Version information is auto-generated during build:
- `VERSION`: Timestamp in format `YYYYmmdd_HHMMSS`
- `BUILD_TIME`: Human-readable build time
- `GIT_COMMIT`: Short git commit hash
- Stored in `cmd/yinstall/version.go` (auto-generated by build script)

## Recent Changes

### v0.1.0+ - Binary Rename
- Binary name changed from `yasinstall` to `yinstall`
- Module path changed from `github.com/yasinstall` to `github.com/yinstall`
- Log directory changed from `~/.yasinstall/logs` to `~/.yinstall/logs`
- All documentation and build scripts updated accordingly
- SSH authentication fallback mechanism added (see SSH & Local Execution section)
