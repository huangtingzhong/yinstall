#!/bin/bash
# yasboot binary info and monit status
echo "=== yasboot banner (version) ==="
yasboot 2>&1 | grep -E "version|Version" | head -3

echo ""
echo "=== yasboot monit status ==="
yasboot monit status 2>/dev/null || echo "(yasboot monit status not available)"

echo ""
echo "=== yasboot monit summary ==="
yasboot monit summary 2>/dev/null || echo "(yasboot monit summary not available)"
