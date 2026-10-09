"""Replace only our disposable Gateway container with a fresh read-only source snapshot."""
import hashlib, json, os, pathlib, shutil, subprocess, time, urllib.request
ROOT=pathlib.Path(__file__).resolve().parents[1]
LOCAL=ROOT/'.migration/integration'; EVIDENCE=ROOT/'integration/evidence'
context=json.loads((LOCAL/'context.json').read_text()); cfg=context['fixture']
name=context['gateway_container']
if not name.startswith('doctorchart-integration-gateway-'):raise RuntimeError('Unowned container')
copy=LOCAL/'gateway-current-source'; manifest={}
for project in ['api-gateway','billing-service-go']:
    source=ROOT.parent/project
    for file in sorted(list(source.rglob('*.go'))+[source/'go.mod',source/'go.sum']):
        relative=file.relative_to(source)
        manifest[project+'/'+relative.as_posix()]=hashlib.sha256(file.read_bytes()).hexdigest()
        if project=='api-gateway':
            target=copy/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(file,target)
(EVIDENCE/'reference-manifest-current.json').write_text(json.dumps(manifest,indent=2),encoding='utf8')
with (EVIDENCE/'gateway-current-build.log').open('wb') as out:
    subprocess.run(['go','build','-mod=readonly','-trimpath','-o',str(LOCAL/'gateway-current'),'.'],cwd=copy,
        env=dict(os.environ,GOOS='linux',GOARCH='amd64',CGO_ENABLED='0'),stdout=out,stderr=out,check=True)
environment=dict(os.environ,PORT='8000',GATEWAY_SHARED_SECRET=cfg['gateway'],
    POINTLY_SERVICE_URL='http://host.docker.internal:18999',BILLING_SERVICE_URL='http://host.docker.internal:18999',
    DOCTORCHART_SERVICE_URL='http://host.docker.internal:18484',STOCK_SERVICE_URL='http://host.docker.internal:18999',
    SUPABASE_JWT_ISSUER=context['issuer'],SUPABASE_JWKS_URL=context['issuer']+'/.well-known/jwks.json',
    SUPABASE_JWT_AUDIENCE='authenticated',CORS_ALLOWED_ORIGINS='http://localhost:4202',
    GEMINI_API_KEY='owned-unusable-fixture-key',SSL_CERT_FILE='/fixture/ca.pem')
subprocess.run(['docker','rm','-f',name],check=True,stdout=subprocess.DEVNULL)
args=['docker','run','-d','--name',name,'-p','127.0.0.1:18080:8000','--mount','type=bind,source='+str(LOCAL)+',target=/fixture,readonly','-w','/fixture']
for key in ['PORT','GATEWAY_SHARED_SECRET','POINTLY_SERVICE_URL','BILLING_SERVICE_URL','DOCTORCHART_SERVICE_URL','STOCK_SERVICE_URL','SUPABASE_JWT_ISSUER','SUPABASE_JWKS_URL','SUPABASE_JWT_AUDIENCE','CORS_ALLOWED_ORIGINS','GEMINI_API_KEY','SSL_CERT_FILE']:args+=['-e',key]
subprocess.run(args+['golang:1.27.2','/fixture/gateway-current'],env=environment,check=True,stdout=subprocess.DEVNULL)
for _ in range(80):
    try:
        if urllib.request.urlopen('http://localhost:18080/health',timeout=1).status==200:break
    except OSError:time.sleep(.2)
else:raise RuntimeError('Current Gateway snapshot did not start')
context['gateway_binary']='gateway-current'
(LOCAL/'context.json').write_text(json.dumps(context,indent=2))
print('Owned Gateway updated from current source snapshot; original repositories untouched.')
