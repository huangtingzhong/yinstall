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
    level: str
    message: str
    details: str = ""


def pass_(m: str, d: str = "") -> CheckResult:
    return CheckResult(True, "PASS", m, d)


def warn(m: str, d: str = "") -> CheckResult:
    return CheckResult(True, "WARN", m, d)


def fail(m: str, d: str = "") -> CheckResult:
    return CheckResult(False, "FAIL", m, d)


def require_ssh() -> Optional[str]:
    return shutil.which("ssh")


def run(cmd: list[str], timeout_sec: int) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout_sec, check=False)


def ssh_run(host: str, user: str, port: int, key_path: str, connect_timeout: int, remote_cmd: str, timeout_sec: int) -> subprocess.CompletedProcess[str]:
    ssh_cmd = [
        "ssh",
        "-o",
        "BatchMode=yes",
        "-o",
        "StrictHostKeyChecking=accept-new",
        "-o",
        f"ConnectTimeout={connect_timeout}",
        "-p",
        str(port),
    ]
    if key_path.strip():
        ssh_cmd += ["-i", key_path]
    remote = f"bash -lc {shlex.quote(remote_cmd)}"
    ssh_cmd += [f"{user}@{host}", remote]
    return run(ssh_cmd, timeout_sec=timeout_sec)


def default_local_dirs() -> list[str]:
    dirs = ["./software", "./pkg"]
    dl = os.path.join(os.path.expanduser("~"), "Downloads", "yashan")
    if os.path.isdir(dl):
        dirs.append(dl)
    return dirs


def main() -> int:
    ap = argparse.ArgumentParser(description="yinstall-agent YCM precheck via SSH (read-only).")
    ap.add_argument("--host", required=True)
    ap.add_argument("--ssh-user", default="root")
    ap.add_argument("--ssh-port", type=int, default=22)
    ap.add_argument("--ssh-key-path", default="")
    ap.add_argument("--sudo", default="true", choices=["true", "false"])
    ap.add_argument("--timeout-sec", type=int, default=8)
    ap.add_argument("--connect-timeout-sec", type=int, default=5)
    ap.add_argument("--local-software-dirs", default="", help="Comma-separated; default ./software,./pkg,~/Downloads/yashan(if exists)")
    ap.add_argument("--remote-software-dir", default="/data/yashan/soft")
    ap.add_argument("--ycm-port", type=int, default=9060)
    ap.add_argument("--ycm-prometheus-port", type=int, default=9061)
    ap.add_argument("--ycm-loki-http-port", type=int, default=9062)
    ap.add_argument("--ycm-loki-grpc-port", type=int, default=9063)
    ap.add_argument("--ycm-yasdb-exporter-port", type=int, default=9064)
    args = ap.parse_args()

    if not require_ssh():
        print("FAIL: ssh command not found on this machine.", file=sys.stderr)
        return 2

    local_dirs = [d.strip() for d in args.local_software_dirs.split(",") if d.strip()] if args.local_software_dirs.strip() else default_local_dirs()
    remote_dir = (args.remote_software_dir or "").strip() or "/data/yashan/soft"

    results: list[CheckResult] = []
    print("== yinstall-agent precheck (ycm) ==")
    print(f"host={args.host} ssh_user={args.ssh_user} ssh_port={args.ssh_port} sudo={args.sudo}")

    cp = ssh_run(args.host, args.ssh_user, args.ssh_port, args.ssh_key_path, args.connect_timeout_sec, "echo ok", args.timeout_sec)
    if cp.returncode != 0:
        results.append(fail("SSH connectivity (cannot connect/authenticate)", (cp.stderr or cp.stdout or "").strip()))
        for r in results:
            print(f"{r.level}: {r.message}")
            if r.details:
                print("  " + r.details)
        print("== PRECHECK RESULT: FAIL ==")
        return 1
    results.append(pass_("SSH connectivity"))

    # Basic commands
    cp = ssh_run(args.host, args.ssh_user, args.ssh_port, args.ssh_key_path, args.connect_timeout_sec, "command -v ss >/dev/null 2>&1 || command -v netstat >/dev/null 2>&1", args.timeout_sec)
    results.append(pass_("ss/netstat present") if cp.returncode == 0 else fail("ss/netstat missing (cannot reliably check ports)"))

    # Ports availability (5 ports)
    ports = [
        ("ycm", args.ycm_port),
        ("prometheus", args.ycm_prometheus_port),
        ("loki-http", args.ycm_loki_http_port),
        ("loki-grpc", args.ycm_loki_grpc_port),
        ("yasdb-exporter", args.ycm_yasdb_exporter_port),
    ]
    for name, p in ports:
        cmd = f"ss -tuln 2>/dev/null | grep -E ':{p}([^0-9]|$)' || netstat -tlnp 2>/dev/null | grep -E ':{p}([^0-9]|$)' || true"
        cp = ssh_run(args.host, args.ssh_user, args.ssh_port, args.ssh_key_path, args.connect_timeout_sec, cmd, args.timeout_sec)
        out = (cp.stdout or "").strip()
        results.append(pass_(f"port {p} available ({name})") if out == "" else fail(f"port {p} in use ({name})", out))

    # Package availability (YCM) - mimic FindLatestYCMPackage (linux arch-aware)
    cp = ssh_run(args.host, args.ssh_user, args.ssh_port, args.ssh_key_path, args.connect_timeout_sec, "uname -m", args.timeout_sec)
    remote_arch = (cp.stdout or "").strip()
    arch_re = r"(?:x86_64|x86-64)"
    if remote_arch in ("aarch64", "arm64"):
        arch_re = r"(?:aarch64|aarch-64)"
    pkg_re = re.compile(rf"^yashandb-cloud-manager-(\d+\.\d+\.\d+\.\d+)-linux-{arch_re}\.tar\.gz$")

    cp = ssh_run(args.host, args.ssh_user, args.ssh_port, args.ssh_key_path, args.connect_timeout_sec, "echo $HOME", args.timeout_sec)
    remote_home = (cp.stdout or "").strip()
    remote_dirs = []
    for d in [remote_dir, remote_home]:
        if d and d not in remote_dirs:
            remote_dirs.append(d)

    remote_hits: list[str] = []
    for d in remote_dirs:
        ls_cmd = f"ls -1 {shlex.quote(d)}/yashandb-cloud-manager-*-linux-*.tar.gz 2>/dev/null || true"
        cp = ssh_run(args.host, args.ssh_user, args.ssh_port, args.ssh_key_path, args.connect_timeout_sec, ls_cmd, args.timeout_sec)
        for line in (cp.stdout or "").splitlines():
            base = os.path.basename(line.strip())
            if base and pkg_re.match(base):
                remote_hits.append(line.strip())

    local_hits: list[str] = []
    if not remote_hits:
        for d in local_dirs:
            dd = os.path.expanduser(d)
            for m in glob.glob(os.path.join(dd, "yashandb-cloud-manager-*-linux-*.tar.gz")):
                if pkg_re.match(os.path.basename(m)):
                    local_hits.append(m)

    if remote_hits or local_hits:
        if remote_hits:
            results.append(pass_(f"YCM package found on remote ({len(remote_hits)} candidate(s))", "\n".join(remote_hits[:5])))
        else:
            results.append(pass_(f"YCM package found locally ({len(local_hits)} candidate(s))", "\n".join(local_hits[:5])))
    else:
        results.append(fail("YCM package not found (auto-discovery)", f"searched remote dirs={remote_dirs}, local dirs={local_dirs}, arch={remote_arch or 'unknown'}"))

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

