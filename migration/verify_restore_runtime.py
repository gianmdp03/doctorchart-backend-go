"""Owned PostgreSQL restore and nonroot Linux container shutdown evidence."""
import concurrent.futures
import json
import os
import secrets
import subprocess
import time
from fixture import ROOT,WORK,command,sql
from compare import schema,capture

def normalized_dump(cfg,db):
    text=command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d',db,'--no-owner','--no-privileges').decode('utf-8')
    return '\n'.join(line for line in text.splitlines() if not line.startswith(('\\restrict','\\unrestrict')))+'\n'

def main():
    cfg=json.loads((WORK/'fixture.json').read_text())
    comparison=json.loads((ROOT/'migration/evidence/compare.json').read_text(encoding='utf-8'))
    origin=comparison['summary']['databases'][1]
    database='restore_'+secrets.token_hex(3)
    backup=normalized_dump(cfg,origin)
    (WORK/'owned-backup.sql').write_text(backup,encoding='utf-8')
    sql(cfg,'postgres','CREATE DATABASE '+database+';');sql(cfg,database,backup)
    restored=normalized_dump(cfg,database)
    evidence={'restore_full_dump_equal':backup==restored,'restore_schema_equal':schema(cfg,origin)==schema(cfg,database),'database':database,'source_database':origin,'tables':int(sql(cfg,database,"SELECT count(*) FROM pg_tables WHERE schemaname='public';").strip())}
    if not evidence['restore_full_dump_equal'] or not evidence['restore_schema_equal']:raise RuntimeError('Owned backup/restore changed data/schema')
    name='doctorchart-go-runtime-'+secrets.token_hex(4);holder=None
    env=dict(os.environ,SPRING_DATASOURCE_URL=f'jdbc:postgresql://host.docker.internal:{cfg["db_port"]}/{database}',DB_USERNAME='migration',DB_PASSWORD=cfg['password'],GATEWAY_SHARED_SECRET=cfg['gateway'],SERVICES_BILLING_SHARED_SECRET=cfg['billing'],SERVICES_BILLING_URL='http://127.0.0.1:1')
    args=['docker','run','-d','--name',name,'--label','doctorchart-go.migration=runtime','-p','127.0.0.1::8083']
    for key in ['SPRING_DATASOURCE_URL','DB_USERNAME','DB_PASSWORD','GATEWAY_SHARED_SECRET','SERVICES_BILLING_SHARED_SECRET','SERVICES_BILLING_URL']:args+=['-e',key]
    args+=['doctorchart-go:migration-local']
    subprocess.run(args,env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    try:
        port=int(command('docker','port',name,'8083').decode().strip().split(':')[-1])
        for _ in range(180):
            try:
                if capture(port,cfg,'GET','/api/doctorchart/health',auth='none')['status']==200:break
            except OSError:time.sleep(.1)
        else:raise RuntimeError('Linux container startup failed')
        evidence['nonroot_user']=command('docker','inspect','-f','{{.Config.User}}',name).decode().strip()
        preview=capture(port,cfg,'POST','/api/doctorchart/calculators/preview',{'calculationCode':'imc','inputSnapshot':{'weightKg':70,'heightCm':170}})
        reference=next(row['go']['body'] for row in comparison['cases'] if row['id']=='calculator-preview-imc')
        evidence['linux_clinical_preview']=preview['status']==200 and all(preview['body'][key]==reference[key] for key in ['rawValue','formattedValue','unit','interpretation','secondaryOutputs','clinicalNotes'])
        holder=subprocess.Popen(['docker','exec','-i',cfg['container'],'psql','-U','migration','-d',database,'-Atq','-v','ON_ERROR_STOP=1'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
        holder.stdin.write("BEGIN;LOCK patients IN ACCESS EXCLUSIVE MODE;SELECT 'fixture-lock-held';\n");holder.stdin.flush()
        if holder.stdout.readline().strip()!='fixture-lock-held':raise RuntimeError('Unable to coordinate shutdown request')
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            request=pool.submit(capture,port,cfg,'GET','/api/doctorchart/patients/1')
            for _ in range(100):
                waiting=int(sql(cfg,database,"SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock';").strip())
                if waiting:break
                time.sleep(.02)
            else:raise RuntimeError('Request did not reach the PostgreSQL barrier')
            started=time.monotonic();command('docker','kill','--signal=TERM',name)
            time.sleep(.3)
            holder.stdin.write('ROLLBACK;\n');holder.stdin.flush()
            response=request.result(timeout=15)
            exit_code=command('docker','wait',name).decode().strip()
            evidence.update(inflight_request_status=response['status'],graceful_exit_code=int(exit_code),shutdown_seconds=time.monotonic()-started,worker_joined=exit_code=='0')
        holder.stdin.close();holder.wait(timeout=5);holder=None
        evidence['passed']=evidence['linux_clinical_preview'] and evidence['nonroot_user']=='65532:65532' and evidence['inflight_request_status']==200 and evidence['graceful_exit_code']==0
        (ROOT/'migration/evidence/restore-runtime.json').write_text(json.dumps(evidence,indent=2)+'\n',encoding='utf-8')
        print(json.dumps(evidence),flush=True)
        if not evidence['passed']:raise SystemExit(1)
    finally:
        if holder and holder.poll() is None:holder.terminate();holder.wait(timeout=5)
        command('docker','rm','-f',name)

if __name__=='__main__':main()
