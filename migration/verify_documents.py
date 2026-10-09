"""Medical file/document boundary proof with a local S3 protocol fixture."""
import base64
import concurrent.futures
import copy
import datetime
import hashlib
import http.server
import io
import json
import os
import secrets
import tempfile
import threading
import time
import uuid
import urllib.parse
from pathlib import Path
from PIL import Image,ImageDraw,ImageChops,ImageStat
from pypdf import PdfReader,PdfWriter
from fixture import ROOT,WORK,command,sql,start_spring,start_go
from compare import Billing,capture,multipart,pdf_fixture,pdf_content

class S3(http.server.BaseHTTPRequestHandler):
    objects={};events=[];modified={}
    protocol_version='HTTP/1.1'
    def handle(self):
        try:super().handle()
        except (ConnectionResetError,BrokenPipeError):pass
    def log_message(self,*args):pass
    def key(self):return urllib.parse.unquote(urllib.parse.urlsplit(self.path).path).lstrip('/')
    def respond(self,status,body=b'',content_type='application/xml'):
        object_data=self.objects.get(self.key(),(body,''))[0]
        self.send_response(status);self.send_header('Content-Length',str(len(body)));self.send_header('Content-Type',content_type);self.send_header('ETag','"'+hashlib.md5(object_data).hexdigest()+'"');self.end_headers()
        if self.command!='HEAD':self.wfile.write(body)
    def event(self):
        if not self.headers.get('Authorization','').startswith('AWS4-HMAC-SHA256 '):raise RuntimeError('unsigned S3 call')
        self.events.append(dict(method=self.command,bucket=self.key().split('/')[0]))
    def do_PUT(self):
        self.event()
        if 'chunked' in self.headers.get('Transfer-Encoding',''):
            data=b''
            while True:
                count=int(self.rfile.readline().split(b';')[0],16)
                if count==0:
                    while self.rfile.readline().strip():pass
                    break
                data+=self.rfile.read(count);self.rfile.read(2)
        else:data=self.rfile.read(int(self.headers.get('Content-Length','0')))
        if 'aws-chunked' in self.headers.get('Content-Encoding',''):
            stream=io.BytesIO(data);decoded=b''
            while True:
                count=int(stream.readline().split(b';')[0],16)
                if count==0:break
                decoded+=stream.read(count);stream.read(2)
            data=decoded
        self.objects[self.key()]=(data,self.headers.get('Content-Type','application/octet-stream'));self.respond(200)
    def do_HEAD(self):
        self.event()
        if self.key() not in self.objects:self.respond(404);return
        data,kind=self.objects[self.key()];self.respond(200,data,kind)
    def do_GET(self):
        self.event()
        if 'list-type=2' in self.path:
            bucket=self.key();contents=''.join('<Contents><Key>'+key[len(bucket)+1:]+'</Key><LastModified>'+self.modified.get(key,datetime.datetime.now(datetime.timezone.utc).isoformat())+'</LastModified><Size>'+str(len(data[0]))+'</Size></Contents>' for key,data in list(self.objects.items()) if key.startswith(bucket+'/'))
            self.respond(200,('<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>'+bucket+'</Name><IsTruncated>false</IsTruncated>'+contents+'</ListBucketResult>').encode());return
        if self.key() not in self.objects:self.respond(404,b'<Error><Code>NoSuchKey</Code></Error>');return
        data,kind=self.objects[self.key()];self.respond(200,data,kind)
    def do_DELETE(self):self.event();self.objects.pop(self.key(),None);self.respond(204)

def picture(format,size=(24,18),orientation=None):
    image=Image.new('RGB',size,'white');draw=ImageDraw.Draw(image)
    draw.rectangle((0,0,size[0]//2,size[1]//2),fill='red');draw.rectangle((size[0]//2,size[1]//2,size[0],size[1]),fill='blue')
    output=io.BytesIO();options={}
    if format=='JPEG':options={'quality':95}
    if orientation:
        exif=Image.Exif();exif[274]=orientation;options['exif']=exif
    image.save(output,format=format,**options);return output.getvalue()

def main():
    cfg=json.loads((WORK/'fixture.json').read_text())
    comparison=json.loads((ROOT/'migration/evidence/compare.json').read_text(encoding='utf-8'))
    origin=comparison['summary']['databases'][1]
    dump=command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d',origin,'--no-owner','--no-privileges').decode('utf-8')
    servers=[http.server.ThreadingHTTPServer(('127.0.0.1',19099),Billing),http.server.ThreadingHTTPServer(('127.0.0.1',19200),S3)]
    for server in servers:threading.Thread(target=server.serve_forever,daemon=True).start()
    results=[];deviations=[];processes=[]
    prefix='documents_'+secrets.token_hex(3);dbs=[prefix+'_spring',prefix+'_go']
    previous=os.environ.get('SPRING_JPA_HIBERNATE_DDL_AUTO');os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']='validate'
    orphan_fixtures=[]
    for bucket in ['spring-fixture','go-fixture']:
        old=bucket+'/'+str(uuid.uuid4());fresh=bucket+'/'+str(uuid.uuid4())
        for key in [old,fresh]:S3.objects[key]=(b'owned orphan fixture','application/octet-stream')
        S3.modified[old]=(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(hours=25)).isoformat()
        orphan_fixtures.append((old,fresh))
    try:
        for index,db in enumerate(dbs):
            sql(cfg,'postgres','CREATE DATABASE '+db+';');sql(cfg,db,dump)
            env={'SERVICES_BILLING_URL':'http://127.0.0.1:19099','STORAGE_TYPE':'s3','S3_ENDPOINT':'http://127.0.0.1:19200','S3_BUCKET':['spring-fixture','go-fixture'][index],'S3_ACCESS_KEY':'owned-test-access','S3_SECRET_KEY':'owned-test-secret','S3_REGION':'auto'}
            if index==0:processes.append(start_spring(cfg,db,18183,overrides=env))
            else:
                processes.append(start_go(cfg,db,18184,overrides=env))
                for _ in range(150):
                    try:capture(18184,cfg,'GET','/api/doctorchart/health');break
                    except OSError:time.sleep(.1)
        studies='/api/doctorchart/patients/1/studies'
        for format,ext in [('JPEG','jpg'),('PNG','png'),('WEBP','webp'),('GIF','gif'),('BMP','bmp'),('TIFF','tiff')]:
            data=picture(format);responses=[];downloads=[];deletions=[]
            for port in [18183,18184]:
                uploaded=capture(port,cfg,'POST',studies,multipart('estudio.'+ext,data));responses.append(uploaded)
                if uploaded['status']==201:
                    path=studies+'/'+str(uploaded['body']['id']);downloads.append(capture(port,cfg,'GET',path+'/file'))
                    deletions.append(capture(port,cfg,'DELETE',path))
            canonical=copy.deepcopy(responses)
            for response in canonical:
                if response['status']==201:response['body']['uploadedAt']='<validated separately in main comparison>'
            passed=canonical[0]==canonical[1] and len(downloads)==2 and downloads[0]==downloads[1] and base64.b64decode(downloads[0]['body']['binaryBase64'])==data and deletions[0]==deletions[1] and deletions[0]['status']==204
            results.append(dict(case='s3-compact-'+format,passed=passed,responses=responses,byte_preserved=passed))
        encrypted=PdfWriter();encrypted.add_blank_page(595,842);encrypted.encrypt('owned-test-password');buffer=io.BytesIO();encrypted.write(buffer)
        responses=[capture(port,cfg,'POST',studies,multipart('cifrado.pdf',buffer.getvalue())) for port in [18183,18184]]
        results.append(dict(case='encrypted-pdf-rejected',passed=responses[0]==responses[1] and responses[0]['status']==415,responses=responses))
        for label,data in [('large-jpeg',picture('JPEG',(3000,1000))),('exif-portrait',picture('JPEG',(3000,1000),6)),('large-png',picture('PNG',(3000,1000))),('high-resolution-48mp',picture('JPEG',(8000,6000)))]:
            ext='png' if label.endswith('png') else 'jpg';responses=[];image_info=[]
            for port in [18183,18184]:
                uploaded=capture(port,cfg,'POST',studies,multipart(label+'.'+ext,data));responses.append(uploaded)
                if uploaded['status']==201:
                    downloaded=capture(port,cfg,'GET',studies+'/'+str(uploaded['body']['id'])+'/file')
                    content=base64.b64decode(downloaded['body']['binaryBase64']);image=Image.open(io.BytesIO(content));image.load()
                    image_info.append(dict(size=image.size,format=image.format,bytes=len(content),sha256=hashlib.sha256(content).hexdigest()))
                    (WORK/(label+'-'+['spring','go'][port-18183]+'.'+ext)).write_bytes(content)
            passed=len(image_info)==2 and image_info[0]['size']==image_info[1]['size'] and image_info[0]['format']==image_info[1]['format']
            same_bytes=passed and image_info[0]['sha256']==image_info[1]['sha256']
            results.append(dict(case='optimized-'+label,passed=passed,exact_bytes=same_bytes,image_info=image_info,responses=responses))
            if not same_bytes:
                metric=None
                if passed:
                    a=Image.open(WORK/(label+'-spring.'+ext)).convert('RGB');b=Image.open(WORK/(label+'-go.'+ext)).convert('RGB')
                    stats=ImageStat.Stat(ImageChops.difference(a,b));metric={'mean_absolute_channel_error':stats.mean,'channel_rms_error':stats.rms,'scope':'Synthetic colored blocks only; no diagnostic quality claim'}
                deviations.append(dict(case=label,type='IMAGE_ENCODER',reason='Native Go encoder/resizer produces different compressed bytes/fileSize from Java ImageIO/Thumbnailator.',image_info=image_info,pixel_comparison=metric))
        path='/api/doctorchart/patients/1/prescriptions/1/pdf?doctorName=CacheFixture'
        owners=[capture(port,cfg,'GET',path) for port in [18183,18184]]
        others=[capture(port,cfg,'GET',path,user='other-doctor') for port in [18183,18184]]
        results.append(dict(case='pdf-other-owner-after-warm-cache',passed=owners[0]['status']==200 and owners[1]['status']==200 and others[1]['status']==403,spring_status=others[0]['status'],go_status=others[1]['status']))
        if others[0]['status']!=others[1]['status']:deviations.append(dict(case='pdf-cache-ownership',type='SECURITY',spring_status=others[0]['status'],go_status=others[1]['status'],reason='Spring cache key omits identity and bypasses service ownership checks. Go always enforces medical-record ownership.'))
        # Large clinical documents: validate every order survives across all pages.
        for db in dbs:
            sql(cfg,db,"UPDATE clinical_encounters SET prescriptions='[]',diagnostic_orders=(SELECT jsonb_agg('Estudio clínico '||n ORDER BY n) FROM generate_series(1,100) n),primary_diagnosis_description='Diagnóstico para documento extenso' WHERE id=1;")
        documents=[capture(port,cfg,'GET','/api/doctorchart/patients/1/encounters/1/pdf?doctorName=PaginationFixture') for port in [18183,18184]]
        texts=[];page_counts=[]
        for index,response in enumerate(documents):
            raw=base64.b64decode(response['body']['binaryBase64']);reader=PdfReader(io.BytesIO(raw));text=' '.join(page.extract_text() or '' for page in reader.pages)
            texts.append(text);page_counts.append(len(reader.pages));(WORK/('long-document-'+['spring','go'][index]+'.pdf')).write_bytes(raw)
        passed=all(all('Estudio clínico '+str(number) in text for number in range(1,101)) for text in texts)
        results.append(dict(case='long-document-all-orders',passed=passed,page_counts=page_counts))
        if page_counts[0]!=page_counts[1]:deviations.append(dict(case='pdf-pagination',type='PDF_RENDERER',page_counts=page_counts,reason='Independent PDF layout engines paginate differently; medical text is preserved.'))
        deadline=time.monotonic()+32
        while any(old in S3.objects for old,_ in orphan_fixtures) and time.monotonic()<deadline:time.sleep(.1)
        results.append(dict(case='s3-scheduled-orphan-grace',passed=all(old not in S3.objects and fresh in S3.objects for old,fresh in orphan_fixtures)))
        evidence=dict(cases=results,deviations=deviations,s3_methods=sorted(set(event['method'] for event in S3.events)),databases=dbs,scope='Owned local signed S3 protocol fixture; no AWS/R2 service contacted')
        (ROOT/'migration/evidence/documents.json').write_text(json.dumps(evidence,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        print(json.dumps({'cases':len(results),'passed':sum(row['passed'] for row in results),'deviations':deviations,'s3_methods':evidence['s3_methods']},ensure_ascii=False),flush=True)
        if not all(row['passed'] for row in results):raise SystemExit(1)
    finally:
        for process in processes:
            if process.poll() is None:process.terminate();process.wait(timeout=20)
        if previous is None:os.environ.pop('SPRING_JPA_HIBERNATE_DDL_AUTO',None)
        else:os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']=previous
        for server in servers:server.shutdown();server.server_close()

if __name__=='__main__':main()
