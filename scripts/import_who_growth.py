"""Rebuild the embedded WHO reference tables. Requires openpyxl; no patient data.

Original workbooks and SHA-256 hashes are retained. --download downloads from WHO
and fails if a published workbook differs from the pinned manifest.
"""
import argparse
import csv
import hashlib
import json
import math
import urllib.request
from pathlib import Path

import openpyxl

ROOT = Path(__file__).resolve().parents[1] / 'internal' / 'growth' / 'data'
BASE = 'https://cdn.who.int/media/docs/default-source/child-growth/'
PATHS = {
    'wfa06': 'child-growth-standards/indicators/weight-for-age/expanded-tables/wfa-{sex}-zscore-expanded-tables.xlsx',
    'lhfa06': 'child-growth-standards/indicators/length-height-for-age/expandable-tables/lhfa-{sex}-zscore-expanded-tables.xlsx',
    'hcfa06': 'child-growth-standards/indicators/head-circumference-for-age/expanded-tables/hcfa-{sex}-zscore-expanded-tables.xlsx',
    'bfa06': 'child-growth-standards/indicators/body-mass-index-for-age/expanded-tables/bfa-{sex}-zscore-expanded-tables.xlsx',
    'wfl06': 'child-growth-standards/indicators/weight-for-length-height/expanded-tables/wfl-{sex}-zscore-expanded-table.xlsx',
    'wfh06': 'child-growth-standards/indicators/weight-for-length-height/expanded-tables/wfh-{sex}-zscore-expanded-tables.xlsx',
    'hfa07': 'growth-reference-5-19-years/height-for-age-%285-19-years%29/hfa-{sex}-z-who-2007-exp.xlsx',
    'bfa07': 'growth-reference-5-19-years/bmi-for-age-%285-19-years%29/bmi-{sex}-z-who-2007-exp.xlsx',
}


def sources():
    for key, path in PATHS.items():
        for sex in ('girls', 'boys'):
            url = BASE + path.format(sex=sex)
            if key.endswith('06'):
                percentile = url.replace('zscore', 'percentiles').replace('expanded-table.xlsx', 'expanded-tables.xlsx')
            else:
                percentile = url.replace('-z-who-2007-', '-perc-who2007-')
            yield key + '_' + sex, url, percentile
    for sex, zsuffix, psuffix in (
        ('girls', '7ea58763-36a2-436d-bef0-7fcfbadd2820', '6040a43e-81da-48fa-a2d4-5c856fe4fe71'),
        ('boys', '0ff9c43c-8cc0-4c23-9fc6-81290675e08b', '07eb5053-9a09-4910-aa6b-c7fb28012ce6'),
    ):
        prefix = BASE + 'growth-reference-5-19-years/weight-for-age-%285-10-years%29/hfa-' + sex
        yield 'wfa07_' + sex, prefix + '-z-who-2007-exp_' + zsuffix + '.xlsx', prefix + '-perc-who2007-exp_' + psuffix + '.xlsx'


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--download', action='store_true')
    parser.add_argument('--initialize', action='store_true', help='Initial scientific import only; review all hash changes.')
    args = parser.parse_args()
    originals = ROOT / 'originals'
    originals.mkdir(parents=True, exist_ok=True)
    pinned = json.loads((ROOT / 'manifest.json').read_text(encoding='utf-8')) if (ROOT / 'manifest.json').exists() else {}
    manifest = {}
    fixtures = []
    for key, url, percentile_url in sources():
        data_rows = None
        for kind, source in (('lms', url), ('percentiles', percentile_url)):
            path = originals / (key + '_' + kind + '.xlsx')
            if args.download or not path.exists():
                request = urllib.request.Request(source, headers={'User-Agent': 'Mozilla/5.0'})
                payload = urllib.request.urlopen(request, timeout=60).read()
                digest = hashlib.sha256(payload).hexdigest()
                expected = pinned.get(key, {}).get(kind, {}).get('sha256')
                if not args.initialize and expected != digest:
                    raise ValueError(f'{key}/{kind}: unexpected WHO workbook checksum {digest}')
                path.write_bytes(payload)
            payload = path.read_bytes()
            digest = hashlib.sha256(payload).hexdigest()
            if not args.initialize and pinned[key][kind]['sha256'] != digest:
                raise ValueError(f'{key}/{kind}: local checksum mismatch')
            workbook = openpyxl.load_workbook(path, read_only=True, data_only=True)
            if len(workbook.sheetnames) != 1:
                raise ValueError(f'{key}: unexpected sheets')
            rows = list(workbook.active.values)
            headers = rows[0]
            if headers[:4] != (headers[0], 'L', 'M', 'S') or headers[0] not in ('Day', 'Age', 'Month', 'Length', 'Height'):
                raise ValueError(f'{key}: invalid LMS headers')
            if kind == 'lms':
                data_rows = rows[1:]
                family = key.split('_')[0]
                expected_domain = {
                    'wfa06': (0, 1856, 1857), 'lhfa06': (0, 1856, 1857),
                    'hcfa06': (0, 1856, 1857), 'bfa06': (0, 1856, 1857),
                    'wfl06': (45, 110, 651), 'wfh06': (65, 120, 551),
                    'wfa07': (61, 120, 60), 'hfa07': (61, 228, 168),
                    'bfa07': (61, 228, 168),
                }[family]
                if (data_rows[0][0], data_rows[-1][0], len(data_rows)) != expected_domain:
                    raise ValueError(f'{key}: unexpected official domain/row count')
                previous = -1
                for row in data_rows:
                    if not all(isinstance(x, (int, float)) and math.isfinite(x) for x in row[:4]):
                        raise ValueError(f'{key}: nonfinite parameters')
                    if row[0] <= previous or row[2] <= 0 or row[3] <= 0:
                        raise ValueError(f'{key}: invalid order/M/S')
                    previous = row[0]
                with (ROOT / (key + '.csv')).open('w', newline='', encoding='utf-8') as dest:
                    writer = csv.writer(dest, lineterminator='\n')
                    writer.writerow(('x', 'l', 'm', 's'))
                    writer.writerows(row[:4] for row in data_rows)
            else:
                if len(rows) - 1 != len(data_rows):
                    raise ValueError(f'{key}: percentile/LMS row count mismatch')
                cols = [headers.index('P' + str(p)) for p in (3, 15, 50, 85, 97)]
                # Independently published centile and extreme-SD values, not recomputed expectations.
                chosen = sorted(set([0, 1, len(data_rows)//2, len(data_rows)-1] + ([730, 731, 1826] if key.split('_')[0] in ('wfa06', 'lhfa06', 'hcfa06', 'bfa06') else [])))
                for index in chosen:
                    row = rows[index+1]
                    if row[0] != data_rows[index][0]:
                        raise ValueError(f'{key}: percentiles/LMS coordinates disagree')
                    fixtures.append({'dataset': key, 'x': row[0], 'percentiles': [row[c] for c in cols]})
            manifest.setdefault(key, {})[kind] = {'url': source, 'sha256': digest, 'file': str(path.relative_to(ROOT)).replace('\\', '/'), 'sheet': workbook.active.title, 'coordinate': headers[0], 'rows': len(rows)-1, 'version': 'WHO 2006' if key.split('_')[0].endswith('06') else 'WHO 2007', 'retrieved': '2026-10-09'}
            workbook.close()
        print(key, len(data_rows), flush=True)
        manifest[key]['normalizedSha256'] = hashlib.sha256((ROOT / (key + '.csv')).read_bytes()).hexdigest()
    (ROOT / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
    (ROOT / 'official-centiles.json').write_text(json.dumps(fixtures, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
