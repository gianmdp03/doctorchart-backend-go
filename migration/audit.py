"""Read-only Spring inventory. Generated rows are discovery, never proof of parity."""
import csv
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT.parent / 'doctorchart-backend'
JAVA = SOURCE / 'src/main/java/com/syntah/doctorchart'
rows = []
matrix_path=ROOT/'migration/CONTRACT_MATRIX.csv'
existing={}
if matrix_path.exists():
    with matrix_path.open(encoding='utf-8',newline='') as inp:
        existing={row['id']:row for row in csv.DictReader(inp)}
for path in sorted((JAVA / 'controller').rglob('*.java')):
    source = path.read_text(encoding='utf-8')
    source = re.sub(r'/\*.*?\*/|//[^\n]*', lambda m: '\n' * m[0].count('\n'), source, flags=re.S)
    class_start = source.index('public class')
    prefix = source[:class_start]
    base = re.search(r'@RequestMapping\((.*?)\)', prefix, re.S)
    bases = re.findall(r'"([^"]*)"', base[1]) if base else ['']
    for match in re.finditer(r'@(Get|Post|Put|Patch|Delete)Mapping(?:\((.*?)\))?', source[class_start:], re.S):
        tail = source[class_start + match.end():]
        method = re.search(r'public\s+.*?\b(\w+)\s*\(', tail, re.S)
        args = match[2] or ''
        route = re.search(r'(?:value\s*=\s*)?"([^"]*)"', args)
        suffix = route[1] if route else ''
        line = source[:class_start + match.start()].count('\n') + 1
        for base_path in bases:
            full = base_path + suffix
            identifier = match[1].upper() + ' ' + full
            rows.append(dict(id=identifier,kind='http',source_reference=f'{path.relative_to(SOURCE).as_posix()}:{line}#{method[1] if method else "UNKNOWN"}',external_contract_or_trigger=identifier,auth_requirements='See gateway filter + controller/method + service ownership',data_and_side_effects='UNVERIFIED: service/SQL trace required',source_test_case='See matching controller/service/integration tests',go_location='',differential_evidence='',status='DISCOVERED',notes='Discovered from actual mappings; exact validation and service semantics require review'))
for path in sorted(JAVA.rglob('*.java')):
    text = path.read_text(encoding='utf-8')
    for match in re.finditer(r'@(Scheduled|Async|EventListener|TransactionalEventListener|PostConstruct)(?:\((.*?)\))?', text, re.S):
        rows.append(dict(id=f'{path.stem}:{match[1]}:{text[:match.start()].count(chr(10))+1}',kind='lifecycle',source_reference=path.relative_to(SOURCE).as_posix(),external_contract_or_trigger=match[0],auth_requirements='Lifecycle',data_and_side_effects='UNVERIFIED',source_test_case='',go_location='',differential_evidence='',status='DISCOVERED',notes=''))
    if 'implements ApplicationRunner' in text or 'ApplicationRunner vaccinationBootstrap' in text:
        rows.append(dict(id=f'{path.stem}:ApplicationRunner',kind='lifecycle',source_reference=path.relative_to(SOURCE).as_posix(),external_contract_or_trigger='ApplicationRunner startup seed transaction',auth_requirements='Lifecycle',data_and_side_effects='UNVERIFIED',source_test_case='',go_location='',differential_evidence='',status='DISCOVERED',notes=''))
for row in rows:
    old=existing.get(row['id'])
    if old:
        for field in ['go_location','differential_evidence','status','notes','auth_requirements','data_and_side_effects']:
            row[field]=old[field]
fields = ['id','kind','source_reference','external_contract_or_trigger','auth_requirements','data_and_side_effects','source_test_case','go_location','differential_evidence','status','notes']
with (ROOT / 'migration/CONTRACT_MATRIX.csv').open('w', encoding='utf-8', newline='') as out:
    writer = csv.DictWriter(out, fields)
    writer.writeheader()
    writer.writerows(rows)
manifest = {str(p.relative_to(SOURCE).as_posix()):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((SOURCE/'src').rglob('*')) if p.is_file()}
for name in ['pom.xml','compose.dev.yml','README.md']:
    manifest[name] = hashlib.sha256((SOURCE/name).read_bytes()).hexdigest()
manifest_path=ROOT/'migration/source-manifest.json'
if manifest_path.exists():
    if json.loads(manifest_path.read_text(encoding='utf-8'))!=manifest:
        raise RuntimeError('Source changed since pinned audit; review changes before refreshing manifest')
else:
    manifest_path.write_text(json.dumps(manifest, indent=2)+'\n', encoding='utf-8')
print(json.dumps({'http_operations':sum(r['kind']=='http' for r in rows),'lifecycle_operations':sum(r['kind']=='lifecycle' for r in rows),'java_files':len(list(JAVA.rglob('*.java'))),'source_files_hashed':len(manifest)}))
