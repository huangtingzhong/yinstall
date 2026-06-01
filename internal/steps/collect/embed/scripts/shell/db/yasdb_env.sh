#!/bin/bash
# YashanDB environment variables (after sourcing env file)
echo "=== YASDB_HOME ==="
echo "${YASDB_HOME:-(not set)}"

echo ""
echo "=== YASDB_DATA ==="
echo "${YASDB_DATA:-(not set)}"

echo ""
echo "=== PATH contains yasdb ==="
echo "$PATH" | tr ':' '\n' | grep -i yas || echo "(no yasdb path entries)"

echo ""
echo "=== yasdb binary ==="
which yasdb 2>/dev/null || echo "(yasdb not in PATH)"
yasdb --version 2>/dev/null || true
