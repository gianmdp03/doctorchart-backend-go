"""Simulated-provider helpers for current Go integration; historical Spring runner retired."""
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
from fixture import ROOT,WORK,command,sql
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
