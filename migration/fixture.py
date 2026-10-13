"""Owned PostgreSQL 17 fixture helpers; no Java or historical schema bootstrap."""
import argparse
import json
import os
import secrets
import subprocess
import time
import atexit
import threading
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / '.migration'
FIXTURE_LABEL_KEY = 'doctorchart-go.integration'
FIXTURE_LABEL_VALUE = 'fixture'

def command(*args, input=None):
    return subprocess.run(args, input=input, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout

def verify_fixture(config):
    if not isinstance(config, dict):
        raise RuntimeError('Invalid fixture descriptor')
    name = config.get('container', '')
    identifier = config.get('container_id', '')
    label = config.get('label')
    port = config.get('db_port')
    if not isinstance(name, str) or not isinstance(identifier, str):
        raise RuntimeError('Invalid fixture identity')
    suffix = name.removeprefix('doctorchart-go-fixture-')
    if (not name.startswith('doctorchart-go-fixture-') or len(suffix) != 8 or
            any(char not in '0123456789abcdef' for char in suffix) or
            len(identifier) != 64 or any(char not in '0123456789abcdef' for char in identifier) or
            label != {FIXTURE_LABEL_KEY: FIXTURE_LABEL_VALUE} or
            not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535):
        raise RuntimeError('Fixture requires its exact container ID, label and loopback port; old descriptors are not adopted')
    metadata = json.loads(command('docker', 'inspect', name))
    if len(metadata) != 1:
        raise RuntimeError('Fixture inspection did not identify one owned container')
    actual = metadata[0]
    bindings = actual.get('NetworkSettings', {}).get('Ports', {}).get('5432/tcp')
    if (actual.get('Id') != identifier or
            actual.get('Config', {}).get('Labels', {}).get(FIXTURE_LABEL_KEY) != FIXTURE_LABEL_VALUE or
            actual.get('Config', {}).get('Image') != 'postgres:17-alpine' or
            bindings != [{'HostIp': '127.0.0.1', 'HostPort': str(port)}]):
        raise RuntimeError('Fixture ownership or PostgreSQL loopback binding does not match its descriptor')

_sessions={}
_sessions_lock=threading.Lock()
def sql(config, db, statement):
    verify_fixture(config)
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

def prepare():
    WORK.mkdir(exist_ok=True)
    descriptor = WORK / 'fixture.json'
    if descriptor.exists():
        raise RuntimeError('Fixture descriptor already exists; do not overwrite an existing fixture')
    cfg = dict(container='doctorchart-go-fixture-'+secrets.token_hex(4),
               password=secrets.token_hex(24), gateway=secrets.token_hex(24), billing=secrets.token_hex(24),
               label={FIXTURE_LABEL_KEY: FIXTURE_LABEL_VALUE})
    env = dict(os.environ, POSTGRES_PASSWORD=cfg['password'])
    result = subprocess.run(['docker', 'run', '-d', '--name', cfg['container'],
                             '--label', FIXTURE_LABEL_KEY+'='+FIXTURE_LABEL_VALUE,
                             '--tmpfs', '/var/lib/postgresql/data',
                             '-e', 'POSTGRES_PASSWORD', '-e', 'POSTGRES_USER=migration', '-e', 'POSTGRES_DB=target',
                             '-p', '127.0.0.1::5432', 'postgres:17-alpine'],
                            env=env, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    cfg['container_id'] = result.stdout.decode('ascii').strip()
    cfg['db_port'] = int(command('docker', 'port', cfg['container'], '5432/tcp').decode().strip().rsplit(':', 1)[1])
    verify_fixture(cfg)
    try:
        with descriptor.open('x', encoding='utf-8') as output:
            output.write(json.dumps(cfg, indent=2)+'\n')
    except FileExistsError:
        # A concurrent prepare owns the existing descriptor. Remove only the
        # container whose exact identity was just verified for this invocation.
        verify_fixture(cfg)
        command('docker', 'rm', '-f', cfg['container'])
        raise RuntimeError('Fixture descriptor appeared concurrently; it was not overwritten')
    for _ in range(30):
        verify_fixture(cfg)
        result = subprocess.run(['docker', 'exec', cfg['container'], 'pg_isready', '-U', 'migration', '-d', 'target'],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if result.returncode == 0:
            print(json.dumps({'fixture': cfg['container'], 'container_id': cfg['container_id'],
                              'label': cfg['label'], 'loopback_port': cfg['db_port'],
                              'postgres': '17-alpine', 'database': 'target', 'schema': 'empty'}))
            return
        time.sleep(0.5)
    raise RuntimeError('Owned PostgreSQL fixture did not become ready; its descriptor was retained for explicit cleanup')

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['prepare', 'stop'])
    action = parser.parse_args().action
    if action == 'prepare':
        prepare()
    else:
        descriptor = WORK / 'fixture.json'
        config = json.loads(descriptor.read_text())
        verify_fixture(config)
        command('docker', 'rm', '-f', config['container'])
        descriptor.unlink()
