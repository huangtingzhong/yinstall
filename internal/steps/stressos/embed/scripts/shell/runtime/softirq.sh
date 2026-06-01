#!/bin/bash
echo "=== /proc/softirqs ==="
cat /proc/softirqs 2>/dev/null || echo "/proc/softirqs not available"
echo ""
echo "=== /proc/net/softnet_stat (formatted) ==="
if [ -f /proc/net/softnet_stat ]; then
  awk 'NR==1{print "CPU total dropped squeezed throttled"}
       {printf "cpu%d %s %s %s %s\n", NR-1, $1, $2, $3, $9}' /proc/net/softnet_stat 2>/dev/null
else
  echo "/proc/net/softnet_stat not available"
fi
