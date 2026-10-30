# Changelog

本文件记录 yinstall 的用户可见变更。日常开发请写入 `[Unreleased]`；发版时由 `./build.sh --release` 自动归入对应 CalVer 章节，并写入 git tag / GitLab Release 说明。

## [Unreleased]

### Added

- CalVer 版本号 `YY.MM.PATCH`（发版自动递增，日常构建为 `…+dev`）
- `scripts/creds.sh`：GitLab API token 存 macOS 钥匙串，供 `build.sh --release` 使用
- 发版流程与 CHANGELOG 联动（本版说明进 tag / Release，历史留在本文件）

### Changed

- 版本号由时间戳改为 CalVer；发版入口统一为 `./build.sh --release`（GitLab / git-tools）
- 发版产物展示将 `darwin` 改为 `MacOS`（如 `yinstall_MacOS_arm64`；编译仍用 `GOOS=darwin`）
- sysctl 统一写入 `/etc/sysctl.conf` 托管块；移除 `--os-sysctl-mode` 与 drop-in 双写

### Fixed

