#!/usr/bin/env python3
from __future__ import annotations

import argparse
import glob
import os
import re
import shlex
import shutil
import subprocess
import sys
from dataclasses import dataclass
from typing import Optional


@dataclass
class CheckResult:
    ok: bool
    level: str  # PASS/WARN/FAIL
    message: str
    details: str = ""


def eprint(*args: object) -> None:
    print(*args, file=sys.stderr)


def pass_(msg: str, details: str = "") -> CheckResult:
    return CheckResult(True, "PASS", msg, details)


def warn(msg: str, details: str = "") -> CheckResult:
    return CheckResult(True, "WARN", msg, details)


def fail(msg: str, details: str = "") -> CheckResult:
    return CheckResult(False, "FAIL", msg, details)


def require_ssh() -> Optional[str]:
    return shutil.which("ssh")


def run(cmd: list[str], timeout_sec: int) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=timeout_sec,
        check=False,
    )


def ssh_run(
    host: str,
    ssh_user: str,
    ssh_port: int,
    ssh_key_path: str,
    connect_timeout_sec: int,
    remote_cmd: str,
    timeout_sec: int,
) -> subprocess.CompletedProcess[str]:
    # Remote side is expected to be Linux; use bash -lc for consistency with yinstall steps.
    ssh_cmd = [
        "ssh",
        "-o",
        "BatchMode=yes",
        "-o",
        "StrictHostKeyChecking=accept-new",
        "-o",
        f"ConnectTimeout={connect_timeout_sec}",
        "-p",
        str(ssh_port),
    ]
    if ssh_key_path.strip():
        ssh_cmd += ["-i", ssh_key_path]

    # IMPORTANT: ssh treats all remaining args as a single command string joined with spaces.
    # We must ensure the -lc argument is passed as ONE shell-quoted string, otherwise bash
    # will receive an incorrect argv (e.g. "bash -lc for d in ..." -> syntax error).
    remote = f"bash -lc {shlex.quote(remote_cmd)}"
    ssh_cmd += [f"{ssh_user}@{host}", remote]
    return run(ssh_cmd, timeout_sec=timeout_sec)


def main() -> int:
    parser = argparse.ArgumentParser(
        prog="ssh_precheck_db.py",
        description="yinstall-agent DB precheck via SSH (read-only, cross-platform control plane).",
    )
    parser.add_argument("--host", required=True, help="Target host (IP or hostname)")
    parser.add_argument("--ssh-user", default="root", help="SSH user (default: root)")
    parser.add_argument("--ssh-port", type=int, default=22, help="SSH port (default: 22)")
    parser.add_argument(
        "--ssh-key-path",
        default="",
        help="SSH private key path (optional; empty = use ssh default identity resolution)",
    )
    parser.add_argument("--sudo", default="true", choices=["true", "false"], help="Whether sudo is required on target")
    parser.add_argument("--db-port", type=int, default=1688, help="DB begin port to check (default: 1688)")
    parser.add_argument("--timeout-sec", type=int, default=8, help="Command timeout seconds (default: 8)")
    parser.add_argument("--connect-timeout-sec", type=int, default=5, help="SSH connect timeout seconds (default: 5)")
    parser.add_argument("--os-ntp-server", default="", help="Optional NTP server to validate (empty = skip)")
    parser.add_argument(
        "--local-software-dirs",
        default="",
        help="Comma-separated local software directories to search packages (default: ./software,./pkg,~/Downloads/yashan if exists)",
    )
    parser.add_argument(
        "--remote-software-dir",
        default="/data/yashan/soft",
        help="Remote software directory on target host (default: /data/yashan/soft)",
    )
    parser.add_argument(
        "--allow-existing-dirs",
        default="false",
        choices=["true", "false"],
        help="Treat existing default directories as WARN instead of FAIL (use when you plan to run yinstall with --force-steps C-005)",
    )
    args = parser.parse_args()

    ssh_bin = require_ssh()
    if not ssh_bin:
        eprint("FAIL: ssh command not found on this machine.")
        eprint("      Install OpenSSH client (Windows: Optional Features -> OpenSSH Client; macOS/Linux usually preinstalled).")
        return 2

    host = args.host
    ssh_user = args.ssh_user
    ssh_port = args.ssh_port
    ssh_key_path = args.ssh_key_path
    use_sudo = args.sudo == "true"
    db_port = args.db_port
    timeout_sec = args.timeout_sec
    connect_timeout_sec = args.connect_timeout_sec
    ntp = args.os_ntp_server.strip()
    allow_existing_dirs = args.allow_existing_dirs == "true"
    remote_software_dir = (args.remote_software_dir or "").strip() or "/data/yashan/soft"

    def default_local_dirs() -> list[str]:
        dirs = ["./software", "./pkg"]
        home = os.path.expanduser("~")
        dl = os.path.join(home, "Downloads", "yashan")
        if os.path.isdir(dl):
            dirs.append(dl)
        return dirs

    local_dirs = []
    if (args.local_software_dirs or "").strip():
        local_dirs = [d.strip() for d in args.local_software_dirs.split(",") if d.strip()]
    else:
        local_dirs = default_local_dirs()

    results: list[CheckResult] = []

    print("== yinstall-agent precheck (db, standalone) ==")
    print(f"host={host} ssh_user={ssh_user} ssh_port={ssh_port} db_port={db_port} sudo={'true' if use_sudo else 'false'}")

    # A) SSH connectivity
    cp = ssh_run(
        host,
        ssh_user,
        ssh_port,
        ssh_key_path,
        connect_timeout_sec,
        "echo connection_ok",
        timeout_sec,
    )
    ssh_ok = cp.returncode == 0
    if ssh_ok:
        # Some environments may suppress stdout; return code is the most reliable signal.
        results.append(pass_("SSH connectivity"))
    else:
        results.append(
            fail(
                "SSH connectivity (cannot connect/authenticate)",
                details=(cp.stderr or cp.stdout or "").strip(),
            )
        )

    # If SSH is not available, do not run further remote checks (avoid misleading PASS).
    if not ssh_ok:
        results.append(warn("Skipping remaining checks due to SSH failure"))
        ok = False
        for r in results:
            print(f"{r.level}: {r.message}")
            if r.details:
                for line in r.details.splitlines():
                    print(f"  {line}")
            if r.level == "FAIL":
                ok = False
        print("== PRECHECK RESULT: FAIL ==")
        return 1

    # B) sudo capability (optional)
    if use_sudo:
        cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, "sudo -n true", timeout_sec)
        if cp.returncode == 0:
            results.append(pass_("sudo -n true (non-interactive sudo)"))
        else:
            results.append(
                fail(
                    "sudo requested but sudo -n true failed (use --sudo=false or configure sudoers)",
                    details=(cp.stderr or cp.stdout or "").strip(),
                )
            )
    else:
        results.append(pass_("sudo disabled"))

    # C) basic commands
    cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, "command -v systemctl >/dev/null 2>&1", timeout_sec)
    results.append(pass_("systemctl present") if cp.returncode == 0 else warn("systemctl not found (many steps require systemd)"))

    cp = ssh_run(
        host,
        ssh_user,
        ssh_port,
        ssh_key_path,
        connect_timeout_sec,
        "command -v ss >/dev/null 2>&1 || command -v netstat >/dev/null 2>&1",
        timeout_sec,
    )
    results.append(pass_("ss/netstat present") if cp.returncode == 0 else fail("ss/netstat missing (cannot reliably check ports)"))

    # C.1) package availability (DB) - mimic FindLatestDBPackage rules (linux arch-aware)
    cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, "uname -m", timeout_sec)
    remote_arch = (cp.stdout or "").strip()
    arch_re = r"(?:x86_64|x86-64)"
    if remote_arch in ("aarch64", "arm64"):
        arch_re = r"(?:aarch64|aarch-64)"
    pkg_re = re.compile(rf"^yashandb-(\d+\.\d+\.\d+\.\d+)-linux-{arch_re}\.tar\.gz$")

    # remote search dirs: remote_software_dir + $HOME (same as internal/common/file remoteSearchDirs)
    cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, "echo $HOME", timeout_sec)
    remote_home = (cp.stdout or "").strip()
    remote_dirs = []
    seen = set()
    for d in [remote_software_dir, remote_home]:
        if d and d not in seen:
            seen.add(d)
            remote_dirs.append(d)

    remote_hits: list[str] = []
    for d in remote_dirs:
        ls_cmd = f"ls -1 {shlex.quote(d)}/yashandb-*-linux-*.tar.gz 2>/dev/null || true"
        cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, ls_cmd, timeout_sec)
        for line in (cp.stdout or "").splitlines():
            base = os.path.basename(line.strip())
            if base and pkg_re.match(base):
                remote_hits.append(line.strip())

    local_hits: list[str] = []
    if not remote_hits:
        for d in local_dirs:
            dd = os.path.expanduser(d)
            for m in glob.glob(os.path.join(dd, "yashandb-*-linux-*.tar.gz")):
                base = os.path.basename(m)
                if pkg_re.match(base):
                    local_hits.append(m)

    if remote_hits or local_hits:
        if remote_hits:
            results.append(pass_(f"DB package found on remote ({len(remote_hits)} candidate(s))", details="\n".join(remote_hits[:5])))
        else:
            results.append(pass_(f"DB package found locally ({len(local_hits)} candidate(s))", details="\n".join(local_hits[:5])))
    else:
        results.append(
            fail(
                "DB package not found for target arch (auto-discovery). Place package in local-software-dirs or remote-software-dir.",
                details=f"searched remote dirs={remote_dirs}, local dirs={local_dirs}, arch={remote_arch or 'unknown'}",
            )
        )

    # D) port availability
    port_cmd = f"ss -tuln 2>/dev/null | grep -E ':{db_port}([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':{db_port}([^0-9]|$)' || true"
    cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, port_cmd, timeout_sec)
    port_out = (cp.stdout or "").strip()
    if port_out == "":
        results.append(pass_(f"db-port {db_port} available"))
    else:
        results.append(fail(f"db-port {db_port} is in use (choose another --db-port)", details=port_out))

    # E) directory conflicts (default convention in cli/db.go)
    if db_port == 1688:
        home = "/data/yashan/yasdb_home"
        data = "/data/yashan/yasdb_data"
        log = "/data/yashan/log"
    else:
        home = f"/data/yashan/yasdb_home_{db_port}"
        data = f"/data/yashan/yasdb_data_{db_port}"
        log = f"/data/yashan/log_{db_port}"

    # Use stat to distinguish "missing" vs "permission denied" vs "exists".
    # Format: STAT:<path>:<rc>:<message>
    # Note: capture stat rc correctly (avoid pipe overriding $?); then flatten output.
    dir_cmd = (
        f"for d in {home} {data} {log}; do "
        f"out=$(stat \"$d\" 2>&1); rc=$?; "
        f"out=$(printf '%s' \"$out\" | tr '\\n' ' '); "
        f"echo \"STAT:$d:$rc:$out\"; "
        f"done"
    )
    cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, dir_cmd, timeout_sec)
    stat_lines = [ln for ln in (cp.stdout or "").splitlines() if ln.startswith("STAT:")]

    exists: list[str] = []
    noaccess: list[str] = []
    other_err: list[str] = []
    for ln in stat_lines:
        # split into 4 parts max: STAT, path, rc, msg
        parts = ln.split(":", 3)
        if len(parts) < 4:
            continue
        _, pth, rc_s, msg = parts
        try:
            rc = int(rc_s)
        except Exception:
            rc = 1
        m = msg.lower()
        if rc == 0:
            exists.append(pth)
        else:
            if "permission denied" in m:
                noaccess.append(pth)
            elif "no such file" in m or "cannot stat" in m:
                # missing is OK
                pass
            else:
                other_err.append(f"{pth}: {msg}")

    if noaccess:
        results.append(
            fail(
                "cannot access default directories (permission denied); run precheck with correct user or enable sudo for checks",
                details="\n".join(noaccess),
            )
        )
    elif other_err:
        results.append(
            warn(
                "directory stat returned unexpected errors (treat with caution)",
                details="\n".join(other_err),
            )
        )

    if exists:
        detail = "\n".join([f"EXISTS:{p}" for p in exists])
        if allow_existing_dirs:
            results.append(
                warn(
                    "default directories already exist (allowed by --allow-existing-dirs=true; ensure you will use --force-steps C-005 to delete/recreate)",
                    details=detail,
                )
            )
        else:
            results.append(
                fail(
                    "default directories already exist (choose: change --db-port / change paths / use --force-steps C-005 / clean)",
                    details=detail,
                )
            )
    else:
        results.append(pass_("default directories do not exist (ok)"))

    # F) optional NTP checks (best-effort, read-only)
    if ntp:
        cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, f"getent hosts '{ntp}' >/dev/null 2>&1", timeout_sec)
        if cp.returncode == 0:
            results.append(pass_(f"NTP server resolvable: {ntp}"))
        else:
            results.append(fail(f"NTP server domain not resolvable: {ntp}"))

        # UDP/123 reachability check via bash /dev/udp (may fail on restricted shells)
        udp_cmd = f"timeout 3 bash -lc \"echo > /dev/udp/{ntp}/123\" >/dev/null 2>&1"
        cp = ssh_run(host, ssh_user, ssh_port, ssh_key_path, connect_timeout_sec, udp_cmd, timeout_sec)
        if cp.returncode == 0:
            results.append(pass_(f"NTP udp/123 reachable: {ntp}"))
        else:
            results.append(fail(f"NTP udp/123 not reachable: {ntp}"))
    else:
        results.append(pass_("NTP checks skipped (os-ntp-server empty)"))

    ok = True
    for r in results:
        print(f"{r.level}: {r.message}")
        if r.details:
            for line in r.details.splitlines():
                print(f"  {line}")
        if r.level == "FAIL":
            ok = False

    print(f"== PRECHECK RESULT: {'PASS' if ok else 'FAIL'} ==")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())

