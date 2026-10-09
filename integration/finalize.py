"""Record current artifacts separately from immutable historical migration evidence."""
import hashlib, json, pathlib, re, subprocess
ROOT=pathlib.Path(__file__).resolve().parents[1]; EVIDENCE=ROOT/'integration/evidence'
def read(name):return json.loads((EVIDENCE/name).read_text(encoding='utf8'))
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
front=ROOT.parent/'doctorchart-frontend'
changed=subprocess.run(['git','-C',str(front),'diff','--name-only'],check=True,capture_output=True,text=True).stdout.splitlines()
changed=sorted(set(changed+subprocess.run(['git','-C',str(front),'ls-files','--others','--exclude-standard'],check=True,capture_output=True,text=True).stdout.splitlines()))
sources=[ROOT/'go.mod',ROOT/'go.sum',ROOT/'Dockerfile']+[p for p in sorted((ROOT/'internal').rglob('*')) if p.is_file()]+sorted((ROOT/'cmd').rglob('*.go'))+sorted((ROOT/'integration').glob('*.py'))+sorted((ROOT/'integration').glob('*.mjs'))
sources += [ROOT/'README.md', ROOT/'MIGRATION_STATE.md', ROOT/'migration/REPORT.md']+sorted((ROOT/'integration').glob('*.md'))
manifest={str(p.relative_to(ROOT)).replace('\\','/'):digest(p) for p in sources}
manifest.update({'doctorchart-frontend/'+p:digest(front/p) for p in changed})
manifest['bin/doctorchart.exe']=digest(ROOT/'bin/doctorchart.exe')
(EVIDENCE/'artifact-manifest.json').write_text(json.dumps(manifest,indent=2),encoding='utf8')
reference=read('reference-manifest-current.json')
drift=[p for p,h in reference.items() if not (ROOT.parent/p).is_file() or digest(ROOT.parent/p)!=h]
(EVIDENCE/'references-current.json').write_text(json.dumps({'files':len(reference),'changed_since_snapshot':drift,'source_repositories_written_by_this_task':False},indent=2),encoding='utf8')
events=[json.loads(line) for line in (EVIDENCE/'go-tests.jsonl').read_text(encoding='utf8').splitlines() if line.startswith('{')]
tests={status:sum(e.get('Action')==status and 'Test' in e for e in events) for status in ['pass','fail','skip']}
angular=re.search(r'Tests\s+(\d+) passed', (EVIDENCE/'frontend-tests.log').read_text(encoding='utf8'))
browser=read('browser.json')['cases']; setup=read('browser-setup.json')['cases']
summary={'status':'READY FOR STAGING — DoctorChart local artifacts; Gateway version and external providers require staging validation',
    'scope':'Synthetic local stack only; no deployment; historical migration evidence not rewritten',
    'frontend_tests':int(angular.group(1)), 'frontend_files':changed,'go_native_tests':tests,
    'go_quality':dict((name,status) for status,name in re.findall(r'^(PASS|FAIL|BLOCKED|NOT RUN)\s+(.+)$',(EVIDENCE/'go-quality.log').read_text(),re.M)),
    'http':read('local-http.json').get('summary'), 'current_gateway':{'passed':read('current-gateway.json')['passed'],'total':read('current-gateway.json')['total']},
    'pdf_current':{'passed':read('pdf-current.json')['passed'],'total':read('pdf-current.json')['total']},
    'browser_final':{'passed':sum(x['status']=='PASS' for x in browser),'total':len(browser)},
    'browser_setup':{'passed':sum(x['status']=='PASS' for x in setup),'total':len(setup)},
    'restore_runtime':read('restore-runtime.json')['passed'],'source_preserved':read('source-preserved.json'),
    'gateway_drift_after_final_snapshot':drift,
    'not_run':['Real Supabase/OAuth/JWKS rotation and automatic long-lived token renewal','Real Billing service/payment lifecycle and plans UI','AWS/R2 durable storage service','Gemini external generation','Real databases and deployment'],
    'evidence_hashes':{p.name:digest(p) for p in sorted(EVIDENCE.iterdir()) if p.is_file() and p.name!='summary.json'}}
(EVIDENCE/'summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2),encoding='utf8')
print(json.dumps({k:v for k,v in summary.items() if k not in ['evidence_hashes','frontend_files','go_quality','http']},ensure_ascii=True))
