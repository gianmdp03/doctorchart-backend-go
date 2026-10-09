"""Directed regressions after the Gateway changed during its external refactor."""
import base64, hashlib, json, re
from cryptography.hazmat.primitives import serialization, hashes
from cryptography.hazmat.primitives.asymmetric import padding
import verify_local as v

for label in ['a','b','browser','admin','pointly','expired','inactive','invalid','missing-expiry','string-active']:v.login(label)
base='/api/doctorchart'
progress=json.loads((v.LOCAL/'browser-progress.json').read_text())
patient=base+'/patients/'+str(progress['patientId'])
v.call('current-gateway-health','GET',base+'/health',who=None)
v.call('current-gateway-absent-session','GET',base+'/patients',who=None,expected=401)
for label in ['pointly','expired','inactive','invalid','missing-expiry','string-active']:
    v.call('current-gateway-entitlement-'+label,'GET',base+'/patients',who=label,expected=403)
v.call('current-gateway-valid-patient','GET',patient,who='browser',check=lambda d,h:d['birthDate']=='2020-02-29')
v.call('current-gateway-patient-isolation','GET',patient,who='b',expected=403)
v.call('current-gateway-header-forgery','GET',patient,who='b',expected=403,headers={'X-User-Id':v.USERS['browser'],'X-User-Role':'SUPER_ADMIN','X-User-Plan':'PRO','X-User-Plan-Expires-At':'2099-01-01T00:00:00Z','X-Gateway-Secret':'forged-browser-value'})
v.call('current-gateway-admin-forbidden','GET',base+'/admin/specialties',who='browser',expected=403)
v.call('current-gateway-admin','GET',base+'/admin/specialties',who='admin')
v.call('current-gateway-history','GET',base+'/encounters/patient/'+str(progress['patientId']),who='browser',check=lambda d,h:len(d)==1 and d[0]['measurements']['weightKg']==18.5)
pdf=patient+'/prescriptions/'+str(progress['prescriptionId'])+'/pdf'
raw=v.call('current-gateway-owner-pdf','GET',pdf,who='browser',check=lambda d,h:d.startswith(b'%PDF') and 'no-store' in h.get('Cache-Control','') and 'private' in h.get('Cache-Control',''))
v.pdf_check('current-gateway-prescription',raw,['Paciente Navegador Prueba','Paracetamol de prueba','5 mg'])
v.call('current-gateway-hot-pdf-isolation','GET',pdf,who='b',expected=403)
v.call('current-gateway-allowed-cors','OPTIONS',base+'/patients',who=None,expected=204,headers={'Origin':'http://localhost:4202','Access-Control-Request-Method':'GET','Access-Control-Request-Headers':'authorization,content-type'})
v.call('current-gateway-foreign-cors','OPTIONS',base+'/patients',who=None,expected=403,headers={'Origin':'https://untrusted.fixture.test','Access-Control-Request-Method':'GET'})
# Real RS256 signed fixture JWT: use the owned issuer's key, not production credentials.
header,body,_=v.TOKENS['a'].split('.')
claims=json.loads(base64.urlsafe_b64decode(body+'='*((4-len(body)%4)%4)))
key=serialization.load_pem_private_key((v.LOCAL/'key.pem').read_bytes(),None)
def plan_token(tier):
    claims['app_metadata']['app_modules']['doctorchart']['tier']=tier
    encoded=base64.urlsafe_b64encode(json.dumps(claims).encode()).rstrip(b'=').decode()
    signed=header+'.'+encoded
    v.TOKENS['plan']=signed+'.'+base64.urlsafe_b64encode(key.sign(signed.encode(),padding.PKCS1v15(),hashes.SHA256())).rstrip(b'=').decode()
workspace=v.call('quota-workspace-current','GET',base+'/workspace')
values={'sys_firstName':'Quota','sys_lastName':'Gateway','sys_dni':'owned-current-quota','sys_dob':'2020-02-29','sys_sex':'FEMALE'}
plan_token('BASIC')
v.call('actual-gateway-basic-quota','POST',base+'/patients',{'workspaceVersion':workspace['version'],'values':values},who='plan',expected=403)
plan_token('PRO')
created=v.call('actual-gateway-upgraded-plan-quota','POST',base+'/patients',{'workspaceVersion':workspace['version'],'values':values},who='plan',expected=201)
v.call('actual-gateway-quota-cleanup','DELETE',base+'/patients/'+str(created['id']),who='plan',expected=204)
reference=json.loads((v.ROOT/'migration/evidence/compare.json').read_text(encoding='utf8'))
def normalize(value):
    value=dict(value);value.pop('calculatedAt',None);value['plainTextEvolution']=re.sub(r'(?m)^Fecha: .*$', 'Fecha: CLOCK',value['plainTextEvolution']);return value
expected=next(x['go']['body'] for x in reference['cases'] if x['id']=='calculator-preview-imc')
v.call('current-gateway-clinical-calculation','POST',base+'/calculators/preview',{'calculationCode':'imc','inputSnapshot':{'weightKg':70,'heightCm':170}},who='browser',check=lambda d,h:normalize(d)==normalize(expected))
result={'scope':'Actual current Gateway snapshot; existing synthetic PostgreSQL; Go clinical/auth/storage boundary','passed':sum(x['passed'] for x in v.RESULTS),'total':len(v.RESULTS),'cases':v.RESULTS}
(v.EVIDENCE/'current-gateway.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf8')
print(result['passed'],'/',result['total'],'PASS')
raise SystemExit(0 if result['passed']==result['total'] else 1)
