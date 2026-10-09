"""Current PDF clinical fidelity, Unicode safety, pagination and rendered QA."""
import io, json
from pathlib import Path
import verify_local as v
from pypdf import PdfReader
import pypdfium2 as pdfium
from PIL import Image, ImageDraw

v.login('browser')
v.login('b')
progress = json.loads((v.LOCAL/'browser-progress.json').read_text())
base = '/api/doctorchart/patients/'+str(progress['patientId'])+'/prescriptions'
query = '?doctorName=Dra.%20Prueba%20Cl%C3%ADnica&license=MN%2012345&clinicName=Consultorio%20Fixture'

def document(name, body, required):
    record = v.call(name+'-create', 'POST', base, body, who='browser', expected=201)
    raw = v.call(name+'-pdf', 'GET', base+'/'+str(record['id'])+'/pdf'+query, who='browser', check=lambda d,h:'no-store' in h.get('Cache-Control','') and 'private' in h.get('Cache-Control',''))
    v.call(name+'-other-owner-after-hot-access','GET',base+'/'+str(record['id'])+'/pdf'+query,who='b',expected=403)
    text, reader = v.pdf_check(name, raw, required)
    pdf = pdfium.PdfDocument(raw)
    panels = []
    for index, page in enumerate(pdf):
        image = page.render(scale=1.8).to_pil().convert('RGB')
        image.save(v.EVIDENCE/(name+'-page-'+str(index+1)+'.png'))
        panels.append(image)
    sheet = Image.new('RGB', (panels[0].width*min(2,len(panels)), panels[0].height*((len(panels)+1)//2)), 'white')
    for index, image in enumerate(panels): sheet.paste(image,(index%2*image.width,index//2*image.height))
    sheet.save(v.EVIDENCE/(name+'-contact.png'))
    v.call(name+'-cleanup','DELETE',base+'/'+str(record['id']),who='browser',expected=204)
    return text, reader

med = {'medicationName':'Препарат Ω μg; Paracetamol de prueba','dosage':'5 mg','administrationRoute':'Oral','frequency':'Cada 8 horas','durationDays':5,'instructions':'Con agua. Información clínica sintética.'}
document('clinical-unicode', {'type':'PRESCRIPTION','diagnosisDescription':'Синтетический Ø œ; Diagnóstico sintético','generalInstructions':'Conservar las indicaciones médicas','prescriptions':[med],'diagnosticOrders':[]},
    ['Препарат Ω μg','Синтетический Ø œ','Paciente Navegador Prueba','5 mg','Cada 8 horas','Dra. Prueba Clínica','MN 12345'])
orders = ['Orden de prueba '+str(i).zfill(3)+' — Estudio clínico completo' for i in range(100)]
text, reader = document('clinical-orders-current', {'type':'STUDY_ORDERS','diagnosisDescription':'Control clínico sintético','generalInstructions':'Presentar todas las órdenes','prescriptions':[],'diagnosticOrders':orders}, orders)
v.RESULTS.append({'case':'ordered-complete-multipage-pdf','passed':len(reader.pages)>1 and [text.index(x) for x in orders]==sorted(text.index(x) for x in orders)})
for value in ['药', 'سلام', '🩺']:
    rx=v.call('unsupported-text-create','POST',base,{'type':'STUDY_ORDERS','prescriptions':[],'diagnosticOrders':['Control '+value]},who='browser',expected=201)
    v.call('unsupported-text-safe-error','GET',base+'/'+str(rx['id'])+'/pdf',who='browser',expected=422,check=lambda d,h:d['code']=='PDF_UNSUPPORTED_CHARACTER')
    v.call('unsupported-text-cleanup','DELETE',base+'/'+str(rx['id']),who='browser',expected=204)
result={'scope':'Actual Gateway and current Go process; all identity/data synthetic; fonts embedded; renders require visual inspection','cases':v.RESULTS,'passed':sum(x['passed'] for x in v.RESULTS),'total':len(v.RESULTS)}
(v.EVIDENCE/'pdf-current.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf8')
print(result['passed'],'/',result['total'],'PASS')
raise SystemExit(0 if result['passed']==result['total'] else 1)
