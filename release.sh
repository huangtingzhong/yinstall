#!/usr/bin/env bash
# 兼容入口：发版已与 yops 对齐，统一走 ./build.sh --release（CalVer + CHANGELOG + GitLab）。
# 用法：
#   ./release.sh
#   ./release.sh --tag v26.10.0
#   ./release.sh --allow-dirty
#   ./release.sh --allow-empty-changelog

set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
exec bash ./build.sh --release "$@"
