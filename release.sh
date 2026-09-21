#!/usr/bin/env bash
# 本机发版：build.sh（含 UPX / darwin arm64 gzip）→ gh release create 上传附件
# 用法：
#   ./release.sh
#   ./release.sh --tag 20260921_120000
#   ./release.sh --skip-build          # 复用已有 build/
#   ./release.sh --dry-run
#   ./release.sh --generate-notes      # 用 GitHub 自动生成说明（默认用固定模板）

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

BINARY_NAME="yinstall"
BUILD_DIR="build"
VERSION_FILE="cmd/yinstall/version.go"

TAG=""
SKIP_BUILD=false
DRY_RUN=false
GENERATE_NOTES=false
TARGET_BRANCH="main"

print_msg() {
	local color=$1
	shift
	local nc='\033[0m'
	echo -e "${color}$*${nc}"
}

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'

usage() {
	cat <<EOF
Usage: $0 [OPTIONS]

Local GitHub release using ./build.sh compression, then gh release create.

OPTIONS:
  -h, --help            Show this help
  -t, --tag TAG         Release tag (default: Version from version.go after build)
  --skip-build          Do not run build.sh; upload existing build/ artifacts
  --generate-notes      Use GitHub --generate-notes instead of the fixed template
  --target BRANCH       Release target branch (default: main)
  --dry-run             Print actions only; do not create the release

ENVIRONMENT:
  SKIP_DEPLOY_LINUX_ARM64=1   Always set when invoking build.sh (no Parallels deploy)

EOF
}

while [[ $# -gt 0 ]]; do
	case $1 in
	-h | --help)
		usage
		exit 0
		;;
	-t | --tag)
		TAG="${2:-}"
		shift 2
		;;
	--skip-build)
		SKIP_BUILD=true
		shift
		;;
	--generate-notes)
		GENERATE_NOTES=true
		shift
		;;
	--target)
		TARGET_BRANCH="${2:-}"
		shift 2
		;;
	--dry-run)
		DRY_RUN=true
		shift
		;;
	*)
		print_msg "$RED" "Unknown option: $1"
		usage
		exit 1
		;;
	esac
done

require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		print_msg "$RED" "Missing required command: $1"
		exit 1
	fi
}

require_cmd go
require_cmd gh
require_cmd git

if [[ "$(uname -s)" != "Darwin" ]]; then
	print_msg "$YELLOW" "Warning: preferred host is macOS (codesign / darwin compress). Continuing on $(uname -s)."
fi

if ! gh auth status >/dev/null 2>&1; then
	print_msg "$RED" "gh is not authenticated. Run: gh auth login"
	exit 1
fi

ASSETS=(
	"${BUILD_DIR}/${BINARY_NAME}_linux_amd64"
	"${BUILD_DIR}/${BINARY_NAME}_linux_arm64"
	"${BUILD_DIR}/${BINARY_NAME}_darwin_amd64"
	"${BUILD_DIR}/${BINARY_NAME}_darwin_arm64"
	"${BUILD_DIR}/${BINARY_NAME}_windows_amd64.exe"
	"${BUILD_DIR}/${BINARY_NAME}_windows_arm64.exe"
)

read_version_from_file() {
	sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$VERSION_FILE" | head -1
}

if [[ "$SKIP_BUILD" != true ]]; then
	print_msg "$BLUE" "Building all platforms via build.sh (UPX / darwin arm64 gzip)..."
	SKIP_DEPLOY_LINUX_ARM64=1 ./build.sh --clean --all
else
	print_msg "$YELLOW" "Skip build; using existing ${BUILD_DIR}/"
fi

for f in "${ASSETS[@]}"; do
	if [[ ! -f "$f" ]]; then
		print_msg "$RED" "Missing asset: $f"
		exit 1
	fi
done

if [[ -z "$TAG" ]]; then
	TAG="$(read_version_from_file)"
fi
if [[ -z "$TAG" ]]; then
	print_msg "$RED" "Cannot determine release tag. Pass --tag or rebuild so ${VERSION_FILE} has Version."
	exit 1
fi

TITLE="${BINARY_NAME} ${TAG}"
NOTES="$(
	cat <<EOF
版本 ${TAG}。二进制在本机 macOS 上用 \`./build.sh --all\` 构建（UPX；darwin arm64 为 gzip 自解压壳）。

## 附件
- \`yinstall_linux_amd64\`、\`yinstall_linux_arm64\`（UPX）
- \`yinstall_darwin_amd64\`（UPX）、\`yinstall_darwin_arm64\`（gzip 壳）
- \`yinstall_windows_amd64.exe\`（UPX）、\`yinstall_windows_arm64.exe\`（未压缩）

## 说明
- build.sh 中的虚拟机部署和本机目录拷贝仅供本地使用，不会作为本次 Release 附件。
EOF
)"

print_msg "$BLUE" "=========================================="
print_msg "$BLUE" "Release plan"
print_msg "$BLUE" "=========================================="
echo "  tag:    ${TAG}"
echo "  title:  ${TITLE}"
echo "  target: ${TARGET_BRANCH}"
echo "  assets:"
for f in "${ASSETS[@]}"; do
	sz=$(du -h "$f" | cut -f1)
	echo "    - ${f} (${sz})"
done
echo ""

if [[ "$DRY_RUN" == true ]]; then
	print_msg "$YELLOW" "Dry-run: not creating release."
	exit 0
fi

if gh release view "$TAG" >/dev/null 2>&1; then
	print_msg "$RED" "Release/tag already exists: ${TAG}"
	print_msg "$YELLOW" "Delete first if intentional: gh release delete ${TAG} --yes && git push origin :refs/tags/${TAG}"
	exit 1
fi

ARGS=(release create "$TAG" --target "$TARGET_BRANCH" --title "$TITLE")
if [[ "$GENERATE_NOTES" == true ]]; then
	ARGS+=(--generate-notes)
else
	ARGS+=(--notes "$NOTES")
fi
ARGS+=("${ASSETS[@]}")

print_msg "$YELLOW" "Creating GitHub release..."
gh "${ARGS[@]}"

URL="$(gh release view "$TAG" --json url --jq .url)"
print_msg "$GREEN" "✓ Release created: ${URL}"
print_msg "$YELLOW" "Note: ${VERSION_FILE} may be dirty from build.sh; do not commit it unless you intend to."
echo "$URL"
