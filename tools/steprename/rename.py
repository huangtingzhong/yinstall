#!/usr/bin/env python3
"""Rename step files to {domain}_{slug}.go and step factory functions to stepCamelCase."""
import os
import re
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

DOMAINS = [
    ("internal/steps/db", "db", re.compile(r"^c(\d+[a-z]?)_(.+)\.go$")),
    ("internal/steps/os", "os", re.compile(r"^b(\d+)_(.+)\.go$")),
    ("internal/steps/standby", "standby", re.compile(r"^e(\d+)_(.+)\.go$")),
    ("internal/steps/ycm", "ycm", re.compile(r"^g(\d+)_(.+)\.go$")),
    ("internal/steps/ymp", "ymp", re.compile(r"^h(\d+)_(.+)\.go$")),
    ("internal/steps/collect", "collect", re.compile(r"^r(\d+)_(.+)\.go$")),
    ("internal/steps/stressos", "stressos", re.compile(r"^s(\d+)_(.+)\.go$")),
    ("internal/steps/mysql", "mysql", re.compile(r"^m(\d+[a-z]?)_(.+)\.go$")),
    ("internal/steps/mysql_standby", "mysql_standby", re.compile(r"^mr(\d+)_(.+)\.go$")),
    ("internal/steps/mssql", "mssql", re.compile(r"^ms(\d+[a-z]?)_(.+)\.go$")),
    ("internal/steps/mssql_ag", "mssql_ag", re.compile(r"^a(\d+)_(.+)\.go$")),
    ("internal/steps/mssql_mirror", "mssql_mirror", re.compile(r"^m(\d+)_(.+)\.go$")),
    ("internal/steps/win_os", "win_os", re.compile(r"^w(\d+)_(.+)\.go$")),
]

SKIP = {
    "registry.go", "debug_log.go", "categories.go", "rule_engine.go",
    "install_summary.go", "collect_util.go", "stress_util.go", "mirror_infra.go",
    "mirror_discovery.go", "status_report.go", "summary.go",
}

EXPORT_ALIASES = {
    ("internal/steps/os", "stepSetHostname"): "StepSetHostname",
    ("internal/steps/os", "stepCheckConnectivity"): "StepCheckConnectivity",
}


def domain_filename(domain: str, slug: str) -> str:
    """Build {domain}_{slug}.go, avoiding redundant prefixes (e.g. win_os_os_*)."""
    if domain == "win_os" and slug.startswith("os_"):
        slug = slug[3:]
    return f"{domain}_{slug}.go"


def move_file(old_path: str, new_path: str):
    if os.path.abspath(old_path) == os.path.abspath(new_path):
        return
    if os.path.exists(new_path):
        raise FileExistsError(f"target already exists: {new_path}")
    try:
        subprocess.run(["git", "mv", old_path, new_path], check=True, cwd=ROOT, capture_output=True)
    except subprocess.CalledProcessError:
        shutil.move(old_path, new_path)


def slug_to_func(slug: str) -> str:
    parts = slug.split("_")
    return "step" + "".join(p[:1].upper() + p[1:] for p in parts if p)


def go_replace_all(old: str, new: str):
    if old == new:
        return
    for dirpath, _, files in os.walk(os.path.join(ROOT, "internal")):
        for fn in files:
            if not fn.endswith(".go"):
                continue
            path = os.path.join(dirpath, fn)
            with open(path) as f:
                c = f.read()
            if old not in c:
                continue
            c = c.replace(old, new)
            with open(path, "w") as f:
                f.write(c)
    cmd_path = os.path.join(ROOT, "cmd")
    if os.path.isdir(cmd_path):
        for dirpath, _, files in os.walk(cmd_path):
            for fn in files:
                if fn.endswith(".go"):
                    path = os.path.join(dirpath, fn)
                    with open(path) as f:
                        c = f.read()
                    if old in c:
                        with open(path, "w") as f:
                            f.write(c.replace(old, new))


def main():
    dry = os.environ.get("RENAME_EXECUTE", "") != "1"
    renames = []

    for domain_dir, domain, pat in DOMAINS:
        full = os.path.join(ROOT, domain_dir)
        if not os.path.isdir(full):
            continue
        for fn in sorted(os.listdir(full)):
            if fn in SKIP or not fn.endswith(".go"):
                continue
            m = pat.match(fn)
            if not m:
                # non-numbered support files: prefix with domain_
                if fn.startswith(domain + "_"):
                    continue
                if re.match(r"^[a-z_]+\.go$", fn) and not re.match(r"^[a-z]\d", fn):
                    new_fn = f"{domain}_{fn}"
                    if new_fn != fn:
                        renames.append((os.path.join(full, fn), os.path.join(full, new_fn), None, None))
                continue
            slug = m.group(2)
            new_fn = domain_filename(domain, slug)
            old_path = os.path.join(full, fn)
            new_path = os.path.join(full, new_fn)
            if old_path == new_path:
                continue

            with open(old_path) as f:
                content = f.read()
            fm = re.search(r"func (Step[A-Za-z0-9]+)\(\)", content)
            if not fm:
                print(f"skip (no factory): {fn}", file=sys.stderr)
                continue
            old_func = fm.group(1)
            new_func = slug_to_func(slug if not (domain == "win_os" and m.group(2).startswith("os_")) else m.group(2)[3:])
            renames.append((old_path, new_path, old_func, new_func))

    print(f"{'DRY-RUN' if dry else 'EXECUTE'}: {len(renames)} renames")
    for old_path, new_path, old_func, new_func in renames:
        print(f"  {os.path.basename(old_path)} -> {os.path.basename(new_path)}  {old_func} -> {new_func}")
        if dry:
            continue
        move_file(old_path, new_path)
        if old_func and new_func:
            with open(new_path) as f:
                c = f.read()
            c = c.replace(f"func {old_func}(", f"func {new_func}(")
            c = re.sub(rf"\b{old_func}\b", new_func, c)
            with open(new_path, "w") as f:
                f.write(c)
            go_replace_all(old_func, new_func)

    if not dry:
        # exported aliases in os registry
        reg = os.path.join(ROOT, "internal/steps/os/registry.go")
        with open(reg) as f:
            c = f.read()
        if "func StepSetHostname()" not in c and "stepSetHostname" in c:
            extra = "\n// StepSetHostname is exported for cross-package reuse (mysql write hosts).\nfunc StepSetHostname() *runner.Step {\n\treturn stepSetHostname()\n}\n"
            if extra.strip() not in c:
                c = c.rstrip() + "\n" + extra
                with open(reg, "w") as f:
                    f.write(c)
        # fix mysql cross-ref
        go_replace_all("StepB023SetHostname", "StepSetHostname")
        go_replace_all("StepB001CheckConnectivity()", "StepCheckConnectivity()")
        go_replace_all("ossteps.StepB001CheckConnectivity()", "ossteps.StepCheckConnectivity()")

    return 0


if __name__ == "__main__":
    sys.exit(main())
