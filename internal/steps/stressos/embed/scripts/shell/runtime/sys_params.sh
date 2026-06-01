#!/bin/bash
echo "=== CPU frequency governor ==="
cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null || echo "not available"

echo ""
echo "=== IO scheduler (first block device) ==="
for dev in /sys/block/*/queue/scheduler; do
  [ -f "$dev" ] && echo "$dev: $(cat $dev)" && break
done

echo ""
echo "=== Transparent Huge Pages ==="
cat /sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null || echo "not available"

echo ""
echo "=== NUMA topology ==="
numactl --hardware 2>/dev/null || echo "numactl not available"

echo ""
echo "=== CPU topology ==="
lscpu 2>/dev/null || echo "lscpu not available"

echo ""
echo "=== NIC settings (first interface, best-effort) ==="
IFACE=$(ip route show default 2>/dev/null | awk '/dev/{print $5; exit}')
if [ -n "$IFACE" ]; then
  echo "Interface: $IFACE"
  ethtool -l "$IFACE" 2>/dev/null || echo "ethtool -l not available"
  ethtool -g "$IFACE" 2>/dev/null || echo "ethtool -g not available"
fi
