#!/usr/bin/env python3
"""Compare pre-recorded sanitized Spring/Go contract snapshots, no I/O to services.

Expected root: {"cases": [{"id": "unique-case-id", "request": {...},
"response": {...}, "database": {...}, "side_effects": [...]}, ...]}.
Strict type/shape/value comparison; only HTTP header name casing is normalized.
"""
import argparse
from decimal import Decimal
import json
from pathlib import Path
import sys


class SnapshotError(Exception):
    pass


def read_cases(path):
    try:
        data = json.loads(Path(path).read_text(encoding='utf-8'), parse_float=Decimal)
    except (OSError, json.JSONDecodeError) as exc:
        raise SnapshotError(f'Could not read {path}: {exc}') from exc
    if not isinstance(data, dict) or not isinstance(data.get('cases'), list):
        raise SnapshotError(f'{path}: root must contain a cases array')
    cases = {}
    for case in data['cases']:
        if not isinstance(case, dict) or not isinstance(case.get('id'), str) or not case['id']:
            raise SnapshotError(f'{path}: each case must have a nonempty string id')
        key = case['id']
        if key in cases:
            raise SnapshotError(f'{path}: duplicate case id {key!r}')
        if not all(field in case for field in ('request', 'response', 'database', 'side_effects')):
            raise SnapshotError(f'{path}: case {key!r} must have request, response, database and side_effects')
        cases[key] = case
    return cases


def comparable(obj, path=''):
    if isinstance(obj, dict):
        out = {}
        for key, value in obj.items():
            normalized_key = key.lower() if path.endswith('/headers') else key
            if normalized_key in out:
                raise SnapshotError(f'Duplicate case-insensitive header key at {path}/{key}')
            out[normalized_key] = comparable(value, path + '/' + str(normalized_key))
        return out
    if isinstance(obj, list):
        return [comparable(v, path + '/' + str(i)) for i, v in enumerate(obj)]
    return obj


def diff(a, b, path, out, limit):
    if len(out) >= limit:
        return
    if type(a) is not type(b):
        out.append(f'{path}: differing types ({type(a).__name__} vs {type(b).__name__})')
    elif isinstance(a, dict):
        for key in sorted(a.keys() | b.keys()):
            next_path = path + '/' + str(key).replace('~', '~0').replace('/', '~1')
            if key not in a:
                out.append(f'{next_path}: only in Go snapshot')
            elif key not in b:
                out.append(f'{next_path}: only in Spring snapshot')
            else:
                diff(a[key], b[key], next_path, out, limit)
            if len(out) >= limit:
                break
    elif isinstance(a, list):
        if len(a) != len(b):
            out.append(f'{path}: array length differs ({len(a)} vs {len(b)})')
        for i, (av, bv) in enumerate(zip(a, b)):
            diff(av, bv, path + '/' + str(i), out, limit)
            if len(out) >= limit:
                break
    elif a != b:
        out.append(f'{path}: Spring={a!r}, Go={b!r}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('spring_snapshot', help='JSON snapshot recorded against Spring (isolated database)')
    parser.add_argument('go_snapshot', help='JSON snapshot recorded against Go (independent equivalent database)')
    parser.add_argument('--max-diffs', type=int, default=50)
    args = parser.parse_args()
    if args.max_diffs < 1:
        parser.error('--max-diffs must be positive')
    try:
        spring = read_cases(args.spring_snapshot)
        go = read_cases(args.go_snapshot)
        errors = []
        for case_id in sorted(spring.keys() | go.keys()):
            if case_id not in spring:
                errors.append(f'case {case_id!r} appears only in Go')
                continue
            if case_id not in go:
                errors.append(f'case {case_id!r} appears only in Spring')
                continue
            a = comparable(spring[case_id], f'/cases/{case_id}')
            b = comparable(go[case_id], f'/cases/{case_id}')
            diff(a, b, f'/cases/{case_id}', errors, args.max_diffs)
            if len(errors) >= args.max_diffs:
                break
    except SnapshotError as exc:
        print(f'INVALID SNAPSHOT: {exc}', file=sys.stderr)
        return 2
    if errors:
        print(f'FAIL: found {len(errors)} mismatch(es) (up to {args.max_diffs} shown):')
        for item in errors:
            print(' - ' + item)
        return 1
    print(f'PASS: {len(spring)} case(s) exactly matched across captured request, response, DB effects and side effects.')
    print('This does NOT prove untested behavior, actual DB/schema compatibility, or deployment safety.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
