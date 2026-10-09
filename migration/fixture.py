"""Target-owned disposable reference. Never reads existing DB connection settings."""
import argparse
import json
import os
import secrets
import subprocess
import tempfile
import time
import atexit
import threading
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / '.migration'
JAVA = Path(os.environ.get('MIGRATION_JAVA', 'C:/Users/gcast/.jdks/ms-25.0.4.1/bin/java.exe'))

def command(*args, input=None):
    return subprocess.run(args, input=input, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout

_sessions={}
_sessions_lock=threading.Lock()
def sql(config, db, statement):
    if not config['container'].startswith('doctorchart-go-fixture-'):
        raise RuntimeError('Refusing an unowned SQL fixture')
    key=(config['container'],db)
    with _sessions_lock:
        if key not in _sessions or _sessions[key][0].poll() is not None:
            process=subprocess.Popen(['docker','exec','-i',config['container'],'psql','-U','migration','-d',db,'-v','ON_ERROR_STOP=1','-Atq'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,encoding='utf-8')
            _sessions[key]=(process,threading.Lock())
        process,lock=_sessions[key]
    with lock:
        marker='fixture_end_'+secrets.token_hex(8)
        process.stdin.write('SET search_path=public;\n'+statement+'\nSET search_path=public;\nSELECT \''+marker+'\';\n');process.stdin.flush()
        result=[]
        while True:
            line=process.stdout.readline()
            if not line:raise RuntimeError('Owned fixture SQL failed; PostgreSQL session closed')
            if line.strip()==marker:return ''.join(result)
            result.append(line)

def close_sql():
    for process,_ in _sessions.values():
        if process.poll() is None:
            process.stdin.close()
            try:process.wait(timeout=5)
            except subprocess.TimeoutExpired:process.terminate();process.wait(timeout=5)
    _sessions.clear()
atexit.register(close_sql)

def start_spring(config, database='spring', port=18083, cwd=WORK, overrides=None):
    env = dict(os.environ)
    env.update(PORT=str(port),SPRING_DATASOURCE_URL=f'jdbc:postgresql://127.0.0.1:{config["db_port"]}/{database}',DB_USERNAME='migration',DB_PASSWORD=config['password'],GATEWAY_SHARED_SECRET=config['gateway'],SERVICES_BILLING_SHARED_SECRET=config['billing'],SERVICES_BILLING_URL='http://127.0.0.1:18999',STUDIES_STORAGE_DIR=str(Path(tempfile.gettempdir()) / config['container'] / ('studies-'+database)))
    if overrides:env.update(overrides)
    log = (WORK / ('spring-'+database+'.log')).open('wb')
    process = subprocess.Popen([str(JAVA),'-jar',str(WORK/'spring/target/doctorchart-0.0.1-SNAPSHOT.jar')],cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT)
    log.close()
    try:
        for _ in range(180):
            if process.poll() is not None:
                raise RuntimeError(f'Spring exited; inspect .migration/spring-{database}.log')
            try:
                with urllib.request.urlopen(f'http://127.0.0.1:{port}/api/doctorchart/health',timeout=1) as response:
                    # HTTP starts before ApplicationRunner/ApplicationReadyEvent; wait for seed commit.
                    startup_log = (WORK / ('spring-'+database+'.log')).read_text(errors='replace')
                    ready = '[RULES REGISTRY] Successfully loaded' in startup_log or '[RULES REGISTRY] Safe fallback rules activated' in startup_log
                    if response.status == 200 and ready:
                        return process
                    time.sleep(0.5)
            except (OSError,urllib.error.URLError):
                time.sleep(0.5)
        raise RuntimeError('Spring did not become healthy')
    except BaseException:
        process.terminate()
        process.wait(timeout=15)
        raise

def start_go(config,database,port=18084,cwd=WORK,overrides=None):
    env=dict(os.environ,PORT=str(port),SPRING_DATASOURCE_URL=f'jdbc:postgresql://127.0.0.1:{config["db_port"]}/{database}',DB_USERNAME='migration',DB_PASSWORD=config['password'],GATEWAY_SHARED_SECRET=config['gateway'],SERVICES_BILLING_SHARED_SECRET=config['billing'],SERVICES_BILLING_URL='http://127.0.0.1:18999',STUDIES_STORAGE_DIR=str(Path(tempfile.gettempdir())/config['container']/('studies-'+database)))
    if overrides:env.update(overrides)
    log=(WORK/('go-'+database+'.log')).open('wb')
    process=subprocess.Popen([str(ROOT/'bin/doctorchart.exe')],cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT);log.close()
    return process

def prepare():
    WORK.mkdir(exist_ok=True)
    cfg = dict(container='doctorchart-go-fixture-'+secrets.token_hex(4),password=secrets.token_hex(24),gateway=secrets.token_hex(24),billing=secrets.token_hex(24))
    env = dict(os.environ, POSTGRES_PASSWORD=cfg['password'])
    subprocess.run(['docker','run','-d','--name',cfg['container'],'--label','doctorchart-go.migration=fixture','-e','POSTGRES_PASSWORD','-e','POSTGRES_USER=migration','-e','POSTGRES_DB=spring','-p','127.0.0.1::5432','postgres:17-alpine'],env=env,check=True,stdout=subprocess.PIPE)
    cfg['db_port'] = int(command('docker','port',cfg['container'],'5432').decode().strip().split(':')[-1])
    (WORK/'fixture.json').write_text(json.dumps(cfg),encoding='utf-8')
    for _ in range(30):
        if subprocess.run(['docker','exec',cfg['container'],'pg_isready','-U','migration','-d','spring'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0:
            break
        time.sleep(0.5)
    source = start_spring(cfg)
    source.terminate()
    source.wait(timeout=20)
    dump = command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d','spring','--no-owner','--no-privileges')
    (WORK/'fixture.sql').write_bytes(dump)
    sql(cfg,'postgres','CREATE DATABASE target;')
    sql(cfg,'target',dump.decode())
    schema = command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d','spring','--schema-only','--no-owner','--no-privileges').decode()
    # PG dump restrict keys are random per invocation and have no schema semantics.
    schema = '\n'.join(line for line in schema.splitlines() if not line.startswith(('\\restrict','\\unrestrict')))+'\n'
    (ROOT/'migration/schema-reference.sql').write_text(schema,encoding='utf-8')
    print(json.dumps({'fixture':cfg['container'],'postgres':'17-alpine','tables':sql(cfg,'spring',"SELECT count(*) FROM pg_tables WHERE schemaname='public';").strip(),'source':'fresh build of pinned Spring revision','original_project':'untouched'}))

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['prepare','stop'])
    action = parser.parse_args().action
    if action == 'prepare':
        prepare()
    else:
        config = json.loads((WORK/'fixture.json').read_text())
        name = config['container']
        if not name.startswith('doctorchart-go-fixture-'):
            raise RuntimeError('Refusing to remove unrecognized fixture')
        command('docker','rm','-f',name)
