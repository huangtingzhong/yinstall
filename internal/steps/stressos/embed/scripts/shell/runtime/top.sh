#!/bin/bash
echo "=== top (batch, 2 iterations) ==="
top -b -n2 -d1 2>/dev/null || echo "top not available"
