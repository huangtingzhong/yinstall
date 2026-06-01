#!/bin/bash
# Swap space usage and swappiness
echo "=== swapon --show ==="
swapon --show 2>/dev/null || echo "(no swap or swapon not available)"

echo ""
echo "=== /proc/meminfo (Swap) ==="
grep -i swap /proc/meminfo 2>/dev/null || echo "(not available)"

echo ""
echo "=== vm.swappiness ==="
sysctl vm.swappiness 2>/dev/null || echo "(not set)"
