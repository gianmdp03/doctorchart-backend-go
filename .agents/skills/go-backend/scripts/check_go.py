#!/usr/bin/env python3
"""Non-destructive Go module quality checks. Python 3.9+; Go 1.27.3 target.

Does not change sources, install tools, start databases, or contact production.
Optional govulncheck may use its own vulnerability database network access.
"""
import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

SKIP_DIRS = {'.git', '.hg', '.svn', '.agents', '.agent', 'vendor', 'node_modules', 'testdata', '.idea'}


def run(cmd, cwd, timeout, env=None):
    print('> ' + ' '.join(cmd), flush=True)
    try:
        r = subprocess.run(cmd, cwd=cwd, env=env, text=True, stdout=subprocess.PIPE,
                           stderr=subprocess.STDOUT, timeout=timeout, check=False)
        if r.stdout:
            print(r.stdout[-12000:].rstrip(), flush=True)
        return r.returncode == 0, 'PASS' if r.returncode == 0 else 'FAIL'
    except subprocess.TimeoutExpired:
        print(f'  Timed out after {timeout}s', flush=True)
        return False, 'BLOCKED'
    except OSError as exc:
        print(f'  Unable to execute: {exc}', flush=True)
        return False, 'BLOCKED'


def go_files(root):
    files = []
    for base, dirs, names in os.walk(root):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS and not d.startswith('.')]
        files.extend(str(Path(base) / n) for n in names if n.endswith('.go'))
    return sorted(files)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('module_root', nargs='?', default='.')
    parser.add_argument('--target', default='go1.27.3', help='Expected exact Go toolchain (default: go1.27.3)')
    parser.add_argument('--skip-race', action='store_true', help='Explicitly skip race test; report NOT RUN')
    parser.add_argument('--require-vulncheck', action='store_true', help='Fail if govulncheck is unavailable')
    parser.add_argument('--timeout', type=int, default=600, help='Timeout per command in seconds')
    args = parser.parse_args()
    root = Path(args.module_root).expanduser().resolve()
    if not root.is_dir() or not (root / 'go.mod').is_file():
        parser.error(f'{root} is not a Go module directory containing go.mod')
    if args.timeout < 1:
        parser.error('--timeout must be positive')

    results = []
    go = shutil.which('go')
    if not go:
        print('BLOCKED: Go binary is not on PATH; nothing tested.')
        return 2
    try:
        version_result = subprocess.run([go, 'version'], cwd=root, stdout=subprocess.PIPE,
                                        stderr=subprocess.STDOUT, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f'BLOCKED: cannot inspect Go toolchain: {exc}')
        return 2
    print(version_result.stdout.strip())
    match = re.search(r'\b(go\d+\.\d+(?:\.\d+)?)\b', version_result.stdout)
    version_ok = version_result.returncode == 0 and bool(match) and match.group(1) == args.target
    results.append(('Go toolchain exact version', 'PASS' if version_ok else 'FAIL'))
    if not version_ok:
        print(f'Expected {args.target}; verify toolchain and record approved newer patch if applicable.')

    fmt = shutil.which('gofmt')
    files = go_files(root)
    if not fmt:
        results.append(('Go formatting', 'BLOCKED'))
    elif not files:
        results.append(('Go formatting', 'BLOCKED'))
        print('No Go files detected')
    else:
        print(f'> gofmt -l on {len(files)} source files')
        bad = []
        for f in files:
            p = subprocess.run([fmt, '-l', f], cwd=root, capture_output=True, text=True)
            if p.returncode != 0 or p.stdout.strip():
                bad.append(f)
        if bad:
            print('Unformatted/invalid Go files:\n' + '\n'.join(bad[:100]))
        results.append(('Go formatting', 'PASS' if not bad else 'FAIL'))

    for name, arguments in [
        ('Go build', ['build', './...']),
        ('Go vet', ['vet', './...']),
        ('Go tests', ['test', './...']),
    ]:
        _, status = run([go] + arguments, root, args.timeout)
        results.append((name, status))
    if args.skip_race:
        print('Race detector explicitly skipped (NOT RUN).')
        results.append(('Go race tests', 'NOT RUN'))
    else:
        _, status = run([go, 'test', '-race', './...'], root, args.timeout)
        results.append(('Go race tests', status))
    vuln = shutil.which('govulncheck')
    if vuln:
        _, status = run([vuln, './...'], root, args.timeout)
        results.append(('Go vulnerability scan', status))
    else:
        status = 'BLOCKED' if args.require_vulncheck else 'NOT RUN'
        print(f'govulncheck unavailable ({status}); not a security pass.')
        results.append(('Go vulnerability scan', status))

    print('\n=== QUALITY CHECK RESULTS ===')
    for name, status in results:
        print(f'{status:8s} {name}')
    mandatory = results[:-1] if not args.require_vulncheck else results
    mandatory_ok = all(s == 'PASS' for _, s in mandatory if s != 'NOT RUN')
    return 0 if mandatory_ok else 1


if __name__ == '__main__':
    sys.exit(main())
