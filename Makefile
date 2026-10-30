.PHONY: all clean build-all build-linux build-windows build-darwin build-current help check-debug-logging check-test-layout check-steps release

# Project information
BINARY_NAME=yinstall
BUILD_DIR=build
CMD_PATH=./cmd/yinstall
# Prefer ./build.sh for CalVer releases. Make embeds latest tag +dev.
VERSION=$(shell ver=$$(git tag -l 'v[0-9][0-9].[0-9][0-9].*' 2>/dev/null | sed 's/^v//' | grep -E '^[0-9]{2}\.[0-9]{2}\.[0-9]+$$' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1); if [ -n "$$ver" ]; then echo "$$ver+dev"; else TZ=Asia/Shanghai date '+%y.%m.0+dev'; fi)
BUILD_TIME=$(shell TZ=Asia/Shanghai date '+%Y-%m-%d %H:%M:%S CST')
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go build flags
LDFLAGS=-ldflags "\
	-s -w \
	-X 'main.Version=$(VERSION)' \
	-X 'main.BuildTime=$(BUILD_TIME)' \
	-X 'main.GitCommit=$(GIT_COMMIT)'"
BUILDFLAGS=-trimpath

# Default target
all: clean build-all

help:
	@echo "Available targets:"
	@echo "  make all           - Clean and build all platforms"
	@echo "  make build-all     - Build for all platforms"
	@echo "  make build-linux    - Build for Linux (amd64, arm64)"
	@echo "  make build-windows  - Build for Windows (amd64, arm64)"
	@echo "  make build-darwin   - Build for MacOS (amd64, arm64); artifacts yinstall_MacOS_*"
	@echo "  make build-current  - Build for current platform only"
	@echo "  make release        - CalVer GitLab release (./build.sh --release)"
	@echo "  make clean          - Remove build directory"
	@echo ""
	@echo "Preferred: ./build.sh [--current|--release]"
	@echo "  make check-debug-logging - Static check for install-step debug patterns"
	@echo "  make check-test-layout   - Fail if *_test.go outside test/go/ (except legacy allowlist)"
	@echo "  make check-steps         - Validate registry step IDs (no hardcoded Step.ID)"
	@echo ""
	@echo "Output directory: $(BUILD_DIR)/"

check-debug-logging:
	@bash scripts/check-debug-logging.sh

check-test-layout:
	@bash scripts/check-test-layout.sh

check-steps:
	@bash scripts/check-steps.sh

# CalVer 发版（git-tools GitLab）；与 yops 同流程
release:
	@bash ./build.sh --release

# Version 经 LDFLAGS -X 注入；完整发版请用 ./build.sh（会更新 version.go）
update-version:
	@echo "Version via ldflags: $(VERSION) ($(GIT_COMMIT))"

clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

build-all: build-linux build-windows build-darwin
	@echo ""
	@echo "=========================================="
	@echo "Build complete! Files in $(BUILD_DIR)/"
	@echo "=========================================="
	@ls -lh $(BUILD_DIR)/

build-linux: $(BUILD_DIR) update-version
	@echo "Building for Linux amd64..."
	@GOOS=linux GOARCH=amd64 go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_linux_amd64 $(CMD_PATH)
	@$(MAKE) compress-binary FILE=$(BUILD_DIR)/$(BINARY_NAME)_linux_amd64 OS=linux
	@echo "Building for Linux arm64..."
	@GOOS=linux GOARCH=arm64 go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_linux_arm64 $(CMD_PATH)
	@$(MAKE) compress-binary FILE=$(BUILD_DIR)/$(BINARY_NAME)_linux_arm64 OS=linux
	@echo "Linux builds complete"

build-windows: $(BUILD_DIR) update-version
	@echo "Building for Windows amd64..."
	@GOOS=windows GOARCH=amd64 go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_windows_amd64.exe $(CMD_PATH)
	@$(MAKE) compress-binary FILE=$(BUILD_DIR)/$(BINARY_NAME)_windows_amd64.exe OS=windows ARCH=amd64
	@echo "Building for Windows arm64..."
	@GOOS=windows GOARCH=arm64 go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_windows_arm64.exe $(CMD_PATH)
	@$(MAKE) compress-binary FILE=$(BUILD_DIR)/$(BINARY_NAME)_windows_arm64.exe OS=windows ARCH=arm64
	@echo "Windows builds complete"

build-darwin: $(BUILD_DIR) update-version
	@echo "Building for macOS amd64..."
	@GOOS=darwin GOARCH=amd64 go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_darwin_amd64 $(CMD_PATH)
	@$(MAKE) compress-binary FILE=$(BUILD_DIR)/$(BINARY_NAME)_darwin_amd64 OS=darwin ARCH=amd64
	@echo "Building for macOS arm64..."
	@GOOS=darwin GOARCH=arm64 go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_darwin_arm64 $(CMD_PATH)
	@$(MAKE) compress-macos-arm64 FILE=$(BUILD_DIR)/$(BINARY_NAME)_darwin_arm64
	@echo "macOS builds complete"
	@if [ "$$(uname)" = "Darwin" ] && [ -d "/Users/yihan/Documents/owner/wendang/home" ]; then \
		echo "Copying macOS ARM64 to /Users/yihan/Documents/owner/wendang/home/yinstall..."; \
		cp $(BUILD_DIR)/$(BINARY_NAME)_darwin_arm64 /Users/yihan/Documents/owner/wendang/home/yinstall; \
		echo "Copy complete"; \
	fi

compress-binary:
	@if command -v upx >/dev/null 2>&1; then \
		echo "  Compressing $(FILE) with UPX..."; \
		if [ "$(OS)" = "darwin" ] && [ "$(ARCH)" = "amd64" ]; then \
			codesign --remove-signature "$(FILE)" 2>/dev/null || true; \
			upx --best --lzma --force-macos "$(FILE)" >/dev/null 2>&1 && \
			codesign -s - "$(FILE)" >/dev/null 2>&1 && \
			echo "  ✓ Compressed and signed" || echo "  ⚠ Compression failed"; \
		elif [ "$(OS)" = "windows" ] && [ "$(ARCH)" = "arm64" ]; then \
			echo "  ⚠ UPX not supported for Windows ARM64"; \
		else \
			upx --best --lzma "$(FILE)" >/dev/null 2>&1 && \
			echo "  ✓ Compressed" || echo "  ⚠ Compression failed"; \
		fi \
	fi

compress-macos-arm64:
	@echo "  Compressing macOS ARM64 with gzip wrapper..."
	@if command -v gzip >/dev/null 2>&1; then \
		TEMP_DIR=$$(mktemp -d); \
		gzip -9 -c "$(FILE)" > "$$TEMP_DIR/payload.gz"; \
		echo '#!/bin/bash' > "$$TEMP_DIR/wrapper.sh"; \
		echo 'PAYLOAD_LINE=$$(awk '"'"'/^__PAYLOAD_BEGIN__/ {print NR + 1; exit 0; }'"'"' "$$0")' >> "$$TEMP_DIR/wrapper.sh"; \
		echo 'TEMP_BIN=$$(mktemp)' >> "$$TEMP_DIR/wrapper.sh"; \
		echo 'tail -n +$${PAYLOAD_LINE} "$$0" | gunzip > "$$TEMP_BIN"' >> "$$TEMP_DIR/wrapper.sh"; \
		echo 'chmod +x "$$TEMP_BIN"' >> "$$TEMP_DIR/wrapper.sh"; \
		echo '"$$TEMP_BIN" "$$@"' >> "$$TEMP_DIR/wrapper.sh"; \
		echo 'EXIT_CODE=$$?' >> "$$TEMP_DIR/wrapper.sh"; \
		echo 'rm -f "$$TEMP_BIN"' >> "$$TEMP_DIR/wrapper.sh"; \
		echo 'exit $$EXIT_CODE' >> "$$TEMP_DIR/wrapper.sh"; \
		echo '__PAYLOAD_BEGIN__' >> "$$TEMP_DIR/wrapper.sh"; \
		cat "$$TEMP_DIR/wrapper.sh" "$$TEMP_DIR/payload.gz" > "$(FILE).new"; \
		chmod +x "$(FILE).new"; \
		codesign -s - "$(FILE).new" >/dev/null 2>&1 || true; \
		SIZE_BEFORE=$$(stat -f%z "$(FILE)" 2>/dev/null); \
		SIZE_AFTER=$$(stat -f%z "$(FILE).new" 2>/dev/null); \
		if [ $$SIZE_AFTER -lt $$SIZE_BEFORE ]; then \
			mv "$(FILE).new" "$(FILE)"; \
			echo "  ✓ Compressed with gzip wrapper"; \
		else \
			rm -f "$(FILE).new"; \
			echo "  ⚠ Gzip wrapper larger, keeping original"; \
		fi; \
		rm -rf "$$TEMP_DIR"; \
	else \
		echo "  ⚠ gzip not available"; \
	fi

build-current: $(BUILD_DIR) update-version
	@echo "Building for current platform..."
	@go build $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"
