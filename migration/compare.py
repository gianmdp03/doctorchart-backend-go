"""Runnable Spring↔Go comparison on fresh, owned clones. Captures contain fixture data only."""
import concurrent.futures
import copy
import csv
import datetime
import hashlib
import http.server
import json
import os
import secrets
import subprocess
import threading
import time
import urllib.error
import urllib.request
import urllib.parse
import sys
import tempfile
import base64
import struct
import zlib
import uuid
import io
import re
from pypdf import PdfReader
sys.stdout.reconfigure(encoding='utf-8')

from fixture import ROOT, WORK, command, sql, start_spring

class Billing(http.server.BaseHTTPRequestHandler):
    catalog = {'BASIC': {'max_patients': 51, 'max_records_per_patient': 101}, 'PRO': {'max_patients': 501, 'max_records_per_patient': 1001}, 'UNLIMITED': {'max_patients': -1, 'max_records_per_patient': -1}}
    def do_GET(self):
        if self.path != '/api/billing/limits/DOCTORCHART':
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header('Content-Type','application/json')
        self.end_headers()
        self.wfile.write(json.dumps(self.catalog).encode())
    def log_message(self,*args):
        pass

def capture(port, cfg, method, path, body=None, role='SUPER_ADMIN', user='fixture-doctor', plan='BASIC', auth='valid', internal=None,extra_headers=None):
    headers={}
    if auth!='none':
        headers.update({'X-Gateway-Secret':cfg['gateway'] if auth!='wrong' else 'wrong','X-User-Id':user,'X-User-Role':role,'X-User-Plan':plan})
    if internal is not None:
        headers['X-Internal-Secret']=cfg['billing'] if internal=='valid' else 'wrong'
    if extra_headers:headers.update(extra_headers)
    data=None
    if body is not None:
        if isinstance(body,tuple):data,headers['Content-Type']=body
        else:
            data=body.encode() if isinstance(body,str) else json.dumps(body,ensure_ascii=False).encode()
            headers['Content-Type']='application/json'
    request=urllib.request.Request(f'http://127.0.0.1:{port}'+path,data=data,headers=headers,method=method)
    try:
        response=urllib.request.urlopen(request,timeout=20)
    except urllib.error.HTTPError as error:
        response=error
    with response:
        raw=response.read()
        if response.headers.get('Content-Type','').startswith(('image/','application/pdf','application/octet-stream')):
            value={'binaryBase64':base64.b64encode(raw).decode(),'size':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}
        else:
            try: value=json.loads(raw)
            except (json.JSONDecodeError,UnicodeDecodeError): value=raw.decode('utf-8',errors='replace')
        observed={name.lower():value for name,value in response.headers.items() if name.lower() in ['content-type','location','cache-control','pragma','expires','x-content-type-options','x-frame-options','x-xss-protection','allow','content-disposition','content-security-policy','accept-ranges','content-range']}
        return dict(status=response.status,headers=observed,body=value)

def multipart(name,data,description=None):
    boundary='doctorchart-fixture-boundary'
    payload=(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()+data+b'\r\n'
    if description is not None:payload+=(f'--{boundary}\r\nContent-Disposition: form-data; name="description"\r\n\r\n'+description+'\r\n').encode()
    payload+=(f'--{boundary}--\r\n').encode()
    return payload,'multipart/form-data; boundary='+boundary

def image_fixture():
    def chunk(kind,data):return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data)&0xffffffff)
    return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',2,2,8,2,0,0,0))+chunk(b'IDAT',zlib.compress(b'\x00\xff\x00\x00\x00\xff\x00'*2))+chunk(b'IEND',b'')

def pdf_fixture():
    content=b'%PDF-1.4\n'
    offsets=[0]
    objects=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << >> /Contents 4 0 R >>',b'<< /Length 0 >>\nstream\n\nendstream']
    for index,body in enumerate(objects,1):
        offsets.append(len(content));content+=str(index).encode()+b' 0 obj\n'+body+b'\nendobj\n'
    start=len(content);content+=b'xref\n0 5\n0000000000 65535 f \n'
    for offset in offsets[1:]:content+=f'{offset:010d} 00000 n \n'.encode()
    return content+b'trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n'+str(start).encode()+b'\n%%EOF\n'

def pdf_content(data):
    reader=PdfReader(io.BytesIO(data))
    pages=[]
    for page in reader.pages:
        text=' '.join((page.extract_text() or '').split())
        labels=['Rp/','Solicito la realización de los siguientes estudios diagnósticos y prácticas:']
        counts={label:text.count(label) for label in labels}
        for label in labels:text=text.replace(label,'')
        pages.append({'width':float(page.mediabox.width),'height':float(page.mediabox.height),'text':' '.join(text.split()),'body_labels':counts})
    return {'pages':pages,'encrypted':reader.is_encrypted}

def schema(cfg,db):
    raw=command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d',db,'--schema-only','--no-owner','--no-privileges').decode()
    return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict','\\unrestrict')))

def state(cfg,db):
    # Audit times cannot be fixed via Java's Instant.now; full time semantics remain UNVERIFIED.
    parts=[]
    for table,excluded in [('specialties',['created_at','updated_at']),('wizard_defaults',['updated_at']),('chart_definitions',[]),('clinical_calculation_definitions',[]),('specialty_calculator_defaults',[]),('vaccination_calendars',[]),('vaccination_doses',[]),('doctor_workspaces',['created_at','updated_at']),('patients',['updated_at']),('doctor_chart_configs',[]),('patient_vaccinations',[]),('clinical_encounters',['updated_at']),('patient_prescription_records',['created_at']),('prescription_medications',[]),('prescription_diagnostic_orders',[]),('doctor_calculator_preferences',['created_at','updated_at']),('doctor_calculator_selections',[]),('patient_calculation_logs',['created_at']),('patient_medical_studies',[]),('study_file_deletions',['requested_at'])]:
        subtract=''.join(" - '"+column+"'" for column in excluded)
        order='record_id,item_position' if table.startswith('prescription_') else 'preference_id,position' if table=='doctor_calculator_selections' else 'storage_key' if table=='study_file_deletions' else 'id'
        parts.extend(["'"+table+"'",f"(SELECT coalesce(jsonb_agg(to_jsonb(t){subtract} ORDER BY {order}),'[]'::jsonb) FROM {table} t)"])
    parts.extend(["'sequences'","(SELECT coalesce(jsonb_agg(jsonb_build_object('name',sequencename,'last',last_value) ORDER BY sequencename),'[]'::jsonb) FROM pg_sequences WHERE schemaname='public')"])
    return json.loads(sql(cfg,db,'SELECT jsonb_build_object('+','.join(parts)+');').strip())

def main():
    cfg=json.loads((WORK/'fixture.json').read_text())
    if not cfg['container'].startswith('doctorchart-go-fixture-'):
        raise RuntimeError('Refusing an unowned DB')
    suffix=secrets.token_hex(3)
    source_db='spring_'+suffix;target_db='target_'+suffix
    for db in [source_db,target_db]:
        sql(cfg,'postgres','CREATE DATABASE '+db+';')
        sql(cfg,db,(WORK/'fixture.sql').read_text(encoding='utf-8'))
    before=schema(cfg,target_db)
    maintenance_fixtures=[]
    for db in [source_db,target_db]:
        directory=__import__('pathlib').Path(tempfile.gettempdir())/cfg['container']/('studies-'+db)
        directory.mkdir(parents=True,exist_ok=True,mode=0o700)
        old_key=str(uuid.uuid4());fresh_key=str(uuid.uuid4())
        for key in [old_key,fresh_key]:(directory/key).write_bytes(b'owned disposable maintenance fixture')
        old=time.time()-25*3600;os.utime(directory/old_key,(old,old))
        maintenance_fixtures.append((directory,old_key,fresh_key))
    billing=http.server.ThreadingHTTPServer(('127.0.0.1',18999),Billing)
    thread=threading.Thread(target=billing.serve_forever,daemon=True);thread.start()
    processes=[]
    records=[]
    prescription_times=[{},{}]
    study_keys=[{},{}]
    try:
        processes.append(start_spring(cfg,source_db))
        env=dict(os.environ,PORT='18084',SPRING_DATASOURCE_URL=f'jdbc:postgresql://127.0.0.1:{cfg["db_port"]}/{target_db}',DB_USERNAME='migration',DB_PASSWORD=cfg['password'],GATEWAY_SHARED_SECRET=cfg['gateway'],SERVICES_BILLING_SHARED_SECRET=cfg['billing'],SERVICES_BILLING_URL='http://127.0.0.1:18999',STUDIES_STORAGE_DIR=str(__import__('pathlib').Path(tempfile.gettempdir())/cfg['container']/('studies-'+target_db)))
        with (WORK/'go.log').open('wb') as log:
            processes.append(subprocess.Popen([str(ROOT/'bin/doctorchart.exe')],cwd=WORK,env=env,stdout=log,stderr=subprocess.STDOUT))
        for _ in range(100):
            try:
                capture(18084,cfg,'GET','/api/doctorchart/health');break
            except OSError:
                time.sleep(.1)
        def case(identifier,method,path,body=None,clock_field=None,calculator_clock=None,study_clock=False,pdf_semantic=False,**kwargs):
            began=datetime.datetime.now(datetime.timezone.utc)
            responses=[capture(port,cfg,method,path,body,**kwargs) for port in [18083,18084]]
            ended=datetime.datetime.now(datetime.timezone.utc)
            states=[state(cfg,db) for db in [source_db,target_db]]
            compared=copy.deepcopy(responses)
            clock_valid=True
            if pdf_semantic:
                for index,response in enumerate(responses):
                    if response['status']!=200 or not isinstance(response['body'],dict) or 'binaryBase64' not in response['body']:
                        clock_valid=False;continue
                    data=base64.b64decode(response['body']['binaryBase64'])
                    reader=PdfReader(io.BytesIO(data))
                    # Compare clinical text and labels independently of OpenPDF's
                    # standalone-paragraph/table stream ordering; visual QA is separate.
                    semantic=pdf_content(data)
                    compared[index]['body']=semantic
                    response['pdfSemantic']=semantic
                    (WORK/(identifier+'-'+['spring','go'][index]+'.pdf')).write_bytes(data)
            for index,response in enumerate(compared):
                if isinstance(response['body'],dict) and 'timestamp' in response['body']:
                    observed=datetime.datetime.fromisoformat(response['body']['timestamp'])
                    clock_valid=clock_valid and began<=observed<=ended
                    response['body']['timestamp']='<validated error issuance time>'
            if study_clock:
                for index,response in enumerate(responses):
                    value=response['body']
                    if response['status']!=201 or not isinstance(value,dict):clock_valid=False;continue
                    observed=datetime.datetime.fromisoformat(value['uploadedAt'])
                    clock_valid=clock_valid and began<=observed<=ended
                    compared[index]['body']['uploadedAt']='<validated upload time>'
                    for row in states[index]['patient_medical_studies']:
                        if row['id']!=value['id']:continue
                        persisted=datetime.datetime.fromisoformat(row['uploaded_at'])
                        key=row['stored_file_name']
                        clock_valid=clock_valid and abs((persisted-observed).total_seconds())<=.000001 and str(uuid.UUID(key,version=4))==key
                        directory=__import__('pathlib').Path(tempfile.gettempdir())/cfg['container']/('studies-'+[source_db,target_db][index])
                        data=(directory/key).read_bytes()
                        clock_valid=clock_valid and len(data)==value['fileSize']
                        study_keys[index][key]='<validated private file for study '+str(value['id'])+'>'
                        row['uploaded_at']='<validated upload time>'
            for index,current in enumerate(states):
                for row in current['patient_medical_studies']:
                    key=row['stored_file_name']
                    if key in study_keys[index]:row['stored_file_name']=study_keys[index][key]
                for row in current['study_file_deletions']:
                    key=row['storage_key']
                    if key in study_keys[index]:row['storage_key']=study_keys[index][key]
            if calculator_clock:
                for index,response in enumerate(responses):
                    value=response['body']
                    if response['status'] not in [200,201] or not isinstance(value,dict):
                        clock_valid=False;continue
                    observed=datetime.datetime.fromisoformat(value[calculator_clock])
                    clock_valid=clock_valid and began<=observed<=ended
                    compared[index]['body'][calculator_clock]='<validated calculator issuance time>'
                    if calculator_clock=='createdAt':
                        location=response['headers'].get('location','')
                        expected=f'http://127.0.0.1:{[18083,18084][index]}'+path+'/'+str(value['id'])
                        clock_valid=clock_valid and location==expected
                        compared[index]['headers']['location']='<validated request origin>'+path+'/'+str(value['id'])
                        persisted=datetime.datetime.fromisoformat(sql(cfg,[source_db,target_db][index],f"SELECT created_at FROM patient_calculation_logs WHERE id={int(value['id'])};").strip())
                        clock_valid=clock_valid and abs((persisted-observed).total_seconds())<=.000001
                    if calculator_clock in ['createdAt','calculatedAt']:
                        date_line=value['plainTextEvolution'].splitlines()[1]
                        expected_date=observed.astimezone(datetime.timezone(datetime.timedelta(hours=-3))).strftime('%d/%m/%Y %H:%M')
                        clock_valid=clock_valid and date_line.startswith('Fecha: '+expected_date)
                        evolution=value['plainTextEvolution'].replace(date_line,'Fecha: <validated business-zone issuance date>',1)
                        compared[index]['body']['plainTextEvolution']=evolution
                        if calculator_clock=='createdAt':
                            for row in states[index]['patient_calculation_logs']:
                                if row['id']==value['id']:row['plain_text_evolution']=evolution
            if clock_field:
                for index, response in enumerate(responses):
                    raw=response['body'].get(clock_field) if isinstance(response['body'],dict) else None
                    if response['status']!=201 or raw is None:
                        clock_valid=False;continue
                    observed=datetime.datetime.fromisoformat(raw)
                    clock_valid=clock_valid and began<=observed<=ended
                    compared[index]['body'][clock_field]='<server issuance time within observed request interval>'
                    # Only this newly created row's date is compared by its observed clock window.
                    # Subsequent PUTs use identical fixed dates and exact row/response equality.
                    if clock_field=='createdAt':
                        prescription_times[index][response['body']['id']]=observed
                        persisted=datetime.datetime.fromisoformat(sql(cfg,[source_db,target_db][index],f"SELECT created_at FROM patient_prescription_records WHERE id={int(response['body']['id'])};").strip())
                        clock_valid=clock_valid and abs((persisted-observed).total_seconds())<=.000001
                    for row in states[index]['clinical_encounters'] if clock_field=='encounterDate' else []:
                        if row['id']==response['body']['id']:
                            persisted=datetime.datetime.fromisoformat(row['encounter_date'])
                            clock_valid=clock_valid and abs((persisted-observed).total_seconds())<=.000001
                            row['encounter_date']='<validated server issuance time>'
            def canonical_prescriptions(value,index):
                nonlocal clock_valid
                if isinstance(value,list):
                    for item in value:canonical_prescriptions(item,index)
                elif isinstance(value,dict):
                    if value.get('source')=='PRESCRIPTION_RECORD' and 'issuedAt' in value:
                        observed=datetime.datetime.fromisoformat(value['issuedAt'])
                        expected=prescription_times[index].get(value['id'])
                        clock_valid=clock_valid and expected is not None and abs((observed-expected).total_seconds())<=.000001
                        value['issuedAt']='<validated record issuance time>'
                    if value.get('type') in ['PRESCRIPTION','STUDY_ORDERS'] and 'createdAt' in value:
                        if clock_field!='createdAt':
                            observed=datetime.datetime.fromisoformat(value['createdAt'])
                            expected=prescription_times[index].get(value['id'])
                            clock_valid=clock_valid and expected is not None and abs((observed-expected).total_seconds())<=.000001
                        value['createdAt']='<validated record issuance time>'
                    for item in value.values():canonical_prescriptions(item,index)
            for index,response in enumerate(compared):canonical_prescriptions(response,index)
            passed=clock_valid and compared[0]==compared[1] and states[0]==states[1]
            records.append(dict(id=identifier,method=method,path=path,spring=responses[0],go=responses[1],database_equal=states[0]==states[1],clock_field=clock_field,clock_window_valid=clock_valid,pdf_semantic=pdf_semantic,passed=passed))
            if not passed:
                (WORK/'last-diff.json').write_text(json.dumps({'id':identifier,'responses':responses,'states':states},ensure_ascii=False,indent=2),encoding='utf-8')
                print('DIFF',identifier,json.dumps(responses,ensure_ascii=False)[:1800], 'db_equal=',states[0]==states[1],flush=True)
            if calculator_clock:
                # Pin validated issuance times in these disposable clones for exact subsequent reads.
                for db in [source_db,target_db]:
                    if calculator_clock=='updatedAt':sql(cfg,db,"UPDATE doctor_calculator_preferences SET created_at='2026-10-08T12:00:00Z',updated_at='2026-10-08T12:00:00Z';")
                    if calculator_clock=='createdAt':sql(cfg,db,"UPDATE patient_calculation_logs SET created_at='2026-10-08T12:00:00Z';")
            if study_clock:
                for db in [source_db,target_db]:sql(cfg,db,"UPDATE patient_medical_studies SET uploaded_at='2026-10-08T12:00:00Z';")
            return responses[0]['body']
        for path in ['ping','health']:
            case(path,'GET','/api/doctorchart/'+path,auth='none')
        for auth in ['none','wrong']:
            case('gateway-'+auth,'GET','/api/doctorchart/limits/all',auth=auth)
        case('gateway-no-user','GET','/api/doctorchart/limits/all',user=' ')
        case('admin-role-denied','GET','/api/doctorchart/admin/specialties',role='DOCTOR')
        case('private-health-head','HEAD','/api/doctorchart/health',auth='none')
        case('unknown-route','GET','/api/doctorchart/unknown-fixture-route')
        case('wrong-method','DELETE','/api/doctorchart/health')
        case('options-health','OPTIONS','/api/doctorchart/health',auth='none')
        case('options-patients','OPTIONS','/api/doctorchart/patients',auth='none')
        case('options-unknown','OPTIONS','/api/doctorchart/unknown-fixture-route',auth='none')
        case('options-preflight','OPTIONS','/api/doctorchart/patients',auth='none',extra_headers={'Origin':'https://fixture.invalid','Access-Control-Request-Method':'GET'})
        case('limits-role-denied','GET','/api/doctorchart/limits/my-limits',role='DOCTOR')
        case('limits-all','GET','/api/doctorchart/limits/all')
        for plan in ['NONE','BASIC','PRO','UNLIMITED','FREE_TRIAL','ENTERPRISE','invalid']:
            case('plan-'+plan,'GET','/api/doctorchart/limits/my-limits',plan=plan,role='COMPANY')
        for path in ['/internal/webhooks/billing/limits-updated','/api/doctorchart/internal/webhooks/billing/limits-updated']:
            for secret in [None,'wrong','valid']:
                case('webhook-'+path+'-'+str(secret),'POST',path,auth='none',internal=secret)
        case('specialties','GET','/api/doctorchart/specialties')
        case('specialties-admin','GET','/api/doctorchart/admin/specialties')
        case('wizard-published','GET','/api/doctorchart/specialties/1/wizard')
        default=case('wizard-admin','GET','/api/doctorchart/admin/specialties/1/wizard')
        case('wizard-missing','GET','/api/doctorchart/specialties/999/wizard')
        case('wizard-invalid-id','GET','/api/doctorchart/specialties/not-a-number/wizard')
        for catalog in ['experience-navigation','experience-record-tabs','experience-record-actions']:
            case(catalog,'GET','/api/doctorchart/admin/specialties/'+catalog)
        experience=case('experience','GET','/api/doctorchart/admin/specialties/1/experience')
        case('experience-missing','GET','/api/doctorchart/admin/specialties/999/experience')
        case('workspace-missing','GET','/api/doctorchart/workspace')
        case('profile-missing','GET','/api/doctorchart/doctor/profile')
        case('prefs-missing','GET','/api/doctorchart/workspace/preferences')
        case('ai-onboarding-required','GET','/api/doctorchart/workspace/ai-access')
        base='/api/doctorchart/workspace'
        for payload in [{},{'specialtyId':1},{'specialtyId':1,'expectedVersion':0},{'specialtyId':999,'expectedVersion':None},{'specialtyId':'1','expectedVersion':None},{'specialtyId':1,'expectedVersion':0.0},{'specialtyId':1,'expectedVersion':None,'userId':'somebody'}]:
            case('select-invalid-'+str(len(records)),'PUT',base+'/specialty',payload)
        setup=case('select','PUT',base+'/specialty',{'specialtyId':1,'expectedVersion':None})
        case('draft-stale','PUT',base+'/wizard/draft',{'expectedVersion':99,'definition':{},'currentStepKey':'encounter'})
        case('discard-before-finish','POST',base+'/wizard/discard-draft',{'expectedVersion':setup['version']})
        case('prefs','GET',base+'/preferences')
        pref=case('notes-trim','PUT',base+'/quick-notes',{'content':'  recordatorio de prueba  '})
        case('notes-invalid-type','PUT',base+'/quick-notes',{'content':42})
        case('notes-too-long','PUT',base+'/quick-notes',{'content':'a'*10001})
        pref=case('notes-null','PUT',base+'/quick-notes',{'content':None})
        pref=case('notes-unicode','PUT',base+'/quick-notes',{'content':'😀'*5000})
        case('notes-unicode-too-long','PUT',base+'/quick-notes',{'content':'😀'*5001})
        pref=case('notes-control-trim','PUT',base+'/quick-notes',{'content':'\u0001 abc \u001f'})
        pref=case('prefs-save','PUT',base+'/preferences',{'expectedVersion':pref['version'],'patientColumnsConfig':{'columns':['fullName','birthDate']}})
        case('prefs-stale','PUT',base+'/preferences',{'expectedVersion':0,'patientColumnsConfig':{}})
        concurrent_body={'expectedVersion':pref['version'],'patientColumnsConfig':pref['patientColumnsConfig']}
        pairs=[]
        for port,db in zip([18083,18084],[source_db,target_db]):
            barrier=threading.Barrier(2)
            def concurrent_write():
                barrier.wait(timeout=10)
                return capture(port,cfg,'PUT',base+'/preferences',concurrent_body)
            # Hold a shared row lock until both transactions have read the old version
            # and attempted UPDATE. This aligns the DB interleaving across runtimes.
            locker=subprocess.Popen(['docker','exec','-i',cfg['container'],'psql','-U','migration','-d',db,'-v','ON_ERROR_STOP=1','-q','-t','-A'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            try:
                locker.stdin.write("BEGIN; SELECT id FROM doctor_workspaces WHERE user_id='fixture-doctor' FOR SHARE; SELECT 'locked';\n");locker.stdin.flush()
                while True:
                    marker=locker.stdout.readline().strip()
                    if marker=='locked':break
                    if locker.poll() is not None:raise RuntimeError('Cannot acquire fixture write barrier')
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
                    futures=[executor.submit(concurrent_write) for _ in range(2)]
                    deadline=time.monotonic()+10
                    try:
                        while time.monotonic()<deadline:
                            waiting=int(sql(cfg,db,"SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND lower(query) LIKE '%update%doctor_workspaces%';").strip())
                            if waiting>=2:break
                            time.sleep(.05)
                        else:raise RuntimeError('Both fixture updates did not reach the write barrier')
                    finally:
                        locker.stdin.write('COMMIT;\n\\q\n');locker.stdin.flush();locker.wait(timeout=10)
                    pairs.append(sorted([future.result() for future in futures],key=lambda value:value['status']))
            finally:
                if locker.poll() is None:locker.terminate();locker.wait(timeout=10)
                for stream in [locker.stdin,locker.stdout,locker.stderr]:stream.close()
        states=[state(cfg,db) for db in [source_db,target_db]]
        passed=pairs[0]==pairs[1] and [r['status'] for r in pairs[0]]==[200,409] and states[0]==states[1]
        records.append(dict(id='prefs-concurrent-same-version',method='PUT',path=base+'/preferences',spring=pairs[0],go=pairs[1],database_equal=states[0]==states[1],passed=passed))
        if not passed:print('DIFF prefs-concurrent-same-version',json.dumps(pairs,ensure_ascii=True)[:1800],flush=True)
        pref=case('prefs-after-concurrent','GET',base+'/preferences')
        for db in [source_db,target_db]:
            sql(cfg,db,"UPDATE doctor_workspaces SET created_at='2024-01-01T00:00:00.123450Z' WHERE user_id='fixture-doctor';")
        case('profile-instant-fraction-padding','GET','/api/doctorchart/doctor/profile')
        case('draft-invalid-step','PUT',base+'/wizard/draft',{'expectedVersion':pref['version'],'definition':setup['draftDefinition'],'currentStepKey':'bad'})
        case('draft-structure-invalid','PUT',base+'/wizard/draft',{'expectedVersion':pref['version'],'definition':{},'currentStepKey':'encounter'})
        setup=case('draft-save','PUT',base+'/wizard/draft',{'expectedVersion':pref['version'],'definition':setup['draftDefinition'],'currentStepKey':'encounter'})
        setup=case('finish','POST',base+'/finish-onboarding',{'expectedVersion':setup['version']})
        patient_values={'sys_firstName':'Ana','sys_lastName':'Prueba','sys_dni':'fixture-123','sys_dob':'2020-02-29','sys_sex':'FEMALE'}
        case('patient-values-missing','POST','/api/doctorchart/patients',{'workspaceVersion':setup['version'],'values':{}})
        case('patient-workspace-stale','POST','/api/doctorchart/patients',{'workspaceVersion':99,'values':patient_values})
        case('patient-no-subscription','POST','/api/doctorchart/patients',{'workspaceVersion':setup['version'],'values':patient_values},role='DOCTOR',plan='NONE')
        case('patient-future-dob','POST','/api/doctorchart/patients',{'workspaceVersion':setup['version'],'values':dict(patient_values,sys_dob='2099-01-01')})
        case('patient-unknown-value','POST','/api/doctorchart/patients',{'workspaceVersion':setup['version'],'values':dict(patient_values,callerOwnership='intruder')})
        patient=case('patient-create','POST','/api/doctorchart/patients',{'workspaceVersion':setup['version'],'values':patient_values})
        patient_id=patient['id']
        case('patient-read','GET',f'/api/doctorchart/patients/{patient_id}')
        case('patient-missing','GET','/api/doctorchart/patients/999')
        case('patient-other-owner','GET',f'/api/doctorchart/patients/{patient_id}',user='other-doctor')
        patient=case('patient-update','PUT',f'/api/doctorchart/patients/{patient_id}',{'expectedVersion':patient['version'],'values':dict(patient_values,sys_firstName='Ana María',sys_insurance='Cobertura de prueba')})
        case('patient-update-stale','PUT',f'/api/doctorchart/patients/{patient_id}',{'expectedVersion':0,'values':patient_values})
        case('growth-empty','GET',f'/api/doctorchart/patients/{patient_id}/measurements')
        case('growth-other-owner','GET',f'/api/doctorchart/patients/{patient_id}/measurements',user='other-doctor')
        case('growth-patient-missing','GET','/api/doctorchart/patients/999/measurements')
        encounter_base='/api/doctorchart/encounters'
        encounter_values={'chiefComplaint':'Control de prueba','weightKg':18.5,'heightCm':105,'headCircumferenceCm':49,'diagnosis':'Control','treatment':'Indicaciones'}
        encounter_request={'patientId':patient_id,'workspaceVersion':setup['version'],'consultationType':'FIRST_VISIT','values':encounter_values}
        case('encounter-no-first-visit','POST',encounter_base,dict(encounter_request,consultationType='FOLLOW_UP'))
        case('encounter-caller-date-forbidden','POST',encounter_base,dict(encounter_request,encounterDate='2024-03-01T15:00:00Z'))
        case('encounter-invalid-type','POST',encounter_base,dict(encounter_request,consultationType='CHECKUP'))
        case('encounter-workspace-stale','POST',encounter_base,dict(encounter_request,workspaceVersion=999))
        case('encounter-no-subscription','POST',encounter_base,encounter_request,role='DOCTOR',plan='NONE')
        case('encounter-missing-required','POST',encounter_base,dict(encounter_request,values={}))
        encounter=case('encounter-create','POST',encounter_base,encounter_request,clock_field='encounterDate')
        encounter_id=encounter['id']
        encounter_update={'expectedVersion':encounter['version'],'encounterDate':'2024-03-01T12:00:00-03:00','consultationType':'FIRST_VISIT','values':encounter_values}
        encounter=case('encounter-update-date','PUT',encounter_base+f'/{encounter_id}',encounter_update)
        case('encounter-first-visit-duplicate','POST',encounter_base,encounter_request)
        case('encounter-read','GET',encounter_base+f'/{encounter_id}')
        case('encounter-missing','GET',encounter_base+'/999')
        case('encounter-other-owner','GET',encounter_base+f'/{encounter_id}',user='other-doctor')
        case('encounter-update-stale','PUT',encounter_base+f'/{encounter_id}',encounter_update)
        case('encounter-update-future','PUT',encounter_base+f'/{encounter_id}',dict(encounter_update,expectedVersion=encounter['version'],encounterDate='2099-01-01T00:00:00Z'))
        second=case('encounter-followup-create','POST',encounter_base,dict(encounter_request,consultationType='FOLLOW_UP'),clock_field='encounterDate')
        second_update=dict(encounter_update,expectedVersion=second['version'],encounterDate='2024-04-01T12:00:00Z',consultationType='FOLLOW_UP',values=dict(encounter_values,weightKg=20,chiefComplaint='Segundo control'))
        second=case('encounter-followup-update','PUT',encounter_base+f'/{second["id"]}',second_update)
        case('encounter-convert-duplicate-first','PUT',encounter_base+f'/{second["id"]}',dict(second_update,expectedVersion=second['version'],consultationType='FIRST_VISIT'))
        for suffix in ['', '?search=control','?search=no-match','?size=1&sort=id,asc','?page=2&size=1','?sort=fullName','?size=101','?sort=encounterDate,ignorecase']:
            case('encounter-directory'+suffix,'GET',encounter_base+suffix)
        case('encounter-patient','GET',encounter_base+f'/patient/{patient_id}')
        case('encounter-patient-specialty','GET',encounter_base+f'/patient/{patient_id}/specialty/1')
        case('encounter-patient-other-specialty','GET',encounter_base+f'/patient/{patient_id}/specialty/999')
        case('encounter-patient-other-owner','GET',encounter_base+f'/patient/{patient_id}',user='other-doctor')
        case('encounter-dashboard','GET',encounter_base+'/dashboard')
        case('encounter-dashboard-other-owner','GET',encounter_base+'/dashboard',user='other-doctor')
        case('patient-after-encounters','GET',f'/api/doctorchart/patients/{patient_id}')
        case('growth-history','GET',f'/api/doctorchart/patients/{patient_id}/measurements')
        # Existing-data fixture: birth measurements cannot be written through the migrated form API.
        for db in [source_db,target_db]:
            sql(cfg,db,"UPDATE patients SET personal_history='{\"birthMeasurements\":{\"weightKg\":3.2,\"heightCm\":49.5,\"headCircumferenceCm\":34}}'::jsonb WHERE id="+str(int(patient_id))+";")
        case('growth-birth-baseline','GET',f'/api/doctorchart/patients/{patient_id}/measurements')
        case('encounter-delete-other-owner','DELETE',encounter_base+f'/{second["id"]}',user='other-doctor')
        case('encounter-delete','DELETE',encounter_base+f'/{second["id"]}')
        case('encounter-delete-missing','DELETE',encounter_base+f'/{second["id"]}')
        prescriptions=f'/api/doctorchart/patients/{patient_id}/prescriptions'
        medication={'medicationName':' Medicamento de prueba ','dosage':' 5 mg ','administrationRoute':' Oral ','frequency':' Cada 8 horas ','durationDays':5,'instructions':' Con agua '}
        prescription_request={'type':'PRESCRIPTION','diagnosisDescription':' Diagnóstico de prueba ','generalInstructions':' Indicaciones generales ','prescriptions':[medication,dict(medication,medicationName='Segundo medicamento',durationDays=None)],'diagnosticOrders':[' Estudio A ','Estudio B']}
        case('prescription-empty','GET',prescriptions)
        case('prescription-role-denied','GET',prescriptions,role='VIEWER')
        case('prescription-other-owner','GET',prescriptions,user='other-doctor')
        case('prescription-patient-missing','GET','/api/doctorchart/patients/999/prescriptions')
        for variant,payload in [('invalid-type',dict(prescription_request,type='OTHER')),('empty-medications',dict(prescription_request,prescriptions=[])),('missing-orders',{k:v for k,v in prescription_request.items() if k!='diagnosticOrders'}),('unknown-field',dict(prescription_request,authorUserId='intruder')),('bad-duration',dict(prescription_request,prescriptions=[dict(medication,durationDays=0)])),('fractional-duration',dict(prescription_request,prescriptions=[dict(medication,durationDays=1.5)])),('duration-overflow',dict(prescription_request,prescriptions=[dict(medication,durationDays=2147483648)])),('bad-item',dict(prescription_request,prescriptions=['medicine'])),('empty-order',dict(prescription_request,diagnosticOrders=[' ']))]:
            case('prescription-'+variant,'POST',prescriptions,payload)
        prescription=case('prescription-create','POST',prescriptions,prescription_request,clock_field='createdAt')
        prescription_id=prescription['id']
        case('prescription-read','GET',prescriptions+f'/{prescription_id}')
        case('prescription-missing','GET',prescriptions+'/999')
        orders=case('prescription-study-orders-create','POST',prescriptions,dict(prescription_request,type='STUDY_ORDERS',prescriptions=[]),clock_field='createdAt')
        case('prescription-list-ordered','GET',prescriptions)
        case('prescription-delete-other-owner','DELETE',prescriptions+f'/{orders["id"]}',user='other-doctor')
        case('prescription-delete','DELETE',prescriptions+f'/{orders["id"]}')
        case('prescription-delete-missing','DELETE',prescriptions+f'/{orders["id"]}')
        for suffix in ['', '?search=Ana', '?filter=ADULT', '?filter=MINOR', '?filter=INFANT', '?filter=TODDLER', '?filter=VACCINE_PENDING', '?size=1&sort=birthDate,desc', '?page=3&size=1', '?size=101', '?sort=unavailable', '?filter=invalid', '?search=%25', '?size=0', '?page=invalid']:
            case('patient-directory'+suffix,'GET','/api/doctorchart/patients'+suffix)
        schedule=case('vaccination-patient','GET',f'/api/doctorchart/patients/{patient_id}/vaccinations')
        dose=schedule['doses'][0]['id']
        vaccine_request={'expectedVersion':patient['version'],'calendarId':'argentina-2026-v1','applied':True,'appliedAt':'2021-03-01'}
        saved=case('vaccination-apply','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/{dose}',vaccine_request)
        patient=saved['patient']
        case('vaccination-stale','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/{dose}',vaccine_request)
        vaccine_request['expectedVersion']=patient['version']
        case('vaccination-dose-missing','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/unknown',vaccine_request)
        case('vaccination-before-birth','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/{dose}',dict(vaccine_request,appliedAt='2019-01-01'))
        case('vaccination-calendar-changed','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/{dose}',dict(vaccine_request,calendarId='unknown'))
        case('vaccination-invalid-unapply','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/{dose}',dict(vaccine_request,applied=False))
        saved=case('vaccination-unapply','PUT',f'/api/doctorchart/patients/{patient_id}/vaccinations/{dose}',dict(vaccine_request,applied=False,appliedAt=None))
        patient=saved['patient']
        case('specialty-locked','PUT',base+'/specialty',{'expectedVersion':setup['version'],'specialtyId':1})
        case('ai-feature','GET',base+'/ai-access')
        case('medications','GET','/api/doctorchart/medications')
        for query in ['','ÁCIDO','antibiotico','ibuprofeno','no-match']:
            case('medications-search-'+query,'GET','/api/doctorchart/medications/search?query='+urllib.parse.quote(query))
        for limit in ['0','100','invalid','2147483648']:
            case('medications-limit-'+limit,'GET','/api/doctorchart/medications/search?limit='+limit)
        case('vaccination-calendar','GET','/api/doctorchart/vaccination-calendar')
        case('charts','GET','/api/doctorchart/charts')
        case('chart-configs-empty','GET',base+'/charts')
        chart_request=[{'chartCode':'WEIGHT_EVOLUTION','fieldMappings':{'x':'encounterDate','y':'weightKg'},'isActive':True}]
        case('chart-config-create','PUT',base+'/charts',chart_request)
        chart_request[0]['fieldMappings']['y']='heightCm'
        case('chart-config-update','PUT',base+'/charts',chart_request)
        case('chart-configs-read','GET',base+'/charts')
        case('chart-configs-other-doctor','GET',base+'/charts',user='other-doctor')
        case('chart-config-transaction-rollback','PUT',base+'/charts',[{'chartCode':'ROLLBACK_PROBE','fieldMappings':{'x':'encounterDate','y':'weightKg'},'isActive':True},{'chartCode':'x'*1000,'fieldMappings':{},'isActive':True}])
        case('chart-config-after-rollback','GET',base+'/charts')
        calculators='/api/doctorchart/calculators'
        case('calculator-catalog','GET',calculators+'/catalog')
        case('calculator-preferences-create','GET',calculators+'/preferences',calculator_clock='updatedAt')
        case('calculator-preferences-read','GET',calculators+'/preferences')
        for codes in [[],['imc','imc'],['unknown'],[' IMC '],[''],[None],['imc','tfge']]:
            case('calculator-preferences-'+str(codes),'PUT',calculators+'/preferences',{'favoriteCalculationCodes':codes},calculator_clock='updatedAt' if codes==['imc','tfge'] else None)
        case('calculator-preferences-reorder','PUT',calculators+'/preferences',{'favoriteCalculationCodes':['tfge','imc']},calculator_clock='updatedAt')
        case('calculator-preferences-reset','POST',calculators+'/preferences/reset',calculator_clock='updatedAt')
        examples={
            'imc':{'weightKg':70,'heightCm':170},
            'cockcroft':{'weightKg':70,'ageYears':50,'creatinineMgDl':1.2,'gender':'MALE'},
            'tfge':{'ageYears':50,'creatinineMgDl':1.2,'gender':'FEMALE'},
            'calcio_corregido':{'serumCalciumMgDl':8,'serumAlbuminGDl':3},
            'bsa_mosteller':{'weightKg':70,'heightCm':170},
            'holliday_segar':{'weightKg':20},
            'pediatric_dosage':{'weightKg':12,'targetDoseMgKg':50,'doseUnit':'mg/kg/dia','drugConcentrationMg':250,'drugConcentrationMl':5,'dosesPerDay':3},
            'chads_vasc':{'ageYears':75,'isFemale':True,'congestiveHeartFailure':True,'hypertension':True,'diabetes':True,'strokeOrTia':True,'vascularDisease':True}}
        for code,inputs in examples.items():
            case('calculator-preview-'+code,'POST',calculators+'/preview',{'calculationCode':code,'inputSnapshot':inputs},calculator_clock='calculatedAt')
            for bad in [dict(inputs,extra=1),{},dict(inputs,**{next(iter(inputs)):-1}),dict(inputs,**{next(iter(inputs)):'1'})]:
                case('calculator-invalid-'+code+'-'+str(len(records)),'POST',calculators+'/preview',{'calculationCode':code,'inputSnapshot':bad})
        for weight in [10,10.01,20,20.01,1e-8,70.05]:
            case('calculator-fluid-boundary-'+str(weight),'POST',calculators+'/preview',{'calculationCode':'holliday_segar','inputSnapshot':{'weightKg':weight}},calculator_clock='calculatedAt')
        for age in [18,65,120]:
            for gender in ['MALE','FEMALE']:
                for creatinine in [.1,.7,.9,2,6]:
                    case(f'calculator-renal-{age}-{gender}-{creatinine}','POST',calculators+'/preview',{'calculationCode':'tfge','inputSnapshot':{'ageYears':age,'gender':gender,'creatinineMgDl':creatinine}},calculator_clock='calculatedAt')
        for calcium in [8.49,8.5,10.5,10.51,12,12.01,0.1]:
            case('calculator-calcium-boundary-'+str(calcium),'POST',calculators+'/preview',{'calculationCode':'calcio_corregido','inputSnapshot':{'serumCalciumMgDl':calcium,'serumAlbuminGDl':4}},calculator_clock='calculatedAt')
        for bad in [{},{'calculationCode':'unknown','inputSnapshot':{}},{'calculationCode':'imc','inputSnapshot':{},'rawValue':1}]:
            case('calculator-request-invalid-'+str(len(records)),'POST',calculators+'/preview',bad)
        for endpoint in ['catalog','preferences',f'patients/{patient_id}/history',f'patients/{patient_id}/autofill']:
            case('calculator-owner-'+endpoint,'GET',calculators+'/'+endpoint,user='other-doctor')
        execution={'patientId':patient_id,'encounterId':encounter_id,'calculationCode':'imc','inputSnapshot':examples['imc']}
        for override in [{'patientId':999},{'encounterId':999},{'encounterId':None,'inputSnapshot':{}},{'encounterId':-1}]:
            case('calculator-execute-invalid-'+str(len(records)),'POST',calculators+'/execute',dict(execution,**override))
        case('calculator-execute','POST',calculators+'/execute',execution,calculator_clock='createdAt')
        case('calculator-history','GET',calculators+f'/patients/{patient_id}/history')
        case('calculator-history-filter','GET',calculators+f'/patients/{patient_id}/history?calculationCode=IMC&fromDate=2026-10-08T12:00:00Z&toDate=2026-10-08T12:00:00Z')
        case('calculator-history-empty','GET',calculators+f'/patients/{patient_id}/history?calculationCode=tfge')
        case('calculator-autofill','GET',calculators+f'/patients/{patient_id}/autofill')
        studies=f'/api/doctorchart/patients/{patient_id}/studies'
        case('patient-delete-missing','DELETE','/api/doctorchart/patients/999')
        case('patient-delete-owner','DELETE',f'/api/doctorchart/patients/{patient_id}',user='other-doctor')
        case('studies-empty','GET',studies)
        case('study-owner','GET',studies,user='other-doctor')
        case('study-role','GET',studies,role='UNKNOWN')
        for name,data,description in [('a.png',b'',None),('a.pdf',b'%PDF-1.7\nnot a PDF',None),('a.jpg',image_fixture(),None),('../a.png',image_fixture(),None),('a:png',image_fixture(),None),('a.png',image_fixture(),'a'*4001)]:
            case('study-upload-invalid-'+str(len(records)),'POST',studies,multipart(name,data,description))
        uploaded=case('study-upload-png','POST',studies,multipart('estudio clínico.png',image_fixture(),'  registro de prueba  '),study_clock=True)
        study_id=uploaded.get('id',999)
        case('study-list','GET',studies)
        case('study-detail','GET',studies+f'/{study_id}')
        case('study-download','GET',studies+f'/{study_id}/file')
        case('study-download-range','GET',studies+f'/{study_id}/file',extra_headers={'Range':'bytes=0-7'})
        case('study-not-found','GET',studies+'/999')
        case('study-download-owner','GET',studies+f'/{study_id}/file',user='other-doctor')
        case('study-delete','DELETE',studies+f'/{study_id}')
        case('study-deleted-detail','GET',studies+f'/{study_id}')
        case('study-after-delete','GET',studies)
        uploaded=case('study-upload-pdf','POST',studies,multipart('laboratorio.pdf',pdf_fixture()),study_clock=True)
        study_id=uploaded.get('id',999)
        case('study-download-pdf','GET',studies+f'/{study_id}/file')
        deadline=time.monotonic()+32
        while any((directory/old_key).exists() for directory,old_key,_ in maintenance_fixtures) and time.monotonic()<deadline:
            time.sleep(.1)
        for directory,old_key,fresh_key in maintenance_fixtures:
            if (directory/old_key).exists() or not (directory/fresh_key).exists():
                raise RuntimeError('Scheduled orphan reconciliation or 24h grace period differs')
        transient=case('delete-fixture-patient-create','POST','/api/doctorchart/patients',{'workspaceVersion':setup['version'],'values':dict(patient_values,sys_dni='deletion-fixture')})
        deleted_id=transient['id']
        child=case('delete-fixture-encounter','POST',encounter_base,dict(encounter_request,patientId=deleted_id),clock_field='encounterDate')
        case('delete-fixture-encounter-date','PUT',encounter_base+'/'+str(child['id']),{'expectedVersion':child['version'],'encounterDate':'2024-03-01T15:00:00Z','consultationType':'FIRST_VISIT','values':encounter_values})
        case('delete-fixture-prescription','POST',f'/api/doctorchart/patients/{deleted_id}/prescriptions',prescription_request,clock_field='createdAt')
        case('delete-fixture-calculation','POST',calculators+'/execute',dict(execution,patientId=deleted_id,encounterId=child['id']),calculator_clock='createdAt')
        case('delete-fixture-study','POST',f'/api/doctorchart/patients/{deleted_id}/studies',multipart('borrar.png',image_fixture()),study_clock=True)
        case('patient-delete-cascade','DELETE',f'/api/doctorchart/patients/{deleted_id}')
        case('patient-delete-twice','DELETE',f'/api/doctorchart/patients/{deleted_id}')
        for index,db in enumerate([source_db,target_db]):
            remaining=int(sql(cfg,db,f"SELECT (SELECT count(*) FROM patients WHERE id={deleted_id})+(SELECT count(*) FROM clinical_encounters WHERE patient_id={deleted_id})+(SELECT count(*) FROM patient_calculation_logs WHERE patient_id={deleted_id})+(SELECT count(*) FROM patient_medical_studies WHERE patient_id={deleted_id})+(SELECT count(*) FROM patient_prescription_records WHERE patient_id={deleted_id});").strip())
            if remaining:raise RuntimeError('Patient cascade retained clinical rows')
            directory=__import__('pathlib').Path(tempfile.gettempdir())/cfg['container']/('studies-'+db)
            for key in study_keys[index]:
                referenced=sql(cfg,db,"SELECT count(*) FROM patient_medical_studies WHERE stored_file_name='"+key+"';").strip()!='0'
                if not referenced and (directory/key).exists():raise RuntimeError('Committed deletion retained a private attachment')
        print_base=f'/api/doctorchart/patients/{patient_id}'
        case('prescription-print','GET',prescriptions+f'/{prescription_id}/print')
        case('encounter-print-empty','GET',print_base+f'/encounters/{encounter_id}/print')
        case('print-prescription-missing','GET',prescriptions+'/999/print')
        case('print-encounter-missing','GET',print_base+'/encounters/999/print')
        case('print-prescription-owner','GET',prescriptions+f'/{prescription_id}/print',user='other-doctor')
        case('print-prescription-role','GET',prescriptions+f'/{prescription_id}/print',role='UNKNOWN')
        # Existing clinical document projections are read from stored data, never from print query params.
        for db in [source_db,target_db]:
            sql(cfg,db,"UPDATE clinical_encounters SET prescriptions='[{\"medicationName\":\"Medicamento clínico\",\"dosage\":\"400 mg\",\"administrationRoute\":\"Oral\",\"frequency\":\"Cada 8 horas\",\"durationDays\":5,\"instructions\":\"Con alimentos\"}]'::jsonb,diagnostic_orders='[\"Laboratorio clínico\",\"Estudio de seguimiento\"]'::jsonb,primary_diagnosis_code='Z00',primary_diagnosis_description='Diagnóstico de prueba',follow_up_instructions='Indicaciones para el paciente' WHERE id="+str(int(encounter_id))+";")
        case('encounter-print','GET',print_base+f'/encounters/{encounter_id}/print')
        metadata='?doctorName=Dra.+Prueba&license=MP+123&specialty=Pediatr%C3%ADa&clinicName=Cl%C3%ADnica+de+Prueba&clinicAddress=Calle+123&clinicPhone=555-0100'
        for name,path in [('prescription',prescriptions+f'/{prescription_id}'),('encounter',print_base+f'/encounters/{encounter_id}')]:
            case('pdf-'+name,'GET',path+'/pdf',pdf_semantic=True)
            case('pdf-'+name+'-metadata','GET',path+'/pdf'+metadata,pdf_semantic=True)
            case('pdf-'+name+'-orders','GET',path+'/pdf'+metadata+'&type=STUDY_ORDERS',pdf_semantic=True)
        setup=case('reset','POST',base+'/reset-to-defaults',{'expectedVersion':setup['version']})
        setup=case('discard','POST',base+'/wizard/discard-draft',{'expectedVersion':setup['version']})
        case('finish-no-draft','POST',base+'/finish-onboarding',{'expectedVersion':setup['version']})
        case('workspace-other-user','GET',base,user='other-doctor')
        admin='/api/doctorchart/admin/specialties'
        case('last-active','PATCH',admin+'/1',{'expectedVersion':experience['specialtyVersion'],'active':False})
        specialty=case('create-draft','POST',admin,{'code':'fixture_specialty','name':'Especialidad de prueba','definition':default['draftDefinition']})
        newID=specialty['id']
        case('duplicate-code','POST',admin,{'code':'fixture_specialty','name':'Especialidad de prueba','definition':default['draftDefinition']})
        case('activate-unpublished','PATCH',admin+'/'+str(newID),{'expectedVersion':0,'active':True})
        case('save-default','PUT',admin+f'/{newID}/wizard/draft',{'expectedVersion':0,'definition':default['draftDefinition']})
        case('publish','POST',admin+f'/{newID}/wizard/publish',{'expectedVersion':1})
        case('publish-stale','POST',admin+f'/{newID}/wizard/publish',{'expectedVersion':1})
        specialty=case('patch-specialty','PATCH',admin+f'/{newID}',{'expectedVersion':1,'name':'Nombre actualizado','description':'fixture'})
        experience=case('experience-navigation-save','PUT',admin+f'/{newID}/experience',{'expectedVersion':specialty['version'],'navigation':['DASHBOARD','PATIENTS','WIZARD']})
        experience=case('experience-tabs-save','PUT',admin+f'/{newID}/experience/patient-record-tabs',{'expectedVersion':experience['specialtyVersion'],'patientRecordTabs':['SUMMARY','ENCOUNTERS','STUDIES']})
        experience=case('experience-actions-empty','PUT',admin+f'/{newID}/experience/patient-record-actions',{'expectedVersion':experience['specialtyVersion'],'patientRecordActions':[]})
        case('experience-invalid','PUT',admin+f'/{newID}/experience',{'expectedVersion':experience['specialtyVersion'],'navigation':['DASHBOARD','DASHBOARD']})
        for variant in ['missing-core','unknown-property','wrong-system-type','gender-option','range','visibility-cycle','unknown-field-type','malformed-formula']:
            definition=copy.deepcopy(default['draftDefinition'])
            if variant=='missing-core':definition['patientForm']['categories']=[]
            elif variant=='unknown-property':definition['extra']='unexpected'
            elif variant=='wrong-system-type':definition['patientForm']['categories'][0]['fields'][3]['type']='TEXT'
            elif variant=='gender-option':definition['patientForm']['categories'][0]['fields'][4]['options'][0]['value']='invented'
            else:
                fields=definition['encounterForm']['categories'][0]['fields']
                if variant=='range':fields.append({'key':'fixture_range','label':'fixture','type':'NUMBER','required':False,'minimum':101,'maximum':100})
                elif variant=='visibility-cycle':fields.append({'key':'fixture_cycle','label':'fixture','type':'BOOLEAN','required':False,'visibleWhen':{'fieldKey':'fixture_cycle','equals':True}})
                elif variant=='unknown-field-type':fields[0]['type']='NATIVE_DATE'
                else:fields.append({'key':'fixture_formula','label':'fixture','type':'FORMULA','required':False,'formula':{'kind':'CUSTOM','operation':'ADD','inputs':{'a':{'fieldKey':'nonexistent','constant':1},'b':{'constant':2}}}})
            case('definition-'+variant,'PUT',admin+f'/{newID}/wizard/draft',{'expectedVersion':2,'definition':definition})
        schema_pass=before==schema(cfg,target_db)
        # Run original Spring against Go-written data. All DDL disabled on this restart.
        processes[0].terminate();processes[0].wait(timeout=20)
        old_setting=os.environ.get('SPRING_JPA_HIBERNATE_DDL_AUTO')
        os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']='validate'
        processes[0]=start_spring(cfg,target_db)
        rollback_started=time.monotonic()
        if old_setting is None:os.environ.pop('SPRING_JPA_HIBERNATE_DDL_AUTO',None)
        else:os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']=old_setting
        rollback=[]
        for path in [base,base+'/preferences',admin,admin+f'/{newID}/wizard',admin+f'/{newID}/experience','/api/doctorchart/doctor/profile',f'/api/doctorchart/patients/{patient_id}',base+'/charts','/api/doctorchart/vaccination-calendar',f'/api/doctorchart/patients/{patient_id}/vaccinations','/api/doctorchart/patients',encounter_base+f'/{encounter_id}',encounter_base,encounter_base+'/dashboard',prescriptions,prescriptions+f'/{prescription_id}',f'/api/doctorchart/patients/{patient_id}/measurements']:
            reference=capture(18083,cfg,'GET',path);target=capture(18084,cfg,'GET',path)
            rollback.append(dict(path=path,passed=reference==target,spring=reference,go=target))
        for path in [calculators+'/catalog',calculators+'/preferences',calculators+f'/patients/{patient_id}/history',calculators+f'/patients/{patient_id}/autofill',studies,studies+f'/{study_id}',studies+f'/{study_id}/file',prescriptions+f'/{prescription_id}/print',print_base+f'/encounters/{encounter_id}/print',prescriptions+f'/{prescription_id}/pdf']:
            reference=capture(18083,cfg,'GET',path);target=capture(18084,cfg,'GET',path)
            compared=copy.deepcopy([reference,target])
            if path.endswith('/pdf'):
                for response in compared:response['body']=pdf_content(base64.b64decode(response['body']['binaryBase64']))
            rollback.append(dict(path=path,passed=compared[0]==compared[1],spring=reference,go=target))
        spring_write=capture(18083,cfg,'PUT',base+'/quick-notes',{'content':'Spring after Go write'})
        go_read=capture(18084,cfg,'GET',base+'/preferences')
        rollback.append(dict(path='Spring write → Go read',passed=spring_write==go_read,spring=spring_write,go=go_read))
        go_write=capture(18084,cfg,'PUT',base+'/quick-notes',{'content':'Go after Spring write'})
        spring_read=capture(18083,cfg,'GET',base+'/preferences')
        rollback.append(dict(path='Go write → Spring read',passed=go_write==spring_read,spring=spring_read,go=go_write))
        for writer,reader,label in [(18083,18084,'Spring → Go'),(18084,18083,'Go → Spring')]:
            if time.monotonic()-rollback_started>25:raise RuntimeError('Rollback exceeded the coordinated scheduler window')
            path=f'/api/doctorchart/patients/{patient_id}'
            current=capture(writer,cfg,'GET',path)['body']
            payload={'expectedVersion':current['version'],'values':dict(current['formValues'],sys_firstName='Paciente '+label)}
            written=capture(writer,cfg,'PUT',path,payload)
            read=capture(reader,cfg,'GET',path)
            rollback.append(dict(path='Patient write/read '+label,passed=written['status']==200 and written==read,writer=written,reader=read))
            path=encounter_base+f'/{encounter_id}'
            current=capture(writer,cfg,'GET',path)['body']
            payload={'expectedVersion':current['version'],'encounterDate':'2024-03-01T15:00:00Z','consultationType':'FIRST_VISIT','values':dict(current['formValues'],chiefComplaint='Consulta '+label)}
            written=capture(writer,cfg,'PUT',path,payload)
            read=capture(reader,cfg,'GET',path)
            rollback.append(dict(path='Encounter write/read '+label,passed=written['status']==200 and written==read,writer=written,reader=read))
            written=capture(writer,cfg,'POST',prescriptions,prescription_request)
            if written['status']==201:
                path=prescriptions+f'/{written["body"]["id"]}'
                origin_read=capture(writer,cfg,'GET',path)
                read=capture(reader,cfg,'GET',path)
                passed=origin_read==read
            else:origin_read=written;read={};passed=False
            rollback.append(dict(path='Prescription write/read '+label,passed=passed,writer=written,origin_read=origin_read,reader=read))
            written=capture(writer,cfg,'POST',calculators+'/execute',execution)
            source_read=capture(writer,cfg,'GET',calculators+f'/patients/{patient_id}/history')
            target_read=capture(reader,cfg,'GET',calculators+f'/patients/{patient_id}/history')
            rollback.append(dict(path='Calculation write/read '+label,passed=written['status']==201 and source_read==target_read,writer=written,reader=target_read))
            written=capture(writer,cfg,'PUT',calculators+'/preferences',{'favoriteCalculationCodes':['imc','tfge']})
            source_read=capture(writer,cfg,'GET',calculators+'/preferences');target_read=capture(reader,cfg,'GET',calculators+'/preferences')
            rollback.append(dict(path='Calculator preferences write/read '+label,passed=written['status']==200 and source_read==target_read,writer=written,reader=target_read))
            written=capture(writer,cfg,'POST',studies,multipart('compartido.png',image_fixture()))
            if written['status']==201:
                path=studies+f'/{written["body"]["id"]}'
                source_read=capture(writer,cfg,'GET',path);target_read=capture(reader,cfg,'GET',path)
                passed=source_read==target_read and capture(writer,cfg,'GET',path+'/file')==capture(reader,cfg,'GET',path+'/file')
            else:passed=False;target_read={}
            rollback.append(dict(path='Study/file write/read '+label,passed=passed,writer=written,reader=target_read))
        if time.monotonic()-rollback_started>29:raise RuntimeError('Rollback overlapped the restarted source scheduler')
        schema_pass=schema_pass and before==schema(cfg,target_db)
        summary=dict(cases=len(records),passed=sum(r['passed'] for r in records),failed=[r['id'] for r in records if not r['passed']],schema_unchanged=schema_pass,rollback_reads=rollback,fixture=cfg['container'],databases=[source_db,target_db],timestamp_policy='audit times excluded from row equality; server issuance clocks checked within observed request intervals and persisted to 1 microsecond; explicit encounter dates compared exactly; full time/monotonicity semantics still UNVERIFIED')
        (ROOT/'migration/evidence').mkdir(exist_ok=True)
        (ROOT/'migration/evidence/compare.json').write_text(json.dumps(dict(summary=summary,cases=records),ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        print(json.dumps({key:value for key,value in summary.items() if key!='rollback_reads'}),flush=True)
        print('Rollback reads:',sum(row['passed'] for row in rollback),'/',len(rollback),flush=True)
        if summary['failed'] or not schema_pass or not all(r['passed'] for r in rollback):
            raise SystemExit(1)
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate();process.wait(timeout=20)
        billing.shutdown();billing.server_close()

if __name__=='__main__':
    main()
