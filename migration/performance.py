"""Equivalent, nonproduction read/preview workload; sequential job ownership."""
import concurrent.futures
import ctypes
from ctypes import wintypes
import http.server
import json
import os
import secrets
import statistics
import threading
import time
import urllib.request
from fixture import ROOT,WORK,command,sql,start_spring,start_go
from compare import Billing,capture

def process_metrics(pid):
    class Counters(ctypes.Structure):
        _fields_=[('cb',wintypes.DWORD),('faults',wintypes.DWORD)]+[(name,ctypes.c_size_t) for name in ['peak','rss','peak_paged','paged','peak_nonpaged','nonpaged','pagefile','peak_pagefile']]
    kernel=ctypes.WinDLL('kernel32',use_last_error=True)
    kernel.OpenProcess.restype=wintypes.HANDLE
    handle=kernel.OpenProcess(0x0400|0x0010,False,pid)
    if not handle:raise ctypes.WinError(ctypes.get_last_error())
    try:
        counters=Counters();counters.cb=ctypes.sizeof(counters)
        if not ctypes.WinDLL('psapi').GetProcessMemoryInfo(handle,ctypes.byref(counters),counters.cb):raise ctypes.WinError()
        times=[wintypes.FILETIME() for _ in range(4)]
        if not kernel.GetProcessTimes(handle,*[ctypes.byref(item) for item in times]):raise ctypes.WinError()
        total=sum((value.dwHighDateTime<<32)|value.dwLowDateTime for value in times[2:])/10_000_000
        return dict(cpu_seconds=total,rss_bytes=counters.rss,peak_rss_bytes=counters.peak)
    finally:kernel.CloseHandle(handle)

def main():
    cfg=json.loads((WORK/'fixture.json').read_text())
    comparison=json.loads((ROOT/'migration/evidence/compare.json').read_text(encoding='utf-8'))
    origin=comparison['summary']['databases'][1]
    dump=command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d',origin,'--no-owner','--no-privileges').decode('utf-8')
    billing=http.server.ThreadingHTTPServer(('127.0.0.1',18999),Billing)
    threading.Thread(target=billing.serve_forever,daemon=True).start()
    paths=[('GET','/api/doctorchart/patients',None),('GET','/api/doctorchart/patients/1',None),('GET','/api/doctorchart/workspace',None),('GET','/api/doctorchart/workspace/preferences',None),('GET','/api/doctorchart/encounters?patientId=1',None),('GET','/api/doctorchart/calculators',None),('GET','/api/doctorchart/vaccination-calendar',None),('POST','/api/doctorchart/calculators/preview',{'calculationCode':'imc','inputSnapshot':{'weightKg':70,'heightCm':170}})]
    # Paths/parameters are checked against the same reference captures first.
    for index,(method,path,body) in enumerate(paths):
        if '/calculators' in path:
            sample=next(row for row in comparison['cases'] if row['method']==method and ('/preview' in row['path'] if method=='POST' else row['id']=='calculator-catalog'))
            paths[index]=(method,sample['path'],body if method=='POST' else None)
    results=[];process=None
    previous=os.environ.get('SPRING_JPA_HIBERNATE_DDL_AUTO');os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']='validate'
    try:
        for runtime in ['spring','go']:
            db='perf_'+secrets.token_hex(3)+'_'+runtime
            sql(cfg,'postgres','CREATE DATABASE '+db+';');sql(cfg,db,dump)
            process=start_spring(cfg,db) if runtime=='spring' else start_go(cfg,db)
            port=18083 if runtime=='spring' else 18084
            if runtime=='go':
                for _ in range(150):
                    try:capture(port,cfg,'GET','/api/doctorchart/health');break
                    except OSError:time.sleep(.1)
            def request(index):
                method,path,body=paths[index%len(paths)];began=time.perf_counter()
                response=capture(port,cfg,method,path,body)
                return (time.perf_counter()-began)*1000,response['status']
            warmup=[request(i) for i in range(64)]
            if any(status!=200 for _,status in warmup):raise RuntimeError('Non-equivalent workload route/fixture')
            before=process_metrics(process.pid)
            db_before=json.loads(sql(cfg,db,"SELECT row_to_json(t) FROM (SELECT xact_commit,xact_rollback,tup_returned,tup_fetched FROM pg_stat_database WHERE datname=current_database()) t;"))
            started=time.perf_counter()
            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:samples=list(pool.map(request,range(1024)))
            duration=time.perf_counter()-started;after=process_metrics(process.pid)
            db_after=json.loads(sql(cfg,db,"SELECT row_to_json(t) FROM (SELECT xact_commit,xact_rollback,tup_returned,tup_fetched FROM pg_stat_database WHERE datname=current_database()) t;"))
            latencies=sorted(value for value,_ in samples)
            result=dict(runtime=runtime,database=db,requests=len(samples),concurrency=8,errors=sum(status!=200 for _,status in samples),seconds=duration,requests_per_second=len(samples)/duration,p50_ms=statistics.median(latencies),p95_ms=latencies[int(len(samples)*.95)-1],p99_ms=latencies[int(len(samples)*.99)-1],cpu_seconds=after['cpu_seconds']-before['cpu_seconds'],rss_bytes=after['rss_bytes'],peak_process_rss_bytes=after['peak_rss_bytes'],database_delta={key:db_after[key]-db_before[key] for key in db_before})
            results.append(result);print(json.dumps(result),flush=True)
            process.terminate();process.wait(timeout=20);process=None
        (ROOT/'migration/evidence/performance.json').write_text(json.dumps(dict(workload=paths,results=results,scope='Windows native runtimes, identical disposable data, warm read/preview workload; no production capacity claim'),ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    finally:
        if process and process.poll() is None:process.terminate();process.wait(timeout=20)
        if previous is None:os.environ.pop('SPRING_JPA_HIBERNATE_DDL_AUTO',None)
        else:os.environ['SPRING_JPA_HIBERNATE_DDL_AUTO']=previous
        billing.shutdown();billing.server_close()

if __name__=='__main__':main()
