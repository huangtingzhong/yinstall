# Changelog

本文件记录 yinstall 的用户可见变更。日常开发请写入 `[Unreleased]`；发版时由 `./build.sh --release` 自动归入对应 CalVer 章节，并写入 git tag / GitLab Release 说明。

## [Unreleased]

### Added

### Changed

- local yum repo 判定由「文件存在即跳过」改为与 HTTP 相同的内容比较（错误内容会纠正）

### Fixed

- 包依赖/工具安装：按逻辑名 Resolve（如 EL8/openEuler/麒麟上 `dstat`→`pcp-system-tools`），满足判定认 RPM Provides，避免假 missing 误触发 ISO 上传
- HTTP/local yum repo 按内容幂等写入：同内容不再重复 `printf` 覆盖；内容变化或缺文件时才写
- `standby --precheck`：缺尚未生成的 `*_add.toml` 时不再在 Add Standby Instance 硬失败（与 Generate Expansion Config 文案对齐 skip 匹配）

## [26.10.0] - 2026-10-30

### Added

- CalVer 版本号 `YY.MM.PATCH`（发版自动递增，日常构建为 `…+dev`）
- `scripts/creds.sh`：GitLab API token 存 macOS 钥匙串，供 `build.sh --release` 使用
- 发版流程与 CHANGELOG 联动（本版说明进 tag / Release，历史留在本文件）

### Changed

- 版本号由时间戳改为 CalVer；发版入口统一为 `./build.sh --release`（GitLab / git-tools）
- 发版产物展示将 `darwin` 改为 `MacOS`（如 `yinstall_MacOS_arm64`；编译仍用 `GOOS=darwin`）
- sysctl 统一写入 `/etc/sysctl.conf` 托管块；移除 `--os-sysctl-mode` 与 drop-in 双写

### Fixed

