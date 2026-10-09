"""Directed real-process clinical/security checks; no Java differential rerun."""
import base64
import concurrent.futures
import hashlib
import io
import json
from pathlib import Path
import re
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'migration'))
from fixture import ROOT, WORK, command, sql
from compare import multipart, pdf_fixture
from verify_documents import picture
from pypdf import PdfReader, PdfWriter

LOCAL=WORK/'integration'; EVIDENCE=ROOT/'integration/evidence'
CONTEXT=json.loads((LOCAL/'context.json').read_text()); CFG=CONTEXT['fixture']; DB=CONTEXT['database']
RESULTS=[]; TOKENS={}; USERS={}

def login(label):
    email = label+'@fixture.test' if label in ['admin','pointly'] else 'doc-'+label+'@fixture.test'
    req=urllib.request.Request('https://localhost:19443/auth/v1/token?grant_type=password',
        data=json.dumps({'email':email,'password':'FixtureOnly123!'}).encode(),headers={'Content-Type':'application/json'})
    with urllib.request.urlopen(req,context=ssl.create_default_context(cafile=LOCAL/'ca.pem')) as r: data=json.load(r)
    TOKENS[label]=data['access_token']; USERS[label]=data['user']['id']; return data

def call(name,method,path,payload=None,who='a',expected=200,check=None,headers=None,direct=False):
    fields={}; body=None
    if who: fields['Authorization']='Bearer '+TOKENS[who]
    if direct:
        fields={'X-Gateway-Secret':CFG['gateway'],'X-User-Id':USERS.get(who,who),'X-User-Role':'DOCTOR',
                'X-User-Plan':'BASIC','X-User-Plan-Expires-At':'2099-01-01T00:00:00Z'}
    if headers: fields.update(headers)
    if isinstance(payload,tuple): body,fields['Content-Type']=payload
    elif payload is not None: body=json.dumps(payload,ensure_ascii=False).encode(); fields['Content-Type']='application/json'
    req=urllib.request.Request('http://127.0.0.1:'+str(18484 if direct else 18080)+path,data=body,method=method,headers=fields)
    try: r=urllib.request.urlopen(req,timeout=40)
    except urllib.error.HTTPError as error: r=error
    with r:
        raw=r.read(); response_headers=dict(r.headers.items()); status=r.status
    try: data=json.loads(raw)
    except (ValueError,UnicodeDecodeError): data=raw
    passed=status in (expected if isinstance(expected,list) else [expected]) and (check(data,response_headers) if check else True)
    RESULTS.append({'case':name,'through':'trusted-Go-boundary' if direct else 'actual-gateway','method':method,'path':path,
                    'status':status,'passed':bool(passed),'response_headers':{key:value for key,value in response_headers.items() if key.lower() in ['cache-control','pragma','expires','content-type','content-length','content-range','access-control-allow-origin','access-control-expose-headers']},'body':data if not isinstance(data,bytes) else {'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}})
    if not passed: print('FAIL:',name,'status',status,flush=True)
    return data

def control(**values):
    req=urllib.request.Request('http://localhost:19200/__fixture/control',data=json.dumps(values).encode(),headers={'Content-Type':'application/json'})
    with urllib.request.urlopen(req) as r: assert r.status==200

def restart():
    previous=json.loads((LOCAL/'context.json').read_text())['doctorchart_pid']
    (LOCAL/'restart-backend').touch()
    for _ in range(80):
        time.sleep(.25)
        if json.loads((LOCAL/'context.json').read_text())['doctorchart_pid']!=previous: return
    raise RuntimeError('Owned Go restart failed')

def pdf_check(name,data,required):
    path=EVIDENCE/(name+'.pdf'); path.write_bytes(data)
    reader=PdfReader(io.BytesIO(data)); text='\n'.join(page.extract_text() for page in reader.pages)
    missing=[value for value in required if value not in text]
    a4=all(abs(float(page.mediabox.width)-595)<1 and abs(float(page.mediabox.height)-842)<1 for page in reader.pages)
    RESULTS.append({'case':name+'-clinical-content','pages':len(reader.pages),'a4':a4,'missing':missing,'passed':not missing and a4})
    return text,reader

def main():
    for label in ['a','b','browser','admin','pointly','expired','inactive','invalid','missing-expiry','string-active']: login(label)
    base='/api/doctorchart'
    call('public-health','GET',base+'/health',who=None)
    call('missing-session','GET',base+'/patients',who=None,expected=401)
    call('forged-browser-identity','GET',base+'/patients',who=None,expected=401,
         headers={'X-Gateway-Secret':CFG['gateway'],'X-User-Id':USERS['a'],'X-User-Role':'SUPER_ADMIN','X-User-Plan':'PRO','X-User-Plan-Expires-At':'2099-01-01T00:00:00Z'})
    call('invalid-jwt','GET',base+'/patients',who=None,expected=401,headers={'Authorization':'Bearer invalid-owned-token'})
    for label in ['pointly','expired','inactive','invalid','missing-expiry','string-active']:
        call('benefit-'+label,'GET',base+'/patients',who=label,expected=403,check=lambda d,h:d.get('code')=='DOCTORCHART_PLAN_REQUIRED')
    call('forged-role-with-valid-session','GET',base+'/admin/specialties',expected=403,headers={'X-User-Role':'SUPER_ADMIN'})
    call('forged-benefit-with-expired-entitlement','GET',base+'/patients',who='expired',expected=403,
         headers={'X-User-Plan':'PRO','X-User-Plan-Expires-At':'2099-01-01T00:00:00Z'})
    from cryptography.hazmat.primitives import hashes,serialization
    from cryptography.hazmat.primitives.asymmetric import padding
    key=serialization.load_pem_private_key((LOCAL/'key.pem').read_bytes(),password=None)
    encode=lambda raw:base64.urlsafe_b64encode(raw).rstrip(b'=').decode()
    header,payload,_=TOKENS['a'].split('.')
    claims=json.loads(base64.urlsafe_b64decode(payload+'='*((-len(payload))%4)))
    for label,invalid in [('expired-jwt',{'exp':int(time.time())-1}),('wrong-issuer',{'iss':'https://untrusted.invalid/auth/v1'}),('wrong-audience',{'aud':'wrong'})]:
        content=header+'.'+encode(json.dumps(dict(claims,**invalid)).encode())
        TOKENS[label]=content+'.'+encode(key.sign(content.encode(),padding.PKCS1v15(),hashes.SHA256()))
        call(label,'GET',base+'/patients',who=label,expected=401)
    call('browser-cors','OPTIONS',base+'/patients',who=None,expected=204,
         headers={'Origin':'http://localhost:4202','Access-Control-Request-Method':'POST','Access-Control-Request-Headers':'authorization,content-type'},
         check=lambda d,h:h.get('Access-Control-Allow-Origin')=='http://localhost:4202')
    call('foreign-origin','OPTIONS',base+'/patients',who=None,expected=403,headers={'Origin':'http://foreign.invalid'})
    call('nonadmin-denied','GET',base+'/admin/specialties',expected=403)
    call('administrator','GET',base+'/admin/specialties',who='admin')
    specialties=call('specialty-catalog','GET',base+'/specialties')
    specialty=next(s for s in specialties if s['code']=='pediatrics')
    for label in ['a','b']:
        setup=call('specialty-selection-'+label,'PUT',base+'/workspace/specialty',{'specialtyId':specialty['id'],'expectedVersion':None},who=label)
        setup=call('wizard-draft-'+label,'PUT',base+'/workspace/wizard/draft',{'expectedVersion':setup['version'],'definition':setup['draftDefinition'],'currentStepKey':setup['currentStepKey']},who=label)
        setup=call('wizard-finish-'+label,'POST',base+'/workspace/finish-onboarding',{'expectedVersion':setup['version']},who=label,
                   check=lambda d,h:d['onboardingCompleted'] and d['definition'] is not None)
        if label=='a': workspace=setup
    call('doctor-profile','GET',base+'/doctor/profile',check=lambda d,h:d['specialty']['id']==specialty['id'])
    preferences=call('workspace-preferences','GET',base+'/workspace/preferences')
    call('workspace-preferences-write','PUT',base+'/workspace/preferences',{'expectedVersion':preferences['version'],'patientColumnsConfig':{'phone':False}})
    call('workspace-notes','PUT',base+'/workspace/quick-notes',{'content':'Nota local sintética'})
    call('workspace-isolation','GET',base+'/workspace/preferences',who='b',check=lambda d,h:d['quickNotes'] is None)
    workspace=call('workspace-current-before-patient','GET',base+'/workspace')
    values={'sys_firstName':'Ana María','sys_lastName':'Prueba','sys_dni':'fixture-001','sys_dob':'2020-02-29','sys_sex':'FEMALE'}
    patient=call('patient-create','POST',base+'/patients',{'workspaceVersion':workspace['version'],'values':values},expected=201)
    pid=patient['id']; patient_path=base+'/patients/'+str(pid)
    call('patient-get','GET',patient_path,check=lambda d,h:d['fullName']=='Ana María Prueba')
    call('patient-owner','GET',patient_path,who='b',expected=403)
    update={'expectedVersion':patient['version'],'values':dict(values,sys_firstName='Ana María')}
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        futures=[pool.submit(call,'optimistic-write-'+str(i),'PUT',patient_path,update,expected=[200,409]) for i in range(2)]
        [f.result() for f in futures]
    concurrent_statuses=[row['status'] for row in RESULTS if row['case'].startswith('optimistic-write-')]
    RESULTS.append({'case':'one-concurrent-winner','passed':sorted(concurrent_statuses)==[200,409]})
    patient=call('patient-after-concurrent','GET',patient_path)
    call('patient-stale','PUT',patient_path,update,expected=409)
    call('patient-validation','POST',base+'/patients',{'workspaceVersion':workspace['version'],'values':{}},expected=400)
    call('patient-directory-filter','GET',base+'/patients?page=0&size=1&sort=fullName,asc&search=Ana&filter=MINOR',check=lambda d,h:d['page']['totalElements']==1 and len(d['content'])==1)
    encounter_values={'chiefComplaint':'Control de fixture','weightKg':18.5,'heightCm':105,'headCircumferenceCm':49,'diagnosis':'Control','treatment':'Indicaciones'}
    encounter=call('encounter-create','POST',base+'/encounters',{'patientId':pid,'workspaceVersion':workspace['version'],'consultationType':'FIRST_VISIT','values':encounter_values},expected=201)
    eid=encounter['id']; enc=base+'/encounters/'+str(eid)
    call('encounter-owner','GET',enc,who='b',expected=403)
    call('encounter-patient','GET',base+'/encounters/patient/'+str(pid),check=lambda d,h:len(d)==1)
    call('encounter-directory','GET',base+'/encounters?sort=encounterDate,desc&size=1')
    call('dashboard','GET',base+'/encounters/dashboard',check=lambda d,h:d['totalPatients']==1)
    call('growth-data','GET',patient_path+'/measurements')
    vaccination=call('vaccination-calendar','GET',base+'/vaccination-calendar')
    vaccines=call('patient-vaccination','GET',patient_path+'/vaccinations')
    dose=vaccines['doses'][0]['id']
    vaccine_request={'expectedVersion':patient['version'],'calendarId':vaccines['calendarId'],'applied':True,'appliedAt':'2021-03-01'}
    call('vaccination-apply','PUT',patient_path+'/vaccinations/'+urllib.parse.quote(dose),vaccine_request,check=lambda d,h:d['patient']['version']>patient['version'])
    call('vaccination-stale','PUT',patient_path+'/vaccinations/'+urllib.parse.quote(dose),vaccine_request,expected=409)
    call('vaccination-owner','GET',patient_path+'/vaccinations',who='b',expected=403)
    call('chart-catalog','GET',base+'/charts')
    call('chart-preferences','GET',base+'/workspace/charts')
    inputs={'imc':{'weightKg':70,'heightCm':170},'cockcroft':{'weightKg':70,'ageYears':50,'creatinineMgDl':1.2,'gender':'MALE'},
            'tfge':{'ageYears':50,'creatinineMgDl':1.2,'gender':'FEMALE'},'calcio_corregido':{'serumCalciumMgDl':8,'serumAlbuminGDl':3},
            'bsa_mosteller':{'weightKg':70,'heightCm':170},'holliday_segar':{'weightKg':20},
            'pediatric_dosage':{'weightKg':12,'targetDoseMgKg':50,'doseUnit':'mg/kg/dia','drugConcentrationMg':250,'drugConcentrationMl':5,'dosesPerDay':3},
            'chads_vasc':{'ageYears':75,'isFemale':True,'congestiveHeartFailure':True,'hypertension':True,'diabetes':True,'strokeOrTia':True,'vascularDisease':True}}
    call('calculator-catalog','GET',base+'/calculators/catalog',check=lambda d,h:len(d)==8)
    reference=json.loads((ROOT/'migration/evidence/compare.json').read_text(encoding='utf-8'))
    for code,input_snapshot in inputs.items():
        original=next(row['go']['body'] for row in reference['cases'] if row['id']=='calculator-preview-'+code)
        def clinical_result(value):
            value=dict(value); value.pop('calculatedAt',None)
            value['plainTextEvolution']=re.sub(r'(?m)^Fecha: .*$', 'Fecha: CLOCK', value['plainTextEvolution'])
            return value
        call('calculator-'+code,'POST',base+'/calculators/preview',{'calculationCode':code,'inputSnapshot':input_snapshot},
             check=lambda d,h,original=original:clinical_result(d)==clinical_result(original))
    call('calculator-preferences-write','PUT',base+'/calculators/preferences',{'favoriteCalculationCodes':['imc','tfge']})
    call('calculator-execute','POST',base+'/calculators/execute',{'patientId':pid,'encounterId':eid,'calculationCode':'imc','inputSnapshot':inputs['imc']},expected=[200,201])
    call('calculator-history','GET',base+'/calculators/patients/'+str(pid)+'/history',check=lambda d,h:len(d)==1)
    call('calculator-autofill','GET',base+'/calculators/patients/'+str(pid)+'/autofill')
    call('calculator-history-owner','GET',base+'/calculators/patients/'+str(pid)+'/history',who='b',expected=403)
    medication={'medicationName':'Paracetamol de prueba','dosage':'5 mg','administrationRoute':'Oral','frequency':'Cada 8 horas','durationDays':5,'instructions':'Con agua'}
    prescriptions=patient_path+'/prescriptions'
    rx=call('prescription-create','POST',prescriptions,{'type':'PRESCRIPTION','diagnosisDescription':'Diagnóstico sintético','generalInstructions':'Indicaciones de fixture',
          'prescriptions':[medication],'diagnosticOrders':['Hemograma de prueba','Ecografía de prueba']},expected=201)
    rxpath=prescriptions+'/'+str(rx['id'])
    call('prescription-list','GET',prescriptions,check=lambda d,h:len(d)==1)
    call('print-projection','GET',rxpath+'/print')
    query='?doctorName=Dra.%20Prueba%20Cl%C3%ADnica&license=MN%2012345&clinicName=Consultorio%20Fixture'
    pdf=call('pdf-owner','GET',rxpath+'/pdf'+query,check=lambda d,h:d.startswith(b'%PDF'))
    pdf_check('clinical-prescription',pdf,['Ana María Prueba','Dra. Prueba Clínica','MN 12345','Paracetamol de prueba','5 mg','Cada 8 horas'])
    call('pdf-other-owner-after-hot-access','GET',rxpath+'/pdf'+query,who='b',expected=403)
    encounter=call('encounter-update','PUT',enc,{'expectedVersion':encounter['version'],'encounterDate':encounter['encounterDate'],
                   'consultationType':'FIRST_VISIT','values':dict(encounter_values,diagnosis='Diagnóstico actualizado de fixture')})
    epdf=patient_path+'/encounters/'+str(eid)+'/pdf'+query
    call('encounter-pdf-other-owner-after-change','GET',epdf,who='b',expected=403)
    call('empty-encounter-print-rejected','GET',patient_path+'/encounters/'+str(eid)+'/print',expected=400)
    call('medication-catalog','GET',base+'/medications')
    call('medication-search','GET',base+'/medications/search?query=para&limit=2')
    orders=['Orden de prueba '+str(i).zfill(3) for i in range(100)]
    long_rx=call('long-orders-create','POST',prescriptions,{'type':'STUDY_ORDERS','diagnosisDescription':'Control sintético','generalInstructions':'Presentar órdenes completas','prescriptions':[],'diagnosticOrders':orders},expected=201)
    pdf=call('long-orders-pdf','GET',prescriptions+'/'+str(long_rx['id'])+'/pdf'+query+'&type=STUDY_ORDERS')
    text,reader=pdf_check('clinical-orders-100',pdf,orders+['Ana María Prueba','Dra. Prueba Clínica'])
    RESULTS.append({'case':'order-sequence-and-pagination','passed':len(reader.pages)>1 and [text.index(x) for x in orders]==sorted(text.index(x) for x in orders)})
    studies=patient_path+'/studies'
    first_study_id=None
    for label,format,size,orientation in [('jpeg','JPEG',(3000,1000),None),('exif','JPEG',(3000,1000),6),('png','PNG',(24,18),None),
                                        ('webp','WEBP',(24,18),None),('gif','GIF',(24,18),None),('bmp','BMP',(24,18),None),('tiff','TIFF',(24,18),None)]:
        raw=picture(format,size,orientation); ext={'JPEG':'jpg','TIFF':'tif'}.get(format,format.lower())
        study=call('study-upload-'+label,'POST',studies,multipart('original-'+label+'.'+ext,raw,'Contenido sintético'),expected=201,
                   check=lambda d,h:d['fileSize']==len(raw))
        if first_study_id is None: first_study_id=study['id']
        file=studies+'/'+str(study['id'])+'/file'
        call('study-original-'+label,'GET',file,check=lambda d,h:d==raw)
        call('study-owner-'+label,'GET',file,who='b',expected=403)
        call('study-range-'+label,'GET',file,expected=206,headers={'Range':'bytes=0-15'},check=lambda d,h:d==raw[:16])
        if label=='exif': (LOCAL/'original-exif.jpg').write_bytes(raw)
    call('study-metadata','GET',studies+'/'+str(first_study_id))
    for name,raw in [('broken.jpg',b'not an image'),('mismatch.jpg',picture('PNG')),('../bad.png',picture('PNG')),('empty.jpg',b'')]:
        call('study-invalid-'+name,'POST',studies,multipart(name,raw),expected=400 if 'bad' in name or not raw else 415)
    writer=PdfWriter(); writer.add_blank_page(595,842); writer.encrypt('owned-test-password'); buf=io.BytesIO(); writer.write(buf)
    call('encrypted-pdf','POST',studies,multipart('encrypted.pdf',buf.getvalue()),expected=415)
    raw=picture('JPEG',(8000,6000))
    call('48mp-original-rejected-without-loss','POST',studies,multipart('original-48mp.jpg',raw),expected=400,check=lambda d,h:d['code']=='STUDY_IMAGE_TOO_LARGE')
    call('oversized-file','POST',studies,multipart('original.jpg',b'x'*(25*1024*1024+1)),expected=413)
    control(fail_puts=True)
    call('storage-unavailable-upload','POST',studies,multipart('unavailable.png',picture('PNG')),expected=503)
    control(fail_puts=False,fail_reads=True)
    call('storage-unavailable-download','GET',studies+'/'+str(first_study_id)+'/file',expected=503)
    control(fail_reads=False,fail_deletes=True)
    key=sql(CFG,DB,'SELECT stored_file_name FROM patient_medical_studies WHERE id='+str(first_study_id)+';').strip()
    call('durable-delete-during-outage','DELETE',studies+'/'+str(first_study_id),expected=204)
    count=sql(CFG,DB,"SELECT count(*) FROM study_file_deletions WHERE storage_key='"+key+"';").strip()
    RESULTS.append({'case':'delete-intent-persisted','passed':count=='1'})
    control(fail_deletes=False); restart()
    for _ in range(70):
        if sql(CFG,DB,"SELECT count(*) FROM study_file_deletions WHERE storage_key='"+key+"';").strip()=='0': break
        time.sleep(.5)
    RESULTS.append({'case':'worker-recovered-after-restart','passed':sql(CFG,DB,"SELECT count(*) FROM study_file_deletions WHERE storage_key='"+key+"';").strip()=='0'})
    call('persistent-patient-after-restart','GET',patient_path,check=lambda d,h:d['fullName']=='Ana María Prueba')
    call('persistent-calculation-after-restart','GET',base+'/calculators/patients/'+str(pid)+'/history',check=lambda d,h:len(d)==1)
    call('patient-delete-other-owner','DELETE',patient_path,who='b',expected=403)
    call('patient-delete-cascade','DELETE',patient_path,expected=204)
    call('patient-deleted','GET',patient_path,expected=404)
    counts=sql(CFG,DB,f'SELECT (SELECT count(*) FROM clinical_encounters WHERE patient_id={pid})+(SELECT count(*) FROM patient_prescription_records WHERE patient_id={pid})+(SELECT count(*) FROM patient_medical_studies WHERE patient_id={pid})+(SELECT count(*) FROM patient_calculation_logs WHERE patient_id={pid});').strip()
    RESULTS.append({'case':'patient-dependent-data-deleted','passed':counts=='0'})
    (LOCAL/'pause-backend').touch(); time.sleep(1)
    call('downstream-unavailable','GET',base+'/patients',expected=502)
    restart(); call('downstream-restored','GET',base+'/patients')
    # BASIC limit exercise is deliberately at the trusted Go boundary; no browser secret.
    for i in range(3): call('billing-basic-limit-'+str(i),'POST',base+'/patients',{'workspaceVersion':workspace['version'],'values':dict(values,sys_dni='basic-fixture-'+str(i))},expected=201 if i<2 else 403,direct=True)
    def normalize(dump): return re.sub(rb'^\\(?:un)?restrict .*\r?\n',b'',dump,flags=re.M).strip()
    after=command('docker','exec',CFG['container'],'pg_dump','-U','migration','-d',DB,'--schema-only','--no-owner','--no-privileges')
    RESULTS.append({'case':'schema-unchanged','passed':normalize(after)==normalize((LOCAL/'schema-before.sql').read_bytes())})

if __name__=='__main__':
    try: main()
    except Exception as error:
        RESULTS.append({'case':'harness-execution','passed':False,'errorType':type(error).__name__,'message':str(error)})
        raise
    finally:
        EVIDENCE.mkdir(parents=True,exist_ok=True)
        summary={'cases':len(RESULTS),'passed':sum(row['passed'] for row in RESULTS),'database':DB,
                 'scope':'actual Gateway snapshot and Go HTTP processes; synthetic auth/Billing/S3; BASIC quota cases explicitly use trusted Go boundary'}
        (EVIDENCE/'local-http.json').write_text(json.dumps({'summary':summary,'cases':RESULTS},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        print(json.dumps(summary),flush=True)
    if not all(row['passed'] for row in RESULTS): sys.exit(1)
