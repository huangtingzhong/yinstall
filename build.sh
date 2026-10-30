#!/bin/bash

# Build script for yinstall - Multi-platform build with anti-decompilation
# Supports: Linux (amd64, arm64), Windows (amd64, arm64), MacOS (amd64, arm64)
# Artifact names use MacOS (not darwin); GOOS remains darwin for the Go toolchain.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Project configuration
BINARY_NAME="yinstall"
BUILD_DIR="build"
CMD_PATH="./cmd/yinstall"

# After linux/arm64 build: optional deploy to Parallels guest (passwordless SSH + sudo -n).
# M14: opt-in only — set DEPLOY_LINUX_ARM64=1 (or true) to enable; default is skip.
DEPLOY_LINUX_ARM64_HOST="10.10.10.147"
DEPLOY_LINUX_ARM64_USER="parallels"
DEPLOY_LINUX_ARM64_PATH="/usr/bin/yinstall"

# Version: CalVer YY.MM.PATCH (Asia/Shanghai). See calver_* helpers below.
# Release: v25.10.0, v25.10.1, ...  |  Dev build: 25.10.0+dev
TZ_CN="Asia/Shanghai"
VERSION=""
BUILD_TIME=""
GIT_COMMIT=""
LDFLAGS=""
BUILDFLAGS="-trimpath"

# Refresh VERSION / BUILD_TIME / GIT_COMMIT / LDFLAGS (call after VERSION is chosen).
refresh_build_meta() {
    BUILD_TIME=$(TZ=$TZ_CN date '+%Y-%m-%d %H:%M:%S CST')
    GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    LDFLAGS="-s -w \
    -X 'main.Version=${VERSION}' \
    -X 'main.BuildTime=${BUILD_TIME}' \
    -X 'main.GitCommit=${GIT_COMMIT}'"
}

# Current calendar YY.MM in China timezone (zero-padded month).
calver_yy_mm() {
    TZ=$TZ_CN date '+%y.%m'
}

# List CalVer tags as VERSION lines (no leading v): 25.10.0
# $1=local -> local tags only; otherwise also merge RELEASE_REMOTE (for release bump).
# Remote failure: for mode!=local hard-fail (REL-2); do not silently fall back to local-only.
list_calver_versions() {
    local mode="${1:-local}"
    local remote="${RELEASE_REMOTE:-yasdb}"
    local remote_out=""
    local remote_err=""
    local raw=""

    raw=$(git tag -l 'v[0-9][0-9].[0-9][0-9].*' 2>/dev/null || true)
    if [ "$mode" != "local" ]; then
        if ! git remote get-url "$remote" >/dev/null 2>&1; then
            print_msg "$RED" "Error: git remote '${remote}' not found (set RELEASE_REMOTE=...; needed for CalVer bump)" >&2
            return 1
        fi
        # Capture stderr; non-zero exit must not be swallowed (REL-2).
        if ! remote_out=$(git ls-remote --tags --refs "$remote" 'v[0-9][0-9].[0-9][0-9].*' 2>/tmp/yinstall_ls_remote_err); then
            remote_err=$(cat /tmp/yinstall_ls_remote_err 2>/dev/null || true)
            rm -f /tmp/yinstall_ls_remote_err
            print_msg "$RED" "Error: git ls-remote ${remote} failed (cannot bump CalVer safely)" >&2
            [ -n "$remote_err" ] && echo "$remote_err" >&2
            return 1
        fi
        rm -f /tmp/yinstall_ls_remote_err
        raw="${raw}"$'\n'"$(echo "$remote_out" | awk '{print $2}' | sed 's|refs/tags/||')"
    fi
    echo "$raw" | sed 's/^v//' | grep -E '^[0-9]{2}\.[0-9]{2}\.[0-9]+$' | sort -u
    return 0
}

# Highest PATCH for a given YY.MM among known tags; prints -1 if none.
# $2 optional list mode (local|all), default all for release safety.
max_calver_patch() {
    local ym="$1"
    local mode="${2:-all}"
    local max=-1
    local ver patch
    local vers
    if ! vers=$(list_calver_versions "$mode"); then
        return 1
    fi
    while IFS= read -r ver; do
        [ -z "$ver" ] && continue
        case "$ver" in
            "${ym}".*)
                patch="${ver##*.}"
                if [ "$patch" -gt "$max" ] 2>/dev/null; then
                    max=$patch
                fi
                ;;
        esac
    done <<< "$vers"
    echo "$max"
}

# Next release version for current month: YY.MM.(max+1), first is .0
next_calver_release() {
    local ym
    local max
    ym=$(calver_yy_mm)
    if ! max=$(max_calver_patch "$ym" "all"); then
        return 1
    fi
    echo "${ym}.$((max + 1))"
}

# REL-2: before creating/pushing tag, ensure remote does not already have it.
release_assert_tag_absent_on_remote() {
    local tag="$1"
    local remote="${RELEASE_REMOTE:-yasdb}"
    local out=""
    if ! git remote get-url "$remote" >/dev/null 2>&1; then
        print_msg "$RED" "Error: git remote '${remote}' not found"
        return 1
    fi
    if ! out=$(git ls-remote --tags --refs "$remote" "refs/tags/${tag}" 2>/tmp/yinstall_ls_remote_err); then
        print_msg "$RED" "Error: git ls-remote ${remote} failed before tag push (REL-2)"
        cat /tmp/yinstall_ls_remote_err >&2 2>/dev/null || true
        rm -f /tmp/yinstall_ls_remote_err
        return 1
    fi
    rm -f /tmp/yinstall_ls_remote_err
    if echo "$out" | awk 'NF{found=1} END{exit !found}'; then
        print_msg "$RED" "Error: tag already exists on ${remote}: ${tag}"
        return 1
    fi
    return 0
}

# Latest CalVer overall (for +dev); local tags only (fast). If none, YY.MM.0
latest_calver() {
    local last
    last=$(list_calver_versions local | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)
    if [ -n "$last" ]; then
        echo "$last"
    else
        echo "$(calver_yy_mm).0"
    fi
}

# Normalize --tag value -> tag (vYY.MM.N) and VERSION (YY.MM.N)
# Prints: TAG VERSION  (errors on stderr)
normalize_release_tag() {
    local raw="$1"
    local ver
    ver="${raw#v}"
    if ! echo "$ver" | grep -Eq '^[0-9]{2}\.[0-9]{2}\.[0-9]+$'; then
        print_msg "$RED" "Error: --tag must be CalVer vYY.MM.PATCH (e.g. v25.10.0), got: ${raw}" >&2
        return 1
    fi
    echo "v${ver} ${ver}"
}

# Print colored message
print_msg() {
    local color=$1
    shift
    echo -e "${color}$@${NC}"
}

# Print header
print_header() {
    echo ""
    print_msg "$BLUE" "=========================================="
    print_msg "$BLUE" "$@"
    print_msg "$BLUE" "=========================================="
    echo ""
}

# Map Go GOOS to release artifact / display name (darwin -> MacOS).
release_os_name() {
    case "$1" in
        darwin) echo "MacOS" ;;
        *) echo "$1" ;;
    esac
}

# Build for specific platform
# $1=GOOS (darwin/linux/windows)  $2=GOARCH  $3=ext (.exe or empty)
build_platform() {
    local os=$1
    local arch=$2
    local ext=$3
    local name_os
    name_os=$(release_os_name "$os")

    local output_name="${BINARY_NAME}_${name_os}_${arch}${ext}"
    local output_path="${BUILD_DIR}/${output_name}"

    print_msg "$YELLOW" "Building ${name_os}/${arch}..."

    GOOS=$os GOARCH=$arch go build $BUILDFLAGS -ldflags "$LDFLAGS" -o "$output_path" $CMD_PATH

    if [ $? -eq 0 ]; then
        local size_before=$(du -h "$output_path" | cut -f1)
        print_msg "$GREEN" "✓ Built: ${output_name} (${size_before})"
        release_record_built "$output_name"

        # Compress with appropriate method based on platform and architecture
        if [ "$os" = "darwin" ] && [ "$arch" = "arm64" ]; then
            # MacOS ARM64: try UPX first, fallback to gzexe
            compress_macos_arm64 "$output_path"
        elif [ "$os" = "windows" ] && [ "$arch" = "arm64" ]; then
            # Windows ARM64: UPX not supported, skip compression
            print_msg "$YELLOW" "  ⚠ UPX compression not supported for Windows ARM64"
        else
            # Other platforms: use UPX
            compress_binary "$output_path" "$os"
        fi

        # Special handling for MacOS ARM64 - copy to specific path if exists
        if [ "$os" = "darwin" ] && [ "$arch" = "arm64" ]; then
            # Compat name for uname-based local paths (darwin vs MacOS artifact)
            ln -sfn "$(basename "$output_path")" "${BUILD_DIR}/${BINARY_NAME}_darwin_arm64"
            cp "$output_path" "${BUILD_DIR}/${BINARY_NAME}"
            local target_dir="/Users/yihan/Documents/owner/wendang/home"
            if [ -d "$target_dir" ]; then
                print_msg "$YELLOW" "Copying MacOS ARM version to ${target_dir}/yinstall..."
                cp "$output_path" "${target_dir}/yinstall"
                if [ $? -eq 0 ]; then
                    print_msg "$GREEN" "✓ Copied to ${target_dir}/yinstall"
                else
                    print_msg "$RED" "✗ Failed to copy to ${target_dir}"
                fi
            fi
        fi

        # Linux ARM64: upload to Parallels guest /usr/bin/yinstall
        if [ "$os" = "linux" ] && [ "$arch" = "arm64" ]; then
            deploy_linux_arm64 "$output_path"
        fi
    else
        print_msg "$RED" "✗ Failed to build ${os}/${arch}"
        return 1
    fi
}

# Deploy linux/arm64 binary to Parallels host (scp + sudo install).
# Host unreachable / SSH unavailable -> skip (return 0), do not fail the build.
deploy_linux_arm64() {
    local local_bin=$1
    local host="${DEPLOY_LINUX_ARM64_HOST}"
    local user="${DEPLOY_LINUX_ARM64_USER}"
    local remote_path="${DEPLOY_LINUX_ARM64_PATH}"
    local remote_tmp="/tmp/${BINARY_NAME}.$$"
    local ssh_opts=(-o BatchMode=yes -o ConnectTimeout=3 -o StrictHostKeyChecking=accept-new)

    if [ "${DEPLOY_LINUX_ARM64:-}" != "1" ] && [ "${DEPLOY_LINUX_ARM64:-}" != "true" ]; then
        print_msg "$YELLOW" "  ⚠ Skip deploy to ${user}@${host}:${remote_path} (set DEPLOY_LINUX_ARM64=1 to enable)"
        return 0
    fi
    # Legacy opt-out still honored if someone exports both.
    if [ "${SKIP_DEPLOY_LINUX_ARM64:-}" = "1" ] || [ "${SKIP_DEPLOY_LINUX_ARM64:-}" = "true" ]; then
        print_msg "$YELLOW" "  ⚠ Skip deploy to ${user}@${host}:${remote_path} (SKIP_DEPLOY_LINUX_ARM64)"
        return 0
    fi

    if [ ! -f "$local_bin" ]; then
        print_msg "$YELLOW" "  ⚠ Deploy skipped: binary not found: ${local_bin}"
        return 0
    fi

    print_msg "$YELLOW" "Checking ${host} before deploy..."

    # Reachability: TCP/22 via ssh; unreachable -> skip quietly for build
    if ! ssh "${ssh_opts[@]}" "${user}@${host}" "true" 2>/dev/null; then
        print_msg "$YELLOW" "  ⚠ ${user}@${host} not reachable (or no key login); skip deploy"
        return 0
    fi

    if ! ssh "${ssh_opts[@]}" "${user}@${host}" "sudo -n true" 2>/dev/null; then
        print_msg "$YELLOW" "  ⚠ sudo -n unavailable on ${host}; skip deploy"
        return 0
    fi

    print_msg "$YELLOW" "Deploying Linux ARM64 to ${user}@${host}:${remote_path}..."

    if ! scp "${ssh_opts[@]}" "$local_bin" "${user}@${host}:${remote_tmp}"; then
        print_msg "$YELLOW" "  ⚠ scp failed; skip deploy"
        return 0
    fi

    if ssh "${ssh_opts[@]}" "${user}@${host}" \
        "sudo -n install -m 755 '${remote_tmp}' '${remote_path}' && rm -f '${remote_tmp}' && ls -la '${remote_path}' && '${remote_path}' -v 2>/dev/null | head -3 || true"; then
        print_msg "$GREEN" "✓ Deployed to ${user}@${host}:${remote_path} (mode 755)"
    else
        print_msg "$YELLOW" "  ⚠ Remote install failed; skip (build continues)"
        ssh "${ssh_opts[@]}" "${user}@${host}" "rm -f '${remote_tmp}'" 2>/dev/null || true
    fi
    return 0
}

# Compress binary with UPX
compress_binary() {
    local binary_path=$1
    local os=$2

    # Check if UPX is available
    if ! command -v upx &> /dev/null; then
        return 0
    fi

    local size_before=$(stat -f%z "$binary_path" 2>/dev/null || stat -c%s "$binary_path" 2>/dev/null)

    print_msg "$YELLOW" "  Compressing with UPX..."

    # For MacOS, we need to handle code signing (GOOS=darwin)
    if [ "$os" = "darwin" ]; then
        # Remove existing signature first
        codesign --remove-signature "$binary_path" 2>/dev/null

        # Compress with UPX (use --force-macos for better compatibility)
        upx --best --lzma --force-macos "$binary_path" >/dev/null 2>&1

        local upx_result=$?

        if [ $upx_result -eq 0 ]; then
            # Ad-hoc sign the binary (allows it to run on MacOS)
            codesign -s - "$binary_path" >/dev/null 2>&1

            if [ $? -eq 0 ]; then
                local size_after=$(stat -f%z "$binary_path" 2>/dev/null || stat -c%s "$binary_path" 2>/dev/null)
                local reduction=$((($size_before - $size_after) * 100 / $size_before))
                print_msg "$GREEN" "  ✓ Compressed and signed (reduced by ${reduction}%)"
            else
                print_msg "$YELLOW" "  ⚠ Compressed but signing failed"
            fi
        else
            # UPX failed, likely ARM64 not supported - skip compression
            print_msg "$YELLOW" "  ⚠ UPX compression not supported for this architecture"
        fi
    else
        # For non-MacOS platforms, just compress
        upx --best --lzma "$binary_path" >/dev/null 2>&1

        if [ $? -eq 0 ]; then
            local size_after=$(stat -f%z "$binary_path" 2>/dev/null || stat -c%s "$binary_path" 2>/dev/null)
            local reduction=$((($size_before - $size_after) * 100 / $size_before))
            print_msg "$GREEN" "  ✓ Compressed (reduced by ${reduction}%)"
        else
            print_msg "$YELLOW" "  ⚠ UPX compression failed"
        fi
    fi
}

# Compress MacOS ARM64 binary using alternative methods
compress_macos_arm64() {
    local binary_path=$1
    local size_before=$(stat -f%z "$binary_path" 2>/dev/null)

    print_msg "$YELLOW" "  Compressing MacOS ARM64 binary..."

    # UPX doesn't support MacOS ARM64 well, use gzip wrapper directly
    if command -v gzip &> /dev/null; then
        # Create a self-extracting wrapper
        local temp_dir=$(mktemp -d)
        local compressed="${temp_dir}/payload.gz"
        local wrapper="${temp_dir}/wrapper.sh"

        # Compress the binary
        gzip -9 -c "$binary_path" > "$compressed"

        # Create wrapper script
        cat > "$wrapper" << 'WRAPPER_EOF'
#!/bin/bash
PAYLOAD_LINE=$(awk '/^__PAYLOAD_BEGIN__/ {print NR + 1; exit 0; }' "$0")
TEMP_BIN=$(mktemp)
tail -n +${PAYLOAD_LINE} "$0" | gunzip > "$TEMP_BIN"
chmod +x "$TEMP_BIN"
"$TEMP_BIN" "$@"
EXIT_CODE=$?
rm -f "$TEMP_BIN"
exit $EXIT_CODE
__PAYLOAD_BEGIN__
WRAPPER_EOF

        # Combine wrapper and compressed binary
        cat "$wrapper" "$compressed" > "${binary_path}.new"
        chmod +x "${binary_path}.new"

        # Sign the new binary
        codesign -s - "${binary_path}.new" >/dev/null 2>&1

        local size_after=$(stat -f%z "${binary_path}.new" 2>/dev/null)

        # Only replace if smaller
        if [ $size_after -lt $size_before ]; then
            mv "${binary_path}.new" "$binary_path"
            local reduction=$((($size_before - $size_after) * 100 / $size_before))
            print_msg "$GREEN" "  ✓ Compressed with gzip wrapper (reduced by ${reduction}%)"
            rm -rf "$temp_dir"
            return 0
        else
            rm -f "${binary_path}.new"
            print_msg "$YELLOW" "  ⚠ Gzip wrapper larger than original, keeping original"
        fi

        rm -rf "$temp_dir"
    else
        print_msg "$YELLOW" "  ⚠ gzip not available, keeping original binary"
    fi
}

# Clean build directory
clean_build() {
    print_msg "$YELLOW" "Cleaning build directory..."
    rm -rf "$BUILD_DIR"
    mkdir -p "$BUILD_DIR"
    print_msg "$GREEN" "✓ Clean complete"
}

# Release defaults (internal GitLab)
RELEASE_REMOTE="${RELEASE_REMOTE:-yasdb}"
GITLAB_HOST="${GITLAB_HOST:-https://git-tools.yasdb.com}"
GITLAB_PROJECT_PATH="${GITLAB_PROJECT_PATH:-huangtingzhong/yinstall}"  # override when git-tools path known

# Show help
show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Build yinstall for multiple platforms with anti-decompilation protection.

OPTIONS:
    -h, --help              Show this help message
    -c, --clean             Clean build directories before building
    -l, --linux             Build for Linux only
    -w, --windows           Build for Windows only
    -m, --macos             Build for MacOS only
    -a, --all               Build for all platforms (default)
    --current               Build for current platform only
    --release               CHANGELOG commit, then build (stamp SHA), then tag/push/Release
    --release-only          Same as --release (always rebuild after commit so -v matches tag)
    --tag TAG               Release tag CalVer vYY.MM.PATCH (default: next for this month)
    --allow-empty-changelog Allow release when [Unreleased] has no bullets
    --allow-dirty           Allow release with unrelated uncommitted changes

VERSION (CalVer YY.MM.PATCH, Asia/Shanghai):
    Release:  25.10.0, 25.10.1, ... (auto +1 in current month; first patch is 0)
    Dev build: <latest>+dev  (e.g. 25.10.0+dev); yinstall --version / embedded Version
    Tag form:  v25.10.0
    Notes:     CHANGELOG.md [Unreleased] (Chinese) -> tag message + GitLab Release
    REL-1:     commit CHANGELOG before build so binary GitCommit matches tagged HEAD
    REL-2:     CalVer bump / pre-push require successful git ls-remote (no silent fallback)

EXAMPLES:
    $0                      # all platform yinstall binaries (Version=…+dev)
    $0 --clean              # Clean and build all
    $0 --linux              # Linux yinstall only
    $0 --current            # current platform yinstall
    $0 --release            # next CalVer: commit notes -> build -> tag/push/Release
    $0 --release --tag v25.10.3
    $0 --release-only --tag v25.10.3   # alias of --release (still rebuilds after commit)

RELEASE ENV:
    RELEASE_REMOTE          git remote (default: yasdb)
    GITLAB_HOST             https://git-tools.yasdb.com
    GITLAB_PROJECT_PATH     huangtingzhong/yinstall
    GITLAB_TOKEN            or GITLAB_PRIVATE_TOKEN (api scope); else Keychain via scripts/creds.sh
    YINSTALL_GITLAB_KC_SERVICE  Keychain service (default: yops.gitlab.token, shared with yops)
    YINSTALL_GITLAB_KC_ACCOUNT  Keychain account (default: api)

    Token: reuse yops Keychain by default; or GITLAB_TOKEN / ./scripts/creds.sh set
    Check:                     ./scripts/creds.sh status

OUTPUT:
    Go binaries:     ${BUILD_DIR}/${BINARY_NAME}_<os|MacOS>_<arch>[.exe]

POST-BUILD (linux/arm64):
    After a successful linux/arm64 build, if ${DEPLOY_LINUX_ARM64_HOST} is reachable
    via passwordless SSH, upload to:
      ${DEPLOY_LINUX_ARM64_USER}@${DEPLOY_LINUX_ARM64_HOST}:${DEPLOY_LINUX_ARM64_PATH}
    (scp + sudo install -m 755). Unreachable host -> skip (build still succeeds).
    Force skip: SKIP_DEPLOY_LINUX_ARM64=1 $0 ...

PLATFORMS:
    Linux:   amd64, arm64
    Windows: amd64, arm64
    MacOS:   amd64, arm64 (Apple Silicon); artifacts named yinstall_MacOS_<arch>

ANTI-DECOMPILATION:
    - Symbol table stripped (-s)
    - Debug info stripped (-w)
    - File paths removed (-trimpath)
    - Version info embedded

COMPRESSION:
    - UPX compression for supported platforms:
      * Linux amd64/arm64: UPX --best --lzma (~68-70% reduction)
      * Windows amd64: UPX --best --lzma (~68% reduction)
      * MacOS amd64: UPX + codesign (~67% reduction)
    - Alternative compression for unsupported platforms:
      * MacOS ARM64: gzip self-extracting wrapper (~59% reduction)
      * Windows ARM64: No compression (UPX not supported)

REQUIREMENTS:
    - Go 1.19+ (required for yinstall)
    - upx (optional, for UPX compression)
    - gzip (optional, for MacOS ARM64 compression)
    - codesign (MacOS only, for signing compressed binaries)

INSTALL UPX:
    MacOS:   brew install upx
    Linux:   apt-get install upx-ucl  or  yum install upx
    Windows: Download from https://upx.github.io/

EOF
}

# Build all platforms
build_all() {
    print_header "Building for all platforms"

    # Linux
    build_platform "linux" "amd64" ""
    build_platform "linux" "arm64" ""

    # Windows
    build_platform "windows" "amd64" ".exe"
    build_platform "windows" "arm64" ".exe"

    # MacOS (GOOS=darwin; artifacts yinstall_MacOS_*)
    build_platform "darwin" "amd64" ""
    build_platform "darwin" "arm64" ""
}

# Build Linux only
build_linux() {
    print_header "Building for Linux"
    build_platform "linux" "amd64" ""
    build_platform "linux" "arm64" ""
}

# Build Windows only
build_windows() {
    print_header "Building for Windows"
    build_platform "windows" "amd64" ".exe"
    build_platform "windows" "arm64" ".exe"
}

# Build MacOS only (artifacts: yinstall_MacOS_*; GOOS=darwin)
build_macos() {
    print_header "Building for MacOS"
    build_platform "darwin" "amd64" ""
    build_platform "darwin" "arm64" ""
}

# Build current platform only
build_current() {
    print_header "Building for current platform"

    local current_os=$(go env GOOS)
    local current_arch=$(go env GOARCH)
    local ext=""

    if [ "$current_os" = "windows" ]; then
        ext=".exe"
    fi

    build_platform "$current_os" "$current_arch" "$ext"
}

# Show build summary
show_summary() {
    echo ""
    print_header "Build Summary"

    if [ -d "$BUILD_DIR" ]; then
        print_msg "$GREEN" "Build directory: ${BUILD_DIR}/"
        echo ""
        ls -lh "$BUILD_DIR"/ | tail -n +2 | while read -r line; do
            echo "  $line"
        done
        echo ""

        local total_size=$(du -sh "$BUILD_DIR" | cut -f1)
        print_msg "$BLUE" "Total size: ${total_size}"
    else
        print_msg "$RED" "No build directory found"
    fi

    echo ""
}

# Update version.go file with current build information
update_version_file() {
    local version_file="cmd/yinstall/version.go"

    print_msg "$YELLOW" "Updating version information..."

    cat > "$version_file" << EOF
package main

// Version information
// This file is automatically updated by build.sh
// Last updated: $(TZ=$TZ_CN date '+%Y-%m-%d %H:%M:%S CST')
var (
	Version   = "${VERSION}"
	BuildTime = "${BUILD_TIME}"
	GitCommit = "${GIT_COMMIT}"
)
EOF

    if [ $? -eq 0 ]; then
        print_msg "$GREEN" "✓ Version updated: ${VERSION}"
    else
        print_msg "$RED" "✗ Failed to update version file"
        return 1
    fi
}

# Resolve path to scripts/creds.sh (same repo as this build.sh).
creds_script() {
    local here
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    echo "${here}/scripts/creds.sh"
}

# Resolve GitLab private token: env first, then scripts/creds.sh（默认共用 yops 钥匙串）。
gitlab_token() {
    if [ -n "${GITLAB_TOKEN:-}" ]; then
        echo "$GITLAB_TOKEN"
        return 0
    fi
    if [ -n "${GITLAB_PRIVATE_TOKEN:-}" ]; then
        echo "$GITLAB_PRIVATE_TOKEN"
        return 0
    fi
    local cs token here yops_creds
    cs=$(creds_script)
    if [ -x "$cs" ] || [ -f "$cs" ]; then
        if token=$("$cs" get 2>/dev/null) && [ -n "$token" ]; then
            echo "$token"
            return 0
        fi
    fi
    # 兜底：直接调同机 yops 的 creds（若本仓默认未命中）
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    yops_creds="${here}/../yops/scripts/creds.sh"
    if [ -f "$yops_creds" ]; then
        if token=$("$yops_creds" get 2>/dev/null) && [ -n "$token" ]; then
            echo "$token"
            return 0
        fi
    fi
    echo ""
}

# URL-encode project path for GitLab API (owner/repo -> owner%2Frepo).
gitlab_project_id_encoded() {
    echo "${GITLAB_PROJECT_PATH}" | sed 's|/|%2F|g'
}

# List release asset binaries under build/ (platform-named files only).
# REL-R2: 本轮 build_platform 写入的正式名清单; 有清单则上传不得混入更旧产物.
RELEASE_BUILT_LIST="${BUILD_DIR}/.yinstall_built_this_run"

release_required_names() {
    cat <<EOF
${BINARY_NAME}_linux_amd64
${BINARY_NAME}_linux_arm64
${BINARY_NAME}_MacOS_amd64
${BINARY_NAME}_MacOS_arm64
${BINARY_NAME}_windows_amd64.exe
${BINARY_NAME}_windows_arm64.exe
EOF
}

release_reset_built_list() {
    mkdir -p "$BUILD_DIR"
    : >"$RELEASE_BUILT_LIST"
}

release_record_built() {
    local name="$1"
    [ -n "$name" ] || return 0
    mkdir -p "$BUILD_DIR"
    echo "$name" >>"$RELEASE_BUILT_LIST"
}

list_release_binaries() {
    local f base
    local found=0
    local use_manifest=0
    if [ ! -d "$BUILD_DIR" ]; then
        return 1
    fi
    if [ -s "$RELEASE_BUILT_LIST" ]; then
        use_manifest=1
    fi
    shopt -s nullglob
    # REL-4: 只收正式产物名, 跳过残留 yinstall_darwin_* 等.
    for f in "$BUILD_DIR"/${BINARY_NAME}_*; do
        [ -f "$f" ] || continue
        base=$(basename "$f")
        case "$base" in
            ${BINARY_NAME}_linux_amd64|${BINARY_NAME}_linux_arm64|\
            ${BINARY_NAME}_MacOS_amd64|${BINARY_NAME}_MacOS_arm64|\
            ${BINARY_NAME}_windows_amd64.exe|${BINARY_NAME}_windows_arm64.exe)
                if [ "$use_manifest" -eq 1 ]; then
                    # REL-R2: 仅本轮记录过的正式名
                    grep -Fxq "$base" "$RELEASE_BUILT_LIST" || continue
                fi
                echo "$f"
                found=1
                ;;
        esac
    done
    shopt -u nullglob
    if [ "$found" -eq 0 ]; then
        return 1
    fi
}

# REL-R2: Release 须六平台齐全且均在本轮清单 (或 skip-build 无清单时磁盘齐全).
release_assert_binaries_complete() {
    local name f miss=0
    local use_manifest=0
    [ -s "$RELEASE_BUILT_LIST" ] && use_manifest=1
    while IFS= read -r name; do
        [ -z "$name" ] && continue
        f="${BUILD_DIR}/${name}"
        if [ ! -f "$f" ]; then
            print_msg "$RED" "  missing release binary: ${name}"
            miss=1
            continue
        fi
        if [ "$use_manifest" -eq 1 ] && ! grep -Fxq "$name" "$RELEASE_BUILT_LIST"; then
            print_msg "$RED" "  stale/not rebuilt this run: ${name} (partial -l/-m/-w would mix old packages)"
            miss=1
        fi
    done < <(release_required_names)
    if [ "$miss" -ne 0 ]; then
        print_msg "$YELLOW" "  hint: ./build.sh -a --release  (full rebuild), or clean ${BUILD_DIR}/ first"
        return 1
    fi
    return 0
}

CHANGELOG_FILE="CHANGELOG.md"

# Extract [Unreleased] body (stdout). Empty sections without bullets still returned.
changelog_extract_unreleased() {
    local file="${1:-$CHANGELOG_FILE}"
    python3 - "$file" <<'PY'
import re, sys
path = sys.argv[1]
text = open(path, encoding="utf-8").read()
m = re.search(r"(?ms)^## \[Unreleased\]\s*\n(.*?)(?=^## \[|\Z)", text)
if not m:
    sys.stderr.write("ERROR: ## [Unreleased] section not found in %s\n" % path)
    sys.exit(2)
body = m.group(1).strip("\n")
print(body)
PY
}

# Return 0 if Unreleased has at least one markdown bullet list item.
changelog_unreleased_has_items() {
    local body="$1"
    echo "$body" | grep -Eq '^[[:space:]]*[-*][[:space:]]+'
}

# Move Unreleased body into ## [VER] - DATE; reset Unreleased skeleton (Chinese headers).
changelog_promote_unreleased() {
    local ver="$1"
    local date="$2"
    local file="${3:-$CHANGELOG_FILE}"
    python3 - "$file" "$ver" "$date" <<'PY'
import re, sys
path, ver, date = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding="utf-8").read()
m = re.search(r"(?ms)^## \[Unreleased\]\s*\n(.*?)(?=^## \[|\Z)", text)
if not m:
    sys.stderr.write("ERROR: ## [Unreleased] not found\n")
    sys.exit(2)
body = m.group(1).strip("\n")
skeleton = (
    "## [Unreleased]\n\n"
    "### Added\n\n"
    "### Changed\n\n"
    "### Fixed\n\n"
)
section = "## [%s] - %s\n\n%s\n\n" % (ver, date, body if body.strip() else "### Changed\n\n- （无 Unreleased 条目）\n")
# Replace Unreleased block with skeleton + new version section
new_text, n = re.subn(
    r"(?ms)^## \[Unreleased\]\s*\n(.*?)(?=^## \[|\Z)",
    skeleton + section,
    text,
    count=1,
)
if n != 1:
    sys.stderr.write("ERROR: failed to rewrite Unreleased\n")
    sys.exit(2)
open(path, "w", encoding="utf-8").write(new_text)
PY
}

# Write release notes file for tag -F / GitLab description.
changelog_write_notes() {
    local ver="$1"
    local body="$2"
    local out="$3"
    {
        echo "yinstall ${ver}"
        echo ""
        if [ -n "$(echo "$body" | tr -d '[:space:]')" ]; then
            echo "$body"
            echo ""
        fi
        echo "---"
        echo "Commit: ${GIT_COMMIT}"
        echo "Build time: ${BUILD_TIME}"
    } >"$out"
}

# release_path_ok: 发版 dirty 白名单路径 (REL-6 共用, 不重复写 case).
release_path_ok() {
    case "$1" in
        build/*|build) return 0 ;;
        CHANGELOG.md|cmd/yinstall/version.go) return 0 ;;
        *.log|yinstall_debug*|yinstall-diag-*) return 0 ;;
    esac
    return 1
}

# Fail if unrelated dirty files exist (unless allow_dirty=true).
release_check_dirty() {
    local allow_dirty="$1"
    local line path src dst
    local bad=0
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        path="${line:3}"
        # REL-6: rename "old -> new" 两侧都要在白名单.
        if [[ "$path" == *" -> "* ]]; then
            src="${path%% -> *}"
            dst="${path#* -> }"
            if release_path_ok "$src" && release_path_ok "$dst"; then
                continue
            fi
        elif release_path_ok "$path"; then
            continue
        fi
        if [ "$bad" -eq 0 ]; then
            print_msg "$RED" "Error: unrelated uncommitted changes (use --allow-dirty to override):"
        fi
        echo "  $line"
        bad=1
    done < <(git status --porcelain)
    if [ "$bad" -ne 0 ] && [ "$allow_dirty" != true ]; then
        return 1
    fi
    if [ "$bad" -ne 0 ]; then
        print_msg "$YELLOW" "Warning: --allow-dirty; proceeding with dirty tree"
    fi
    return 0
}

# Commit CHANGELOG + version.go for this release.
release_commit_changelog() {
    local ver="$1"
    git add "$CHANGELOG_FILE" cmd/yinstall/version.go
    if git diff --cached --quiet; then
        print_msg "$YELLOW" "No staged CHANGELOG/version.go changes to commit"
        return 0
    fi
    git commit -m "$(cat <<EOF
chore: release ${ver}

EOF
)"
    print_msg "$GREEN" "✓ Committed CHANGELOG for ${ver}"
    # Refresh commit id embedded in notes after commit
    GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
}

# Resolve browser download URL for a generic package file (login required if project internal).
# Fallback empty -> caller uses API generic URL.
gitlab_package_download_url() {
    local token=$1
    local api=$2
    local pkg_ver=$3
    local name=$4
    local fid
    fid=$(TOKEN="$token" API="$api" PKG_VER="$pkg_ver" NAME="$name" python3 - <<'PY'
import json, os, urllib.request
from urllib.parse import quote
api = os.environ["API"]
token = os.environ["TOKEN"]
ver = os.environ["PKG_VER"]
name = os.environ["NAME"]
req = urllib.request.Request(
    api + "/packages?package_type=generic&package_name=yinstall&package_version=" + quote(ver, safe="."),
    headers={"PRIVATE-TOKEN": token},
)
try:
    pkgs = json.load(urllib.request.urlopen(req))
except Exception:
    raise SystemExit(0)
pid = None
for p in pkgs:
    if p.get("name") == "yinstall" and str(p.get("version")) == ver:
        pid = p["id"]
        break
if pid is None:
    raise SystemExit(0)
req = urllib.request.Request(
    api + "/packages/%s/package_files" % pid,
    headers={"PRIVATE-TOKEN": token},
)
files = json.load(urllib.request.urlopen(req))
for f in files:
    if f.get("file_name") == name:
        print(f["id"])
        break
PY
) || true
    if [ -n "$fid" ]; then
        echo "${GITLAB_HOST}/${GITLAB_PROJECT_PATH}/-/package_files/${fid}/download"
    fi
}

# Create git tag, push remote, create GitLab Release and upload binaries.
# $1=tag  $2=notes_file
do_release() {
    local tag=$1
    local notes_file=$2
    local token
    local project_enc
    local api
    local f
    local name
    local upload_json
    local file_url
    local full_url
    local http_code
    local link_payload
    local remote_url
    local payload

    print_header "Release ${tag}"

    token=$(gitlab_token)
    if [ -z "$token" ]; then
        print_msg "$RED" "Error: no GitLab token (api scope)"
        print_msg "$YELLOW" "  set GITLAB_TOKEN / GITLAB_PRIVATE_TOKEN, or: ./scripts/creds.sh set"
        return 1
    fi

    if ! git rev-parse --git-dir >/dev/null 2>&1; then
        print_msg "$RED" "Error: not a git repository"
        return 1
    fi

    if ! git remote get-url "$RELEASE_REMOTE" >/dev/null 2>&1; then
        print_msg "$RED" "Error: git remote '${RELEASE_REMOTE}' not found (set RELEASE_REMOTE=...)"
        return 1
    fi
    remote_url=$(git remote get-url "$RELEASE_REMOTE")
    print_msg "$BLUE" "Remote: ${RELEASE_REMOTE} (${remote_url})"

    if ! list_release_binaries >/dev/null; then
        print_msg "$RED" "Error: no ${BINARY_NAME}_* binaries in ${BUILD_DIR}/"
        return 1
    fi
    if ! release_assert_binaries_complete; then
        print_msg "$RED" "Error: release binaries incomplete or stale (REL-R2)"
        return 1
    fi

    if git rev-parse "$tag" >/dev/null 2>&1; then
        print_msg "$RED" "Error: tag already exists locally: ${tag}"
        return 1
    fi
    if ! release_assert_tag_absent_on_remote "$tag"; then
        return 1
    fi

    if [ -z "$notes_file" ] || [ ! -f "$notes_file" ]; then
        print_msg "$RED" "Error: release notes file missing"
        return 1
    fi

    print_msg "$YELLOW" "Creating annotated tag ${tag}..."
    git tag -a "$tag" -F "$notes_file"
    print_msg "$GREEN" "✓ Tag created: ${tag}"

    print_msg "$YELLOW" "Pushing HEAD and tag to ${RELEASE_REMOTE}..."
    git push "$RELEASE_REMOTE" HEAD
    git push "$RELEASE_REMOTE" "$tag"
    print_msg "$GREEN" "✓ Pushed to ${RELEASE_REMOTE}"

    project_enc=$(gitlab_project_id_encoded)
    api="${GITLAB_HOST}/api/v4/projects/${project_enc}"

    print_msg "$YELLOW" "Creating GitLab Release ${tag}..."
    payload=$(NOTES_FILE="$notes_file" TAG="$tag" python3 - <<'PY'
import json, os
notes = open(os.environ["NOTES_FILE"], encoding="utf-8").read()
tag = os.environ["TAG"]
print(json.dumps({
    "name": "yinstall %s" % tag,
    "tag_name": tag,
    "description": notes,
}, ensure_ascii=False))
PY
)
    http_code=$(curl -sS -o /tmp/yinstall_release_create.json -w "%{http_code}" \
        --header "PRIVATE-TOKEN: ${token}" \
        --header "Content-Type: application/json" \
        --data "$payload" \
        "${api}/releases")
    if [ "$http_code" != "201" ] && [ "$http_code" != "200" ]; then
        print_msg "$RED" "Error: create release HTTP ${http_code}"
        cat /tmp/yinstall_release_create.json 2>/dev/null || true
        echo ""
        return 1
    fi
    print_msg "$GREEN" "✓ Release created"

    # Upload to Generic Package Registry (durable). Do NOT use project /uploads/ —
    # those are markdown attachments; host-root /uploads/... 404s; not for Release downloads.
    local pkg_ver="${VERSION:-}"
    if [ -z "$pkg_ver" ]; then
        pkg_ver="${tag#v}"
    fi
    print_msg "$YELLOW" "Uploading binaries to Package Registry (yinstall/${pkg_ver})..."
    while IFS= read -r f; do
        name=$(basename "$f")
        print_msg "$YELLOW" "  upload ${name}..."
        http_code=$(curl -sS -o /tmp/yinstall_pkg_upload.json -w "%{http_code}" \
            --header "PRIVATE-TOKEN: ${token}" \
            --upload-file "$f" \
            "${api}/packages/generic/yinstall/${pkg_ver}/${name}")
        if [ "$http_code" != "201" ] && [ "$http_code" != "200" ]; then
            print_msg "$RED" "  ✗ package upload HTTP ${http_code} for ${name}"
            cat /tmp/yinstall_pkg_upload.json 2>/dev/null || true
            echo ""
            return 1
        fi
        full_url=$(gitlab_package_download_url "$token" "$api" "$pkg_ver" "$name")
        if [ -z "$full_url" ]; then
            full_url="${api}/packages/generic/yinstall/${pkg_ver}/${name}"
        fi
        link_payload=$(NAME="$name" URL="$full_url" python3 - <<'PY'
import json, os
print(json.dumps({
    "name": os.environ["NAME"],
    "url": os.environ["URL"],
    "link_type": "package",
}, ensure_ascii=False))
PY
)
        http_code=$(curl -sS -o /tmp/yinstall_release_link.json -w "%{http_code}" \
            --header "PRIVATE-TOKEN: ${token}" \
            --header "Content-Type: application/json" \
            --data "$link_payload" \
            "${api}/releases/${tag}/assets/links")
        if [ "$http_code" != "201" ] && [ "$http_code" != "200" ]; then
            print_msg "$RED" "  ✗ link asset failed HTTP ${http_code} for ${name}"
            cat /tmp/yinstall_release_link.json 2>/dev/null || true
            echo ""
            return 1
        fi
        print_msg "$GREEN" "  ✓ ${name}"
    done < <(list_release_binaries)

    echo ""
    print_msg "$GREEN" "✓ Release complete: ${GITLAB_HOST}/${GITLAB_PROJECT_PATH}/-/releases/${tag}"
}

# Main script
main() {
    # Parse arguments first
    local do_clean=false
    local build_target="all"
    local do_release=false
    local release_only=false
    local release_tag=""
    local skip_build=false
    local allow_empty_changelog=false
    local allow_dirty=false
    local unreleased_body=""
    local notes_file=""
    local release_date=""

    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -c|--clean)
                do_clean=true
                shift
                ;;
            -l|--linux)
                build_target="linux"
                shift
                ;;
            -w|--windows)
                build_target="windows"
                shift
                ;;
            -m|--macos)
                build_target="macos"
                shift
                ;;
            -a|--all)
                build_target="all"
                shift
                ;;
            --current)
                build_target="current"
                shift
                ;;
            --release)
                do_release=true
                shift
                ;;
            --release-only)
                do_release=true
                release_only=true
                skip_build=true
                shift
                ;;
            --tag)
                release_tag="$2"
                if [ -z "$release_tag" ]; then
                    print_msg "$RED" "Error: --tag requires a value"
                    exit 1
                fi
                shift 2
                ;;
            --allow-empty-changelog)
                allow_empty_changelog=true
                shift
                ;;
            --allow-dirty)
                allow_dirty=true
                shift
                ;;
            *)
                print_msg "$RED" "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done

    # Resolve CalVer Version / release tag
    if [ "$do_release" = true ]; then
        if [ -n "$release_tag" ]; then
            local norm
            if ! norm=$(normalize_release_tag "$release_tag"); then
                exit 1
            fi
            release_tag="${norm%% *}"
            VERSION="${norm##* }"
        else
            if ! VERSION=$(next_calver_release); then
                exit 1
            fi
            release_tag="v${VERSION}"
        fi
        # REL-1: --release-only also rebuilds after CHANGELOG commit (binary SHA = tag HEAD).
        if [ "$release_only" = true ]; then
            print_msg "$YELLOW" "Note: --release-only still rebuilds after commit (REL-1)"
            skip_build=false
        fi
    else
        VERSION="$(latest_calver)+dev"
        release_tag=""
    fi
    refresh_build_meta

    # Show build info
    print_header "yinstall Build Script"
    print_msg "$BLUE" "Version:    ${VERSION}"
    print_msg "$BLUE" "Build Time: ${BUILD_TIME}"
    print_msg "$BLUE" "Git Commit: ${GIT_COMMIT}"
    if [ "$do_release" = true ]; then
        print_msg "$BLUE" "Release:    ${release_tag} -> ${RELEASE_REMOTE}"
    fi
    echo ""

    if [ "$do_release" = true ]; then
        if [ ! -f "$CHANGELOG_FILE" ]; then
            print_msg "$RED" "Error: ${CHANGELOG_FILE} missing"
            exit 1
        fi
        if ! release_check_dirty "$allow_dirty"; then
            exit 1
        fi
        unreleased_body=$(changelog_extract_unreleased "$CHANGELOG_FILE") || exit 1
        if ! changelog_unreleased_has_items "$unreleased_body"; then
            if [ "$allow_empty_changelog" != true ]; then
                print_msg "$RED" "Error: CHANGELOG [Unreleased] has no bullet items"
                print_msg "$YELLOW" "  add entries, or pass --allow-empty-changelog"
                exit 1
            fi
            print_msg "$YELLOW" "Warning: empty Unreleased; continuing (--allow-empty-changelog)"
        fi
        notes_file=$(mktemp /tmp/yinstall_release_notes.XXXXXX)
        changelog_write_notes "$VERSION" "$unreleased_body" "$notes_file"
        print_msg "$GREEN" "✓ Release notes prepared from CHANGELOG [Unreleased]"

        # REL-1: promote + commit CHANGELOG/version.go BEFORE build so ldflags GitCommit matches tag.
        echo ""
        release_date=$(TZ=$TZ_CN date '+%Y-%m-%d')
        print_msg "$YELLOW" "Promoting CHANGELOG [Unreleased] -> [${VERSION}] - ${release_date}..."
        changelog_promote_unreleased "$VERSION" "$release_date" "$CHANGELOG_FILE"
        update_version_file
        release_commit_changelog "$VERSION"
        # REL-1: ldflags GitCommit must be post-commit HEAD (do not rewrite version.go again —
        # that would dirty the tree / force amend). Binary stamp comes from refresh_build_meta.
        refresh_build_meta
        changelog_write_notes "$VERSION" "$unreleased_body" "$notes_file"
        print_msg "$BLUE" "Post-commit GitCommit for build: ${GIT_COMMIT}"
    fi

    if [ "$skip_build" = false ]; then
        # Clean if requested
        if [ "$do_clean" = true ]; then
            clean_build
        else
            mkdir -p "$BUILD_DIR"
        fi
        # REL-R2: 本轮重建清单, 避免 -l/--release 混上传旧 MacOS/Windows.
        release_reset_built_list

        # Check if go is installed
        if ! command -v go &> /dev/null; then
            print_msg "$RED" "Error: Go is not installed"
            exit 1
        fi

        # Dev builds: stamp version.go here. Release already wrote version.go before commit.
        if [ "$do_release" != true ]; then
            update_version_file
            echo ""
        fi

        # Build based on target
        case $build_target in
            all)
                build_all
                ;;
            linux)
                build_linux
                ;;
            windows)
                build_windows
                ;;
            macos)
                build_macos
                ;;
            current)
                build_current
                ;;
        esac

        # Show summary
        show_summary

        print_msg "$GREEN" "✓ Build process complete!"
    else
        print_msg "$YELLOW" "Skip build; using existing ${BUILD_DIR}/"
        rm -f "$RELEASE_BUILT_LIST"
        show_summary
    fi

    if [ "$do_release" = true ]; then
        echo ""
        do_release "$release_tag" "$notes_file"
        rm -f "$notes_file"
    fi
}

# Run main function
main "$@"
