#!/usr/bin/env python3
"""Strictly compare two sanitized pg_dump --schema-only captures, without DB access.

Requires comparable pg_dump/PG versions and extraction flags; textual equality is
useful evidence but NOT proof of semantic compatibility or preserved data.
"""
import argparse
import difflib
from pathlib import Path
import sys


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('spring_schema_sql', help='Schema-only dump of isolated Spring-compatible clone')
    p.add_argument('go_schema_sql', help='Schema-only dump of isolated Go-compatible clone')
    p.add_argument('--max-lines', type=int, default=60)
    args = p.parse_args()
    if args.max_lines < 1:
        p.error('--max-lines must be positive')
    try:
        spring = Path(args.spring_schema_sql).read_text(encoding='utf-8').splitlines()
        go = Path(args.go_schema_sql).read_text(encoding='utf-8').splitlines()
    except (OSError, UnicodeError) as exc:
        print(f'INVALID INPUT: {exc}', file=sys.stderr)
        return 2
    if not spring or not go:
        print('INVALID INPUT: schema captures must be nonempty', file=sys.stderr)
        return 2
    if spring == go:
        print('PASS: schema SQL captures are textually identical.')
        print('Still requires catalog checks and bidirectional data tests; no databases were contacted.')
        return 0
    print('FAIL: schema SQL captures differ (investigate even when only formatting changes).')
    for line in list(difflib.unified_diff(spring, go, fromfile='spring.sql', tofile='go.sql', lineterm=''))[:args.max_lines]:
        print(line)
    return 1


if __name__ == '__main__':
    sys.exit(main())
