#!/bin/bash
echo "=== network interfaces ==="
netstat -i 2>/dev/null || ip -s link 2>/dev/null || echo "netstat/ip not available"
echo ""
echo "=== network statistics ==="
netstat -s 2>/dev/null || ss -s 2>/dev/null || echo "netstat/ss not available"
echo ""
echo "=== active connections (summary) ==="
netstat -an 2>/dev/null | awk '/^tcp/{state[$NF]++} END{for(s in state) print s, state[s]}' || echo "netstat not available"
