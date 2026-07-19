#!/usr/bin/env bash
# yinstall test suite entry (layout check + Go unit tests + optional shell cases).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODE="${1:-default}"

run_layout() {
	make check-test-layout
}

run_go_unit() {
	go test ./test/go/... -count=1
}

run_smoke() {
	local c
	shopt -s nullglob
	for c in test/cases/*.sh; do
		echo "=== case: $(basename "$c") ==="
		bash "$c"
	done
	shopt -u nullglob
}

case "$MODE" in
	default|all)
		run_layout
		run_go_unit
		if compgen -G "test/cases/*.sh" >/dev/null; then
			run_smoke
		fi
		;;
	layout)
		run_layout
		;;
	go|unit)
		run_layout
		run_go_unit
		;;
	smoke)
		run_smoke
		;;
	*)
		echo "usage: $0 [default|all|layout|go|smoke]" >&2
		exit 2
		;;
esac

echo "run_all.sh ($MODE): OK"
