#!/usr/bin/env bash
# Static check: install-domain steps should not use inline multi-line shell via ExecuteWithCheck.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DOMAINS=(internal/steps/os internal/steps/db internal/steps/standby internal/steps/ycm internal/steps/ymp)
fail=0

for dir in "${DOMAINS[@]}"; do
  while IFS= read -r -d '' f; do
    [[ "$(basename "$f")" == *"_test.go" ]] && continue
    if grep -q 'debug-ok: inline-script' "$f" 2>/dev/null; then
      continue
    fi
    if grep -q 'ExecuteWithCheck(script' "$f" 2>/dev/null; then
      echo "FAIL: $f uses ExecuteWithCheck(script...) without RunShellScript"
      fail=1
    fi
    if grep -q 'ExecuteWithCheck(`set -e' "$f" 2>/dev/null; then
      echo "FAIL: $f uses inline set -e script via ExecuteWithCheck"
      fail=1
    fi
  done < <(find "$ROOT/$dir" -maxdepth 1 -name '*.go' -print0)
done

if [[ $fail -ne 0 ]]; then
  exit 1
fi
echo "check-debug-logging: OK"
