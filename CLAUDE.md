# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**yinstaller** is a Go-based CLI tool for automating YashanDB installation across multiple target hosts via SSH. It orchestrates complex multi-step installation workflows for OS baseline preparation, database installation (single/YAC cluster), standby database setup, and YCM/YMP deployment.

The main binary is named `yinstall` (previously `yasinstall`).

## Build & Development Commands

### Build
- **Current platform**: `make build-current` or `./build.sh --current`
- **All platforms**: `make build-all` or `./build.sh --all`
- **Specific platform**: `make build-linux`, `make build-windows`, `make build-darwin`
- **Clean build**: `make clean` or `./build.sh --clean`

Output binaries go to `build/` directory with naming convention: `yinstall_<os>_<arch>[.exe]`

The binary is named `yinstall` (changed from `yasinstall` in v0.1.0+). All references in code, documentation, and build scripts have been updated accordingly.

### Run Tests
- **All tests**: `go test ./...`
- **Single test file**: `go test ./internal/cli -run TestParseStepRanges`
- **Verbose output**: `go test -v ./...`

### Lint & Format
- **Format code**: `go fmt ./...`
- **Vet code**: `go vet ./...`

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
- **Subcommands**: `os.go`, `db.go`, `ycm.go`, `ymp.go`, `standby.go`, `clean.go`
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
- Logs are named: `yinstall_<timestamp>_<runID>.log` and `yinstall_<timestamp>_<runID>_debug.log`

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

The codebase includes unit tests for step filtering logic:
- `internal/cli/steps_util_test.go` tests `parseStepRanges()` function
- Tests cover single steps, ranges, comma-separated lists, and mixed formats
- Run with: `go test ./internal/cli -run TestParseStepRanges -v`

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
