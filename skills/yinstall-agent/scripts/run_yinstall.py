#!/usr/bin/env python3
from __future__ import annotations

import os
import platform
import subprocess
import sys
from pathlib import Path


def repo_root() -> Path:
    # skills/yinstall-agent/scripts/run_yinstall.py
    return Path(__file__).resolve().parents[3]


def detect_binary_name() -> str:
    plat = sys.platform
    arch = "amd64"
    m = (platform.machine() or "").lower()
    if m in ("aarch64", "arm64"):
        arch = "arm64"
    elif m in ("x86_64", "amd64", "x64"):
        arch = "amd64"

    if plat.startswith("win"):
        return f"yinstall_windows_{arch}.exe"
    if plat == "darwin":
        return f"yinstall_darwin_{arch}"
    return f"yinstall_linux_{arch}"


def yinstall_path() -> Path:
    return repo_root() / "skills" / "yinstall-agent" / "scripts" / "bin" / detect_binary_name()


def main() -> int:
    yinstall = yinstall_path()
    if not yinstall.exists():
        print(f"ERROR: yinstall binary not found for this platform: {yinstall}", file=sys.stderr)
        return 2

    # Ensure we run from repo root so relative defaults (logs/software dirs) are stable.
    try:
        os.chdir(repo_root())
    except Exception:
        pass

    cmd = [str(yinstall), *sys.argv[1:]]
    p = subprocess.run(cmd)
    return p.returncode


if __name__ == "__main__":
    raise SystemExit(main())

