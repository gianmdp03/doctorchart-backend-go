"""Account for real Go handlers and concrete differential cases, without declaring full parity."""
import csv
import json
import re
from pathlib import Path
root=Path(__file__).resolve().parents[1]
def canonical(route):
    return re.sub(r'\{[^}]+\}','{}',route)
handlers={}
for path in (root/'internal').rglob('*.go'):
    if path.name.endswith('_test.go'):continue
    for match in re.finditer(r'"((?:GET|POST|PUT|PATCH|DELETE) /[^"\s]+)"',path.read_text(encoding='utf-8')):
        handlers[canonical(match[1])]=str(path.relative_to(root).as_posix())
evidence=root/'migration/evidence/compare.json'
cases=json.loads(evidence.read_text(encoding='utf-8'))['cases'] if evidence.exists() else []
path=root/'migration/CONTRACT_MATRIX.csv'
with path.open(encoding='utf-8',newline='') as inp:
    reader=csv.DictReader(inp);fields=reader.fieldnames;rows=list(reader)
for row in rows:
    if row['kind']!='http':continue
    signature=canonical(row['id'])
    if signature not in handlers:continue
    row['go_location']=handlers[signature]
    verb,route=row['id'].split(' ',1)
    pattern='^'+re.sub(r'\\\{[^}]+\\\}',r'[^/]+',re.escape(route))+'$'
    tested=[c for c in cases if c['method']==verb and re.fullmatch(pattern,c['path'].split('?')[0])]
    row['status']='IMPLEMENTED'
    row['differential_evidence']='migration/evidence/compare.json: '+','.join(c['id'] for c in tested)
    if tested and all(c['passed'] for c in tested):
        row['notes']='Differential cases PASS; endpoint remains IMPLEMENTED until exhaustive batch gates (time, concurrency, validator combinations, startup/deployment) pass.'
    elif tested:
        row['status']='PARTIAL';row['notes']='Differential failed: '+','.join(c['id'] for c in tested if not c['passed'])
    else:
        row['notes']='Handler exists; no differential evidence yet.'
with path.open('w',encoding='utf-8',newline='') as out:
    writer=csv.DictWriter(out,fields);writer.writeheader();writer.writerows(rows)
print(json.dumps({'discovered':sum(r['kind']=='http' for r in rows),'implemented_handlers':sum(r['kind']=='http' and bool(r['go_location']) for r in rows),'routes_with_passing_cases':sum(r['kind']=='http' and r['status']=='IMPLEMENTED' and r['differential_evidence']!='migration/evidence/compare.json: ' for r in rows),'verified_full_contracts':sum(r['status']=='VERIFIED' for r in rows)}))
