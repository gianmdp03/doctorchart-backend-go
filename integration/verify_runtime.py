"""Restore this owned integration DB; check the current nonroot Go image and SIGTERM."""
import concurrent.futures, json, os, secrets, subprocess, sys, time
import urllib.parse
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'migration'))
from fixture import ROOT, WORK, command, sql
from compare import schema, capture
from verify_restore_runtime import normalized_dump

cfg=json.loads((WORK/'fixture.json').read_text())
origin=json.loads((WORK/'integration/context.json').read_text())['database']
patient_id,owner=sql(cfg,origin,'SELECT id,user_id FROM patients ORDER BY id LIMIT 1;').strip().split('|')
database='integration_restore_'+secrets.token_hex(4)
backup=normalized_dump(cfg,origin)
(WORK/'integration/owned-backup.sql').write_text(backup,encoding='utf8')
sql(cfg,'postgres','CREATE DATABASE '+database+';')
sql(cfg,database,backup)
evidence={'source_database':origin,'database':database,'restore_full_dump_equal':backup==normalized_dump(cfg,database),
    'restore_schema_equal':schema(cfg,origin)==schema(cfg,database),'tables':int(sql(cfg,database,"SELECT count(*) FROM pg_tables WHERE schemaname='public';").strip())}
name='doctorchart-integration-runtime-'+secrets.token_hex(4)
database_url=f'postgresql://migration:{urllib.parse.quote(cfg["password"],safe="")}@host.docker.internal:{cfg["db_port"]}/{urllib.parse.quote(database,safe="")}?sslmode=disable'
env=dict(os.environ,DATABASE_URL=database_url,GATEWAY_SHARED_SECRET=cfg['gateway'],
    SERVICES_BILLING_SHARED_SECRET=cfg['billing'],SERVICES_BILLING_URL='http://host.docker.internal:18999')
args=['docker','run','-d','--name',name,'--label','doctorchart-go.integration=runtime','-p','127.0.0.1::8083']
for key in ['DATABASE_URL','GATEWAY_SHARED_SECRET','SERVICES_BILLING_SHARED_SECRET','SERVICES_BILLING_URL']:args+=['-e',key]
args+=['doctorchart-go:staging-local']
subprocess.run(args,env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
holder=None
try:
    port=int(command('docker','port',name,'8083').decode().strip().split(':')[-1])
    for _ in range(150):
        try:
            if capture(port,cfg,'GET','/api/doctorchart/health',auth='none')['status']==200:break
        except OSError:time.sleep(.1)
    else:raise RuntimeError('Current Linux image did not become healthy')
    evidence['nonroot_user']=command('docker','inspect','-f','{{.Config.User}}',name).decode().strip()
    preview=capture(port,cfg,'POST','/api/doctorchart/calculators/preview',{'calculationCode':'imc','inputSnapshot':{'weightKg':70,'heightCm':170}},user=owner,role='DOCTOR',plan='PRO',extra_headers={'X-User-Plan-Expires-At':'2099-01-01T00:00:00Z'})
    reference=next(row['go']['body'] for row in json.loads((ROOT/'migration/evidence/compare.json').read_text(encoding='utf8'))['cases'] if row['id']=='calculator-preview-imc')
    evidence['linux_clinical_preview']=preview['status']==200 and all(preview['body'][key]==reference[key] for key in ['rawValue','formattedValue','unit','interpretation','secondaryOutputs','clinicalNotes'])
    evidence['linux_preview_status']=preview['status']
    evidence['linux_preview_difference_fields']=[key for key in ['rawValue','formattedValue','unit','interpretation','secondaryOutputs','clinicalNotes'] if preview['body'].get(key)!=reference[key]]
    evidence['linux_preview']=preview['body']
    holder=subprocess.Popen(['docker','exec','-i',cfg['container'],'psql','-U','migration','-d',database,'-Atq','-v','ON_ERROR_STOP=1'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
    holder.stdin.write("BEGIN;LOCK patients IN ACCESS EXCLUSIVE MODE;SELECT 'fixture-lock-held';\n");holder.stdin.flush()
    if holder.stdout.readline().strip()!='fixture-lock-held':raise RuntimeError('Owned PostgreSQL lock was not acquired')
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        request=pool.submit(capture,port,cfg,'GET','/api/doctorchart/patients/'+patient_id,user=owner)
        for _ in range(100):
            if int(sql(cfg,database,"SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock';").strip()):break
            time.sleep(.02)
        else:raise RuntimeError('Request did not reach DB barrier')
        started=time.monotonic();command('docker','kill','--signal=TERM',name)
        time.sleep(.3);holder.stdin.write('ROLLBACK;\n');holder.stdin.flush()
        response=request.result(timeout=15)
        code=int(command('docker','wait',name).decode().strip())
        evidence.update(inflight_request_status=response['status'],graceful_exit_code=code,shutdown_seconds=time.monotonic()-started,worker_joined=code==0)
    holder.stdin.close();holder.wait(timeout=5);holder=None
    evidence['passed']=all([evidence['restore_full_dump_equal'],evidence['restore_schema_equal'],evidence['tables']==21,evidence['nonroot_user']=='65532:65532',evidence['linux_clinical_preview'],evidence['inflight_request_status']==200,evidence['graceful_exit_code']==0])
    (ROOT/'integration/evidence/restore-runtime.json').write_text(json.dumps(evidence,indent=2),encoding='utf8')
    print(json.dumps(evidence),flush=True)
    if not evidence['passed']:raise SystemExit(1)
finally:
    if holder and holder.poll() is None:holder.terminate();holder.wait(timeout=5)
    command('docker','rm','-f',name)
