#!/bin/bash
# Disk I/O statistics snapshot
echo "=== iostat (1 sample) ==="
iostat -xk 1 1 2>/dev/null || echo "(iostat not available)"

echo ""
echo "=== /proc/diskstats ==="
cat /proc/diskstats 2>/dev/null | awk '{print $3, $4, $6, $8, $10}' | head -40 || echo "(not available)"
