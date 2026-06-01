#!/bin/bash
echo "=== mpstat (all CPUs, 1s x 2 samples) ==="
mpstat -P ALL 1 2 2>/dev/null || echo "mpstat not available"
