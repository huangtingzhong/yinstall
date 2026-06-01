#!/bin/bash
echo "=== iostat (extended stats, 1s x 4 samples) ==="
iostat -xk 1 4 2>/dev/null || echo "iostat not available"
