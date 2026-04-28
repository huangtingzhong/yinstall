#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import platform
import subprocess
import sys
from pathlib import Path


def run(cmd: list[str]) -> int:
    p = subprocess.run(cmd, text=True)
    return p.returncode


def repo_root() -> Path:
    # this script lives in skills/yinstall-agent/scripts/
    return Path(__file__).resolve().parents[3]

def detect_platform_binary_name() -> str:
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
    # default to linux
    return f"yinstall_linux_{arch}"

def skill_yinstall_path() -> Path:
    root = repo_root()
    name = detect_platform_binary_name()
    return root / "skills" / "yinstall-agent" / "scripts" / "bin" / name


def precheck_cmd(args: argparse.Namespace, allow_existing_dirs: bool) -> list[str]:
    script = repo_root() / "skills" / "yinstall-agent" / "scripts" / "ssh_precheck_db.py"
    cmd = [
        sys.executable,
        str(script),
        "--host",
        args.host,
        "--ssh-user",
        args.ssh_user,
        "--ssh-port",
        str(args.ssh_port),
        "--ssh-key-path",
        args.ssh_key_path or "",
        "--sudo",
        "true" if args.sudo else "false",
        "--db-port",
        str(args.db_port),
        "--timeout-sec",
        str(args.timeout_sec),
        "--connect-timeout-sec",
        str(args.connect_timeout_sec),
        "--os-ntp-server",
        args.os_ntp_server or "",
        "--allow-existing-dirs",
        "true" if allow_existing_dirs else "false",
    ]
    return cmd


def install_cmd(args: argparse.Namespace, extra_flags: list[str]) -> list[str]:
    # Use bundled yinstall binary from skill (cross-platform control plane).
    yinstall = skill_yinstall_path()
    if not yinstall.exists():
        raise FileNotFoundError(f"yinstall binary not found: {yinstall}")
    cmd = [
        str(yinstall),
        "db",
        "--run-id",
        args.run_id,
        "--targets",
        args.host,
        "--ssh-user",
        args.ssh_user,
        "--ssh-auth",
        args.ssh_auth,
        "--sudo=" + ("true" if args.sudo else "false"),
        "--db-port",
        str(args.db_port),
        "--skip-os=" + ("true" if args.skip_os else "false"),
    ]
    if args.db_package:
        cmd += ["--db-package", args.db_package]
    if args.db_sys_password:
        cmd += ["--db-sys-password", args.db_sys_password]
    if args.ssh_auth == "key" and args.ssh_key_path:
        cmd += ["--ssh-key-path", args.ssh_key_path]
    if args.ssh_auth == "password" and args.ssh_password:
        cmd += ["--ssh-password", args.ssh_password]
    cmd += extra_flags
    return cmd


def prompt_choice(prompt: str, valid: set[str]) -> str:
    while True:
        s = input(prompt).strip()
        if s in valid:
            return s


def main() -> int:
    ap = argparse.ArgumentParser(description="Skill runner: SSH precheck then yinstall db apply (interactive on failures).")
    ap.add_argument("--host", required=True)
    ap.add_argument("--run-id", default="")
    ap.add_argument("--ssh-user", default="root")
    ap.add_argument("--ssh-port", type=int, default=22)
    ap.add_argument("--ssh-auth", choices=["key", "password"], default="key")
    ap.add_argument("--ssh-key-path", default="")
    ap.add_argument("--ssh-password", default="")
    ap.add_argument("--sudo", action="store_true", default=False)
    ap.add_argument("--skip-os", default="false", choices=["true", "false"])
    ap.add_argument("--db-port", type=int, default=1688)
    ap.add_argument("--db-package", default="")
    ap.add_argument("--db-sys-password", default="")
    ap.add_argument("--os-ntp-server", default="")
    ap.add_argument("--timeout-sec", type=int, default=8)
    ap.add_argument("--connect-timeout-sec", type=int, default=5)
    args = ap.parse_args()

    args.skip_os = args.skip_os == "true"

    if not args.run_id:
        args.run_id = f"db-apply-{args.host}-p{args.db_port}"

    # Ensure we run from repo root so relative paths (logs/software dirs) behave consistently.
    try:
        os.chdir(repo_root())
    except Exception:
        pass

    # Validate bundled binary exists for current platform
    try:
        _ = skill_yinstall_path()
    except Exception as e:
        print(f"ERROR: failed to resolve skill yinstall binary: {e}")
        return 2
    if not skill_yinstall_path().exists():
        print(f"ERROR: skill yinstall binary not found: {skill_yinstall_path()}")
        return 2

    print("## Step 1: SSH precheck (read-only)")
    rc = run(precheck_cmd(args, allow_existing_dirs=False))
    if rc == 0:
        print("## Precheck PASS -> installing now")
        os_rc = run(install_cmd(args, extra_flags=[]))
        return os_rc

    print("## Precheck FAIL -> choose a solution (will be validated by re-running precheck)")
    print("- 1) Change port and recheck (recommended)")
    print("- 2) Proceed with --force-steps C-005 (dirs may exist; precheck will be relaxed for dirs)")
    print("- 3) Abort")

    choice = prompt_choice("Select (1/2/3): ", {"1", "2", "3"})
    if choice == "3":
        print("Aborted.")
        return 1

    extra_flags: list[str] = []
    allow_dirs = False

    if choice == "1":
        new_port = input("Enter new --db-port: ").strip()
        if not new_port.isdigit():
            print("Invalid port.")
            return 2
        args.db_port = int(new_port)
        args.run_id = f"db-apply-{args.host}-p{args.db_port}"
    elif choice == "2":
        extra_flags = ["--force-steps", "C-005"]
        allow_dirs = True

    print("## Step 2: Validate chosen solution by precheck")
    rc2 = run(precheck_cmd(args, allow_existing_dirs=allow_dirs))
    if rc2 != 0:
        print("## Validation precheck still FAIL. Not installing.")
        return 1

    print("## Validation PASS -> installing now")
    os_rc = run(install_cmd(args, extra_flags=extra_flags))
    return os_rc


if __name__ == "__main__":
    raise SystemExit(main())

