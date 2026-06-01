#!/bin/bash
# HugePages and transparent hugepages configuration
echo "=== /proc/meminfo (HugePages) ==="
grep -i huge /proc/meminfo 2>/dev/null || echo "(not available)"

echo ""
echo "=== Transparent HugePages ==="
if [ -f /sys/kernel/mm/transparent_hugepage/enabled ]; then
    echo "enabled: $(cat /sys/kernel/mm/transparent_hugepage/enabled)"
    echo "defrag:  $(cat /sys/kernel/mm/transparent_hugepage/defrag)"
else
    echo "(transparent_hugepage sysfs not found)"
fi

echo ""
echo "=== vm.nr_hugepages ==="
sysctl vm.nr_hugepages 2>/dev/null || echo "(not set)"
