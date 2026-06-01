#!/bin/bash
# NUMA topology and memory distribution
echo "=== numactl --hardware ==="
numactl --hardware 2>/dev/null || echo "(numactl not available)"

echo ""
echo "=== /sys/devices/system/node ==="
if ls /sys/devices/system/node/node* 2>/dev/null | head -1 > /dev/null; then
    for node in /sys/devices/system/node/node*; do
        node_id=$(basename "$node")
        mem_free=$(cat "$node/meminfo" 2>/dev/null | grep MemFree | awk '{print $4, $5}')
        mem_total=$(cat "$node/meminfo" 2>/dev/null | grep MemTotal | awk '{print $4, $5}')
        echo "$node_id: total=$mem_total free=$mem_free"
    done
else
    echo "(NUMA nodes not found, likely single-node)"
fi

echo ""
echo "=== /proc/sys/kernel/numa_balancing ==="
cat /proc/sys/kernel/numa_balancing 2>/dev/null || echo "(not available)"
