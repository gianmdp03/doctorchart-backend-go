#!/usr/bin/env python3
"""Conservative local helper for go-security-audit. No installs, active scans or source edits."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid

TOOLS = ("go", "govulncheck", "semgrep", "gitleaks", "trivy", "docker", "zap.sh")
MARKER = ".go-security-audit-owned"


def tool_path(name):
    return shutil.which(name)


def preflight(repo):
    if not repo.is_dir():
        raise ValueError("Repository directory does not exist")
    print(json.dumps({"repo": str(repo), "platform": sys.platform,
                      "tools": {t: tool_path(t) for t in TOOLS}}, indent=2))


def run_command(argv, cwd, env, output, timeout=300):
    try:
        with output.open("w", encoding="utf-8") as handle:
            proc = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                  stdout=handle, stderr=subprocess.STDOUT,
                                  timeout=timeout, check=False)
        return {"command": argv, "exit_code": proc.returncode, "log": str(output)}
    except subprocess.TimeoutExpired:
        return {"command": argv, "status": "TIMEOUT", "log": str(output)}
    except OSError as exc:
        return {"command": argv, "status": "ERROR", "reason": str(exc)}


def run_static(repo, out):
    if not repo.is_dir() or not (repo / "go.mod").is_file():
        raise ValueError("Expected a Go repository containing go.mod")
    if out.resolve() == repo or repo in out.resolve().parents:
        raise ValueError("Report directory must be outside the repository")
    out.mkdir(parents=True, exist_ok=True)
    # Protect reports on POSIX (may include scanner snippets).
    if os.name == "posix":
        out.chmod(0o700)
    temp = Path(tempfile.mkdtemp(prefix="go-security-audit-", dir=None)).resolve()
    run_id = str(uuid.uuid4())
    (temp / MARKER).write_text(run_id, encoding="utf-8")
    manifest = out / "run-manifest.json"
    manifest.write_text(json.dumps({"run_id": run_id, "temp_dir": str(temp),
                                    "marker": MARKER, "created_at": dt.datetime.now(dt.timezone.utc).isoformat()}, indent=2), encoding="utf-8")
    env = os.environ.copy()
    env.update({"TMPDIR": str(temp), "TMP": str(temp), "TEMP": str(temp),
                "GOCACHE": str(temp / "gocache"), "GOMODCACHE": str(temp / "gomodcache"),
                "GOPATH": str(temp / "gopath"), "XDG_CACHE_HOME": str(temp / "cache"),
                "TRIVY_CACHE_DIR": str(temp / "trivy")})
    commands = [
        ("go-vet", "go", ["go", "vet", "./..."]),
        ("govulncheck", "govulncheck", ["govulncheck", "./..."]),
        ("semgrep", "semgrep", ["semgrep", "scan", "--config", "auto", "--metrics", "off", "--no-git-ignore", "."]),
        ("gitleaks", "gitleaks", ["gitleaks", "git", "--redact", "--no-banner", "--exit-code", "1"]),
        ("trivy", "trivy", ["trivy", "fs", "--scanners", "vuln,misconfig,secret", "--no-progress", "."]),
    ]
    results = []
    try:
        for label, tool, argv in commands:
            if not tool_path(tool):
                results.append({"tool": label, "status": "NOT_TESTED", "reason": "not installed"})
                continue
            # Network-dependent tools must be separately authorized by operator.
            if label in ("govulncheck", "semgrep", "trivy") and not args_allow_network:
                results.append({"tool": label, "status": "NOT_TESTED", "reason": "network/cache authorization not supplied"})
                continue
            results.append({"tool": label, **run_command(argv, repo, env, out / (label + ".log"))})
    finally:
        (out / "static-results.json").write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(f"Wrote results: {out / 'static-results.json'}")
    print(f"Owned temp directory: {temp}")
    print(f"Cleanup: {sys.executable} {Path(__file__).resolve()} cleanup --manifest {manifest}")


def cleanup(manifest):
    obj = json.loads(manifest.read_text(encoding="utf-8"))
    path = Path(obj["temp_dir"]).resolve(strict=True)
    marker = path / MARKER
    if (not path.name.startswith("go-security-audit-") or not marker.is_file()
            or marker.read_text(encoding="utf-8") != obj["run_id"]
            or path.is_symlink() or path == Path(tempfile.gettempdir()).resolve()
            or path.parent != Path(tempfile.gettempdir()).resolve()):
        raise ValueError("Ownership validation failed; refusing cleanup")
    # Refuse symlinks in the owned tree to avoid deleting through linked directories.
    for root, dirs, files in os.walk(path, followlinks=False):
        for item in dirs + files:
            if (Path(root) / item).is_symlink():
                raise ValueError("Symlink found in temp tree; refusing cleanup")
    shutil.rmtree(path)
    print(f"Removed owned temporary directory: {path}")
    print("Manifest and reports retained. No repository files changed by cleanup.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    p = sub.add_parser("preflight")
    p.add_argument("--repo", required=True, type=Path)
    p = sub.add_parser("run-static")
    p.add_argument("--repo", required=True, type=Path)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--allow-network", action="store_true", help="Operator approved network-dependent scanners")
    p = sub.add_parser("cleanup")
    p.add_argument("--manifest", required=True, type=Path)
    args = parser.parse_args()
    args_allow_network = getattr(args, "allow_network", False)
    try:
        if args.action == "preflight":
            preflight(args.repo.resolve())
        elif args.action == "run-static":
            run_static(args.repo.resolve(), args.out.resolve())
        else:
            cleanup(args.manifest.resolve())
    except (ValueError, OSError, KeyError, json.JSONDecodeError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        sys.exit(2)
