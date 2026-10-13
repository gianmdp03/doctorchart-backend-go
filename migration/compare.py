"""Synthetic HTTP, PDF and SQL helpers used by current Go integration; no legacy runner."""
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

from fixture import ROOT, WORK, command, sql

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
