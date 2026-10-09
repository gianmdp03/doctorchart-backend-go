"""Compare startup transactions on owned schema-only and seeded databases."""
import json
import os
import secrets
import threading
import time
import urllib.request
from fixture import ROOT,WORK,sql,start_spring,start_go
from compare import Billing,schema
import http.server

def catalog(cfg,db):
    tables=['specialties','wizard_defaults','chart_definitions','clinical_calculation_definitions','specialty_calculator_defaults','vaccination_calendars','vaccination_doses']
    parts=[]
    for table in tables:
        subtract=" - 'created_at' - 'updated_at'" if table=='specialties' else " - 'updated_at'" if table=='wizard_defaults' else ''
        parts.extend(["'"+table+"'",f"(SELECT coalesce(jsonb_agg(to_jsonb(t){subtract} ORDER BY id),'[]') FROM {table} t)"])
    parts.extend(["'sequences'","(SELECT jsonb_agg(jsonb_build_object('name',sequencename,'value',last_value) ORDER BY sequencename) FROM pg_sequences WHERE schemaname='public')"])
    return json.loads(sql(cfg,db,'SELECT jsonb_build_object('+','.join(parts)+');'))

def main():
    cfg=json.loads((WORK/'fixture.json').read_text())
    server=http.server.ThreadingHTTPServer(('127.0.0.1',18999),Billing)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    results=[];processes=[]
    changes=[
        ('empty',None,True),
        ('existing','',True),
        ('missing-chart',"DELETE FROM chart_definitions WHERE chart_code='BMI_TRACKING';",True),
        ('custom-catalog',"UPDATE clinical_calculation_definitions SET name='Catálogo conservado' WHERE id=1; UPDATE chart_definitions SET title='Título conservado' WHERE id=1;",True),
        ('no-published-wizard',"UPDATE specialties SET active=false;",False),
        ('changed-calendar',"UPDATE vaccination_calendars SET definition=jsonb_set(definition,'{vacunas,0,nombre}','\"Cambio incompatible\"');",False),
        ('missing-dose',"DELETE FROM vaccination_doses WHERE id=(SELECT min(id) FROM vaccination_doses);",False),
        ('calendar-unknown-fields',"UPDATE vaccination_calendars SET definition=jsonb_set(definition,'{vacunas,0,unknownField}','true');",False),
        ('external-properties','',True),
    ]
    previous=os.environ.get('SPRING_JPA_HIBERNATE_DDL_AUTO');os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']='validate'
    try:
        for name,change,expected in changes:
            prefix='startup_'+secrets.token_hex(3)
            dbs=[prefix+'_spring',prefix+'_go']
            for db in dbs:
                sql(cfg,'postgres','CREATE DATABASE '+db+';')
                sql(cfg,db,(ROOT/'migration/schema-reference.sql').read_text(encoding='utf-8') if change is None else (WORK/'fixture.sql').read_text(encoding='utf-8'))
                if change:sql(cfg,db,change)
            before=schema(cfg,dbs[1]);observed=[]
            for index,db in enumerate(dbs):
                cwd=WORK;overrides=None
                if name=='external-properties':
                    cwd=WORK/db;cwd.mkdir()
                    (cwd/'secrets.properties').write_text('spring.datasource.username=migration\nspring.datasource.password='+cfg['password']+'\nserver.port='+str(18083+index)+'\n',encoding='ascii')
                    overrides={'DB_USERNAME':'incorrect-fallback','DB_PASSWORD':'incorrect-fallback','PORT':'18099'}
                if index==0:
                    try:process=start_spring(cfg,db,cwd=cwd,overrides=overrides);success=True;processes.append(process)
                    except RuntimeError:success=False;process=None
                else:
                    process=start_go(cfg,db,cwd=cwd,overrides=overrides);processes.append(process);success=False
                    for _ in range(150):
                        if process.poll() is not None:break
                        try:
                            with urllib.request.urlopen('http://127.0.0.1:18084/api/doctorchart/health',timeout=1) as response:
                                if response.status==200:success=True;break
                        except OSError:pass
                        time.sleep(.1)
                observed.append(success)
                if process and process.poll() is None:process.terminate();process.wait(timeout=20)
            states=[catalog(cfg,db) for db in dbs]
            passed=observed==[expected,expected] and states[0]==states[1] and before==schema(cfg,dbs[1])
            results.append(dict(case=name,expected_start=expected,observed_start=observed,same_catalog=states[0]==states[1],schema_unchanged=before==schema(cfg,dbs[1]),passed=passed,databases=dbs))
            if not passed:(WORK/'startup-diff.json').write_text(json.dumps(dict(case=name,states=states),ensure_ascii=False,indent=2),encoding='utf-8')
            print(name,'PASS' if passed else 'FAIL',flush=True)
        (ROOT/'migration/evidence/lifecycle.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        if not all(row['passed'] for row in results):raise SystemExit(1)
    finally:
        for process in processes:
            if process.poll() is None:process.terminate();process.wait(timeout=20)
        if previous is None:os.environ.pop('SPRING_JPA_HIBERNATE_DDL_AUTO',None)
        else:os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']=previous
        server.shutdown();server.server_close()

if __name__=='__main__':main()
