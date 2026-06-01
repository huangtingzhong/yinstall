#!/usr/bin/env bash
# 块设备 /dev/nvme0n2 组合矩阵（在测试机 10.10.10.130 上执行 S-07）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
YINSTALL="${ROOT}/build/yinstall"
TARGET="10.10.10.130"
DEV="/dev/nvme0n2"
LDIR="/Users/yihan/Downloads/yashan"
RDIR="/data/yashan/soft"
IO_SIZE="15G"
IO_TIME="25"
OUT_BASE="${HOME}/.yinstall/stress/blkdev-matrix-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT_BASE"
LOG="${OUT_BASE}/matrix.log"

run_case() {
  local id="$1"
  shift
  echo "======== CASE ${id}: $* ========" | tee -a "$LOG"
  local odir="${OUT_BASE}/case-${id}"
  if ! "$YINSTALL" stressos -t "$TARGET" -L "$LDIR" -R "$RDIR" \
    --output "$odir" \
    --cpu=false --mem=false --net=false --install-deps=false \
    -s S-01,S-02,S-07,S-11 \
    --io=true --io-device="$DEV" --danger-io-device \
    --io-size="$IO_SIZE" --io-time="$IO_TIME" \
    "$@" 2>&1 | tee -a "$LOG"; then
    echo "CASE ${id}: FAILED" | tee -a "$LOG"
    return 1
  fi
  echo "CASE ${id}: OK -> $odir" | tee -a "$LOG"
}

# 1) 只读：randread + libaio（安全，先做）
run_case "01-readonly-libaio" \
  --io-device-mode=readonly \
  --io-engine=libaio

# 2) 读写：三场景默认 libaio
run_case "02-readwrite-libaio-all" \
  --io-device-mode=readwrite \
  --io-engine=libaio

# 3) 数据 libaio + redo sync
run_case "03-readwrite-libaio-sync" \
  --io-device-mode=readwrite \
  --io-engine=libaio \
  --io-engine-data=libaio \
  --io-engine-logwrite=sync

# 4) 数据 libaio + redo psync
run_case "04-readwrite-libaio-psync-log" \
  --io-device-mode=readwrite \
  --io-engine=libaio \
  --io-engine-data=libaio \
  --io-engine-logwrite=psync

# 5) 全 psync（兼容/engine 回退）
run_case "05-readwrite-psync-all" \
  --io-device-mode=readwrite \
  --io-engine=psync

# 6) 分场景引擎：randrw/randread libaio，logwrite sync
run_case "06-readwrite-split-engines" \
  --io-device-mode=readwrite \
  --io-engine=libaio \
  --io-engine-randrw=libaio \
  --io-engine-randread=libaio \
  --io-engine-logwrite=sync

# 7) 块大小组合 16k/8k/4k
run_case "07-readwrite-bs-mix" \
  --io-device-mode=readwrite \
  --io-engine=libaio \
  --io-bs-randrw=16k \
  --io-bs-randread=8k \
  --io-bs-logwrite=4k

# 8) direct=0 缓冲 IO（只读，避免缓冲写设备语义混淆）
run_case "08-readonly-buffered" \
  --io-device-mode=readonly \
  --io-engine=libaio \
  --io-direct=0

echo "All cases finished. Summary: $OUT_BASE" | tee -a "$LOG"
