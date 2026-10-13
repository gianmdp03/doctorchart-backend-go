"""Owned local stack: actual Angular/Gateway/DoctorChart, synthetic auth/Billing/S3.

Gateway is built from a hash-recorded copy, never from or into its live checkout.
Stop by creating .migration/integration/stop. All secrets/keys stay in ignored work.
"""
import base64
import datetime as dt
import hashlib
import http.server
import json
import os
from pathlib import Path
import secrets
import shutil
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'migration'))
from fixture import ROOT, WORK, command, sql
from compare import Billing
from verify_documents import S3
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.x509.oid import NameOID

LOCAL = WORK / 'integration'
EVIDENCE = ROOT / 'integration/evidence'
FRONT = ROOT.parent / 'doctorchart-frontend'
ISSUER = 'https://host.docker.internal:19443/auth/v1'
def b64(data): return base64.urlsafe_b64encode(data).rstrip(b'=').decode()
KEY = rsa.generate_private_key(public_exponent=65537, key_size=2048)
USERS = {}
TOKENS = {}
def user(email, benefit=None, role='DOCTOR'):
    module = {'active': True, 'tier': 'PRO', 'expires_at': '2099-01-01T00:00:00Z'}
    if benefit is not None: module.update(benefit)
    return {'id': hashlib.sha256(email.encode()).hexdigest()[:32], 'aud': 'authenticated', 'role': 'authenticated',
            'email': email, 'created_at': '2026-01-01T00:00:00Z', 'updated_at': '2026-01-01T00:00:00Z',
            'email_confirmed_at': '2026-01-01T00:00:00Z', 'identities': [],
            'user_metadata': {'full_name': 'Dra. Prueba Clínica', 'license': 'MN 12345', 'clinic_name': 'Consultorio Fixture'},
            'app_metadata': {'role': role, 'app_modules': {'doctorchart': module}}}
for label, benefit in [('a', {}), ('b', {}), ('browser', {}), ('expired', {'expires_at': '2020-01-01T00:00:00Z'}),
                       ('inactive', {'active': False}), ('invalid', {'tier': 'FAKE'}), ('missing-expiry', {'expires_at': None}),
                       ('string-active', {'active': 'true'})]:
    email = 'doc-'+label+'@fixture.test'; USERS[email] = user(email, benefit)
USERS['admin@fixture.test'] = user('admin@fixture.test', role='SUPER_ADMIN')
USERS['pointly@fixture.test'] = user('pointly@fixture.test')
USERS['pointly@fixture.test']['app_metadata']['app_modules'] = {'pointly': {'active': True, 'tier': 'PRO', 'expires_at': '2099-01-01T00:00:00Z'}}

def session(account):
    now = int(time.time())
    claims = {'iss': ISSUER, 'aud': 'authenticated', 'sub': account['id'], 'iat': now, 'exp': now+3600,
              'role': 'authenticated', 'app_metadata': account['app_metadata'], 'user_metadata': account['user_metadata']}
    body = b64(json.dumps({'alg': 'RS256', 'kid': 'doctorchart-owned-fixture', 'typ': 'JWT'}).encode()) + '.' + b64(json.dumps(claims).encode())
    token = body + '.' + b64(KEY.sign(body.encode(), padding.PKCS1v15(), hashes.SHA256()))
    TOKENS[token] = account
    return {'access_token': token, 'token_type': 'bearer', 'expires_in': 3600, 'expires_at': now+3600,
            'refresh_token': 'owned-refresh:'+account['email'], 'user': account}

class Auth(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def respond(self, status, data):
        payload = json.dumps(data).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.send_header('Access-Control-Allow-Origin', 'http://localhost:4202')
        self.send_header('Access-Control-Allow-Headers', 'authorization, apikey, content-type, x-client-info, x-supabase-api-version')
        self.send_header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS')
        self.end_headers(); self.wfile.write(payload)
    def do_OPTIONS(self): self.respond(200, {})
    def do_GET(self):
        if self.path.endswith('/.well-known/jwks.json'):
            n = KEY.public_key().public_numbers()
            self.respond(200, {'keys': [{'kty': 'RSA', 'kid': 'doctorchart-owned-fixture', 'alg': 'RS256', 'use': 'sig',
                            'n': b64(n.n.to_bytes((n.n.bit_length()+7)//8, 'big')), 'e': b64(n.e.to_bytes(3, 'big'))}]}); return
        account = TOKENS.get(self.headers.get('Authorization', '').removeprefix('Bearer '))
        self.respond(200 if account else 401, account or {'message': 'Invalid synthetic session'})
    def do_POST(self):
        if self.path.startswith('/auth/v1/logout'): self.respond(200, {}); return
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or b'{}')
        email = body.get('email') or body.get('refresh_token', '').removeprefix('owned-refresh:')
        account = USERS.get(email)
        if account and (body.get('password') == 'FixtureOnly123!' or body.get('refresh_token', '').startswith('owned-refresh:')):
            self.respond(200, session(account))
        else: self.respond(400, {'error_code': 'invalid_credentials', 'msg': 'Credenciales de prueba inválidas'})

class StudyS3(S3):
    fail_deletes = False
    fail_reads = False
    fail_puts = False
    def do_POST(self):
        if self.path != '/__fixture/control': self.respond(404); return
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))))
        for key in ['fail_deletes', 'fail_reads', 'fail_puts']:
            if key in body: setattr(type(self), key, body[key] is True)
        self.respond(200, b'{}', 'application/json')
    def do_GET(self):
        if self.fail_reads: self.event(); self.respond(503); return
        super().do_GET()
    def do_HEAD(self):
        if self.fail_reads: self.event(); self.respond(503); return
        super().do_HEAD()
    def do_PUT(self):
        if self.fail_puts: self.event(); self.respond(503); return
        super().do_PUT()
    def do_DELETE(self):
        if self.fail_deletes: self.event(); self.respond(503); return
        super().do_DELETE()

def wait(url, tls=False):
    for _ in range(180):
        if (LOCAL/'stop').exists(): raise RuntimeError('Owned startup cancelled')
        try:
            with urllib.request.urlopen(url, timeout=1, context=ssl.create_default_context(cafile=LOCAL/'ca.pem') if tls else None) as response:
                if response.status == 200: return
        except (OSError, urllib.error.URLError): time.sleep(.5)
    raise RuntimeError('Local startup failed: '+url)

def stop_process(process):
    if process.poll() is None:
        if os.name == 'nt':
            subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        else: process.terminate()
        try: process.wait(timeout=10)
        except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=10)

def start_go(config, database, port=18084, cwd=WORK, overrides=None):
    # This current Go fixture starts against a new empty database. PostgreSQL
    # baseline creation and catalog seeds belong entirely to the Go bootstrap.
    user = urllib.parse.quote('migration', safe='')
    password = urllib.parse.quote(config['password'], safe='')
    database_name = urllib.parse.quote(database, safe='')
    address = f'postgresql://{user}:{password}@127.0.0.1:{config["db_port"]}/{database_name}?sslmode=disable'
    env = dict(os.environ, PORT=str(port), DATABASE_URL=address,
               GATEWAY_SHARED_SECRET=config['gateway'], SERVICES_BILLING_SHARED_SECRET=config['billing'],
               SERVICES_BILLING_URL='http://127.0.0.1:18999',
               STUDIES_STORAGE_DIR=str(Path(tempfile.gettempdir())/config['container']/('studies-'+database)))
    if overrides: env.update(overrides)
    with (WORK/('go-'+database+'.log')).open('wb') as log:
        return subprocess.Popen([str(ROOT/'bin/doctorchart.exe')], cwd=cwd, env=env, stdout=log, stderr=log)

def main():
    LOCAL.mkdir(parents=True, exist_ok=True); EVIDENCE.mkdir(parents=True, exist_ok=True)
    stop = LOCAL/'stop'; stop.unlink(missing_ok=True)
    cfg = json.loads((WORK/'fixture.json').read_text())
    if not cfg['container'].startswith('doctorchart-go-fixture-'): raise RuntimeError('Unowned fixture')
    # A launch creates a new database/key; checkpoints belong to the old fixture.
    for name in ['browser-state.json', 'browser-progress.json', 'pause-backend', 'restart-backend']:
        (LOCAL/name).unlink(missing_ok=True)
    # Docker Desktop can reassign an ephemeral published port after a restart.
    cfg['db_port'] = int(command('docker','port',cfg['container'],'5432/tcp').decode().strip().rsplit(':',1)[1])
    (WORK/'fixture.json').write_text(json.dumps(cfg, indent=2)+'\n')
    gateway_source = ROOT.parent/'api-ai-gateway-go'; gateway_copy = LOCAL/'gateway-source'
    if gateway_copy.exists():
        if gateway_copy.resolve().parent != LOCAL.resolve(): raise RuntimeError('Unowned gateway copy')
        for path in list(gateway_copy.rglob('*.go'))+[gateway_copy/'go.mod', gateway_copy/'go.sum']:
            if path.resolve().is_relative_to(gateway_copy.resolve()): path.unlink(missing_ok=True)
            else: raise RuntimeError('Gateway copy contains an external link')
    manifest = {}
    for project in ['api-ai-gateway-go', 'billing-service-go']:
        source = ROOT.parent/project
        for path in sorted(list(source.rglob('*.go'))+[source/'go.mod', source/'go.sum']):
            relative = path.relative_to(source)
            manifest[project+'/'+relative.as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
            if project == 'api-ai-gateway-go':
                dest = gateway_copy/relative; dest.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(path, dest)
    (EVIDENCE/'reference-manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
    with (LOCAL/'gateway-build.log').open('wb') as log:
        subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-o', str(LOCAL/'gateway'), '.'], cwd=gateway_copy,
                       env=dict(os.environ, GOOS='linux', GOARCH='amd64', CGO_ENABLED='0'), stdout=log, stderr=log, check=True)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'DoctorChart owned local fixture')])
    now = dt.datetime.now(dt.timezone.utc)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(KEY.public_key())
            .serial_number(x509.random_serial_number()).not_valid_before(now-dt.timedelta(minutes=5)).not_valid_after(now+dt.timedelta(days=2))
            .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
            .add_extension(x509.SubjectAlternativeName([x509.DNSName('localhost'), x509.DNSName('host.docker.internal')]), critical=False)
            .sign(KEY, hashes.SHA256()))
    (LOCAL/'ca.pem').write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    (LOCAL/'key.pem').write_bytes(KEY.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    auth = http.server.ThreadingHTTPServer(('0.0.0.0',19443), Auth)
    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER); tls.load_cert_chain(LOCAL/'ca.pem', LOCAL/'key.pem'); auth.socket = tls.wrap_socket(auth.socket, server_side=True)
    # HTTP fixtures are intentionally reachable by the owned Docker gateway.
    servers = [auth, http.server.ThreadingHTTPServer(('0.0.0.0',18999), Billing), http.server.ThreadingHTTPServer(('127.0.0.1',19200), StudyS3)]
    Billing.catalog = {'BASIC': {'max_patients': 2, 'max_records_per_patient': 2}, 'PRO': {'max_patients': 50, 'max_records_per_patient': 100}, 'UNLIMITED': {'max_patients': -1, 'max_records_per_patient': -1}}
    for server in servers: threading.Thread(target=server.serve_forever, daemon=True).start()
    database = 'integration_'+secrets.token_hex(4)
    sql(cfg, 'postgres', 'CREATE DATABASE '+database+';')
    processes = []; container = 'doctorchart-integration-gateway-'+secrets.token_hex(4)
    try:
        overrides = {'STORAGE_TYPE': 's3', 'S3_ENDPOINT': 'http://127.0.0.1:19200', 'S3_BUCKET': 'integration-fixture',
                     'S3_ACCESS_KEY': 'owned-test-access', 'S3_SECRET_KEY': 'owned-test-secret', 'S3_REGION': 'auto'}
        processes.append(start_go(cfg, database, 18484, overrides=overrides)); wait('http://127.0.0.1:18484/api/doctorchart/health')
        environment = dict(os.environ, PORT='8000', GATEWAY_SHARED_SECRET=cfg['gateway'],
            POINTLY_SERVICE_URL='http://host.docker.internal:18999', BILLING_SERVICE_URL='http://host.docker.internal:18999',
            DOCTORCHART_SERVICE_URL='http://host.docker.internal:18484', STOCK_SERVICE_URL='http://host.docker.internal:18999',
            SUPABASE_JWT_ISSUER=ISSUER, SUPABASE_JWKS_URL=ISSUER+'/.well-known/jwks.json', SUPABASE_JWT_AUDIENCE='authenticated',
            CORS_ALLOWED_ORIGINS='http://localhost:4202', GEMINI_API_KEY='owned-unusable-fixture-key', SSL_CERT_FILE='/fixture/ca.pem')
        args = ['docker','run','-d','--name',container,'-p','127.0.0.1:18080:8000','--mount','type=bind,source='+str(LOCAL)+',target=/fixture,readonly','-w','/fixture']
        for key in ['PORT','GATEWAY_SHARED_SECRET','POINTLY_SERVICE_URL','BILLING_SERVICE_URL','DOCTORCHART_SERVICE_URL','STOCK_SERVICE_URL',
                    'SUPABASE_JWT_ISSUER','SUPABASE_JWKS_URL','SUPABASE_JWT_AUDIENCE','CORS_ALLOWED_ORIGINS','GEMINI_API_KEY','SSL_CERT_FILE']:
            args += ['-e',key]
        subprocess.run(args+['golang:1.27.2','/fixture/gateway'], env=environment, check=True, stdout=subprocess.DEVNULL)
        wait('http://127.0.0.1:18080/health')
        front_env = dict(os.environ, PUBLIC_APP_URL='http://localhost:4202', PUBLIC_API_URL='http://localhost:18080/api/doctorchart',
            PUBLIC_AI_GATEWAY_URL='http://localhost:18080/api/ai/doctorchart/chat', PUBLIC_SYNTAH_PLANS_URL='http://localhost:18999/fixture-plans',
            PUBLIC_SUPABASE_URL='https://localhost:19443', PUBLIC_SUPABASE_PUBLISHABLE_KEY='sb_publishable_doctorchart_local_fixture')
        with (LOCAL/'angular.log').open('wb') as log:
            processes.append(subprocess.Popen(['cmd.exe','/d','/c','npm start -- --port 4202'], cwd=FRONT, env=front_env, stdout=log, stderr=log))
        wait('http://localhost:4202')
        context = {'fixture': cfg, 'database': database, 'gateway_container': container, 'doctorchart_pid': processes[0].pid,
                   'gateway_port':18080, 'backend_port':18484, 'frontend_port':4202, 'issuer':ISSUER}
        (LOCAL/'context.json').write_text(json.dumps(context, indent=2))
        print('READY: actual Angular :4202 → snapshot Go Gateway :18080 → DoctorChart Go :18484; owned PostgreSQL/auth/Billing/S3.', flush=True)
        (LOCAL/'schema-before.sql').write_bytes(command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d',database,'--schema-only','--no-owner','--no-privileges'))
        while not stop.exists():
            pause = LOCAL/'pause-backend'; restart = LOCAL/'restart-backend'
            if pause.exists(): pause.unlink(); stop_process(processes[0])
            if restart.exists():
                restart.unlink(); stop_process(processes[0]); processes[0] = start_go(cfg, database, 18484, overrides=overrides)
                wait('http://127.0.0.1:18484/api/doctorchart/health')
                context['doctorchart_pid'] = processes[0].pid
                (LOCAL/'context.json').write_text(json.dumps(context, indent=2))
            time.sleep(.5)
    finally:
        for process in reversed(processes): stop_process(process)
        subprocess.run(['docker','rm','-f',container], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for server in servers: server.shutdown(); server.server_close()
        print('Owned integration stack stopped. Disposable database retained for review.', flush=True)

if __name__ == '__main__': main()
