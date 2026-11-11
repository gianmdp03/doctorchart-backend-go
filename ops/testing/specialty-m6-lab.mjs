// Owned, disposable M6 validation only. Never reads a development/production DSN.
import {readFileSync,writeFileSync,mkdirSync,existsSync} from 'node:fs';
import {resolve,join,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {tmpdir} from 'node:os';
import {randomBytes,X509Certificate,createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
const backend=resolve(dirname(fileURLToPath(import.meta.url)),'../..'),repos=resolve(backend,'..');
const frontend=join(repos,'doctorchart-frontend'),gateway=join(repos,'api-ai-gateway-go');
const tag='doctorchart-specialty-m6-20261110',project='doctorchart-specialty-m6',root=join(tmpdir(),tag);
const command=process.argv[2]??'status';
const run=(exe,args,options={})=>execFileSync(exe,args,{stdio:'inherit',...options});
const capture=(exe,args)=>execFileSync(exe,args,{encoding:'utf8'}).trim();
const json=(p,value)=>writeFileSync(p,JSON.stringify(value,null,2),{mode:0o600});
const docker=(...args)=>run('docker',args);
const compose=(...args)=>docker('compose','-p',project,'-f',join(root,'compose.json'),...args);
const mount=p=>p.replaceAll('\\','/');
function owned(){
 const state=JSON.parse(readFileSync(join(root,'settings.json')));if(state.task!==tag)throw Error('Lab identity mismatch');
 const network=project+'_default';
 if(capture('docker',['network','ls','--filter','name='+network,'--format','{{.Name}}']).split('\n').includes(network)){
  const [info]=JSON.parse(capture('docker',['network','inspect',network]));if(info.Labels?.['codex.task']!==tag)throw Error('Network ownership mismatch');
 }
 for(const name of ['pg17','pg18','support','backend','gateway','converter','mailpit']){
  const found=capture('docker',['ps','-aq','--filter',`name=^${project}-${name}$`]);if(!found)continue;
  const [info]=JSON.parse(capture('docker',['inspect',found]));if(info.Config.Labels['codex.task']!==tag)throw Error('Container ownership mismatch');
  const expected={pg17:'55468',pg18:'55470',support:'19447',gateway:'18087',mailpit:'18029'}[name];
  if(expected&&!Object.values(info.NetworkSettings.Ports??{}).flat().some(p=>p?.HostIp==='127.0.0.1'&&p.HostPort===expected))throw Error('Lab port mismatch');
 }
 return state;
}
if(command==='prepare'){
 mkdirSync(root,{recursive:true});for(const d of ['bin','certs','storage'])mkdirSync(join(root,d),{recursive:true});
 if(!existsSync(join(root,'settings.json')))json(join(root,'settings.json'),{task:tag,origin:'https://localhost:4353',email:'doctor-m6@local.test',password:randomBytes(24).toString('hex'),refresh:randomBytes(32).toString('hex'),gateway:randomBytes(32).toString('hex'),billing:randomBytes(32).toString('hex'),image:randomBytes(32).toString('hex'),database:'fixture_m6_specialties',notifications:randomBytes(32).toString('base64')});
 const s=owned();
 for(const name of ['gomod','gocache']){const volume=tag+'-'+name;const found=capture('docker',['volume','ls','-q','--filter',`name=^${volume}$`]);if(found){const [v]=JSON.parse(capture('docker',['volume','inspect',volume]));if(v.Labels?.['codex.task']!==tag)throw Error('Cache ownership mismatch');}else docker('volume','create','--label','codex.task='+tag,volume);}
 writeFileSync(join(root,'cert.sh'),'set -eu\ncd /src\ngo run ./ops/local/cert.go /lab/certs\n');
 if(!existsSync(join(root,'certs/cert.pem')))docker('run','--rm','--label','codex.task='+tag,'--mount',`type=bind,source=${backend},target=/src,readonly`,'--mount',`type=bind,source=${root},target=/lab`,'--mount',`type=volume,source=${tag}-gocache,target=/root/.cache/go-build`,'golang:1.27.3','sh','/lab/cert.sh');
 const labels={'codex.task':tag},certs=mount(join(root,'certs'))+':/certs:ro',bin=mount(join(root,'bin'))+':/app:ro';
 const postgres=major=>({image:'postgres:'+major,container_name:project+'-pg'+major,labels,environment:{POSTGRES_USER:'migration_fixture',POSTGRES_PASSWORD:s.database,POSTGRES_DB:major===18?'doctorchart_m6_e2e':'postgres'},ports:[`127.0.0.1:${major===17?55468:55470}:5432`],tmpfs:[major===17?'/var/lib/postgresql/data':'/var/lib/postgresql'],healthcheck:{test:['CMD','pg_isready','-U','migration_fixture'],interval:'2s',timeout:'3s',retries:30}});
 const services={pg17:postgres(17),pg18:postgres(18),
 support:{image:'node:24.20.0',container_name:project+'-support',labels,command:['node','/support.mjs'],ports:['127.0.0.1:19447:9443'],volumes:[mount(join(backend,'ops/local/support.mjs'))+':/support.mjs:ro',mount(root)+':/local',certs]},
 backend:{image:'golang:1.27.3',container_name:project+'-backend',labels,command:['/app/backend'],volumes:[bin,mount(join(root,'storage'))+':/data',certs],environment:{PORT:'8083',DATABASE_URL:`postgres://migration_fixture:${s.database}@pg18:5432/doctorchart_m6_e2e?sslmode=disable`,GATEWAY_SHARED_SECRET:s.gateway,SERVICES_BILLING_SHARED_SECRET:s.billing,SERVICES_BILLING_URL:'http://support:9000',STORAGE_TYPE:'local',STUDIES_STORAGE_DIR:'/data/studies',MEDIA_PROVIDER:'local',MEDIA_ENVIRONMENT:'development',MEDIA_LOCAL_DIR:'/data/media',MEDIA_RECOVERY_LOCAL_DIR:'/data/recovery',IMAGE_CONVERTER_URL:'http://converter:8086',IMAGE_CONVERTER_API_KEY:s.image,STUDIES_ALLOW_IMAGE_TRANSFORM:'true',SCHEDULES_ENABLED:'true',BOOKINGS_ENABLED:'true',PUBLIC_BOOKINGS_ENABLED:'true',STAFF_ENABLED:'true',STAFF_ORIGIN:s.origin,NOTIFICATIONS_ENABLED:'true',NOTIFICATIONS_KEY:s.notifications,NOTIFICATIONS_PUBLIC_ORIGIN:s.origin,NOTIFICATIONS_SMTP_HOST:'mailpit',NOTIFICATIONS_SMTP_PORT:'1025',NOTIFICATIONS_SMTP_FROM:'turnos@doctorchart.local',NOTIFICATIONS_SMTP_TLS:'starttls',SSL_CERT_FILE:'/certs/cert.pem'},depends_on:{pg18:{condition:'service_healthy'},support:{condition:'service_started'},mailpit:{condition:'service_started'}}},
 gateway:{image:'golang:1.27.3',container_name:project+'-gateway',labels,command:['/app/gateway'],ports:['127.0.0.1:18087:8000'],volumes:[bin,certs],environment:{PORT:'8000',GATEWAY_SHARED_SECRET:s.gateway,POINTLY_SERVICE_URL:'http://support:9000',BILLING_SERVICE_URL:'http://support:9000',DOCTORCHART_SERVICE_URL:'http://backend:8083',SUPABASE_JWT_ISSUER:'https://local-support:9443/auth/v1',SUPABASE_JWKS_URL:'https://local-support:9443/auth/v1/.well-known/jwks.json',SUPABASE_JWT_AUDIENCE:'authenticated',CORS_ALLOWED_ORIGINS:s.origin,SSL_CERT_FILE:'/certs/cert.pem'},depends_on:['backend','support']},
 converter:{image:'bycode/image-email:local',container_name:project+'-converter',labels,environment:{LISTEN_ADDR:':8086',APPLICATIONS:'doctorchart',PROFILES:'doctorchart-study,avatar',APP_DOCTORCHART_API_KEY:s.image,APP_DOCTORCHART_PROFILES:'doctorchart-study,avatar',PROFILE_DOCTORCHART_STUDY_MODE:'transform',PROFILE_DOCTORCHART_STUDY_FORMAT:'webp',PROFILE_DOCTORCHART_STUDY_QUALITY:'75',PROFILE_DOCTORCHART_STUDY_MAX_WIDTH:'2048',PROFILE_DOCTORCHART_STUDY_MAX_HEIGHT:'2048',MAIL_ENABLED:'false'}},
 mailpit:{image:'axllent/mailpit:v1.27.10',container_name:project+'-mailpit',labels,ports:['127.0.0.1:18029:8025'],volumes:[certs],environment:{MP_SMTP_TLS_CERT:'/certs/cert.pem',MP_SMTP_TLS_KEY:'/certs/key.pem',MP_SMTP_REQUIRE_STARTTLS:'true'}}};
 Object.assign(services.converter.environment,{PROFILE_AVATAR_MAX_WIDTH:'512',PROFILE_AVATAR_MAX_HEIGHT:'512',PROFILE_AVATAR_FORMAT:'webp',PROFILE_AVATAR_FIT:'cover',PROFILE_AVATAR_QUALITY:'80'});
 services.support.networks={default:{aliases:['local-support']}};
 json(join(root,'compose.json'),{services,networks:{default:{labels}}});
 const local=JSON.parse(readFileSync(join(frontend,'config/local.public.json')));
 const values={...local,publicSiteUrl:s.origin,apiGatewayOrigin:s.origin,applicationUrls:{bycode:s.origin,pointly:s.origin,doctorchart:s.origin},supabase:{url:s.origin,publishableKey:local.supabase.publishableKey}};
 // Reuse the existing development adapter's public client identifier only in
 // the opt-in lab build. It is never used to pass production validation.
 const generated=join(frontend,'src/environments/environment.m6.generated.ts');if(existsSync(generated)&&!readFileSync(generated,'utf8').startsWith('// Owned M6 lab only'))throw Error('Refusing to overwrite foreign generated config');
 writeFileSync(generated,"// Owned M6 lab only; never deploy.\nimport {baseEnvironment} from './environment.base';\nexport const environment={...baseEnvironment,production:false,appUrl:"+JSON.stringify(s.origin)+",apiUrl:"+JSON.stringify(s.origin+'/api/doctorchart')+",aiGatewayUrl:"+JSON.stringify(s.origin+'/api/ai/doctorchart/chat')+",bycodePlansUrl:"+JSON.stringify(s.origin+'/dashboard')+",supabase:"+JSON.stringify(values.supabase)+"};\n");
 writeFileSync(join(root,'build.sh'),`set -eu\ncd /src\ngo build -mod=readonly -o /lab/bin/backend ./cmd/api\ncd /gateway\ngo build -mod=readonly -o /lab/bin/gateway .\n`);
 compose('up','-d','pg17','pg18','support','converter','mailpit');
 console.log('Owned M6 lab prepared; no existing database or frontend local config changed.');
}else if(command==='build'){
 owned();docker('run','--rm','--label','codex.task='+tag,'--mount',`type=bind,source=${backend},target=/src,readonly`,'--mount',`type=bind,source=${gateway},target=/gateway,readonly`,'--mount',`type=bind,source=${join(repos,'bycode-image-email-service-go/pkg/media')},target=/bycode-image-email-service-go/pkg/media,readonly`,'--mount',`type=bind,source=${root},target=/lab`,'--mount',`type=volume,source=${tag}-gomod,target=/go/pkg/mod`,'--mount',`type=volume,source=${tag}-gocache,target=/root/.cache/go-build`,'-e','GOMAXPROCS=2','golang:1.27.3','sh','/lab/build.sh');
}else if(command==='up'){owned();compose('up','-d','backend','gateway');
}else if(command==='reset-e2e'){
 owned();compose('stop','gateway','backend');
 docker('exec',project+'-pg18','dropdb','-U','migration_fixture','--force','doctorchart_m6_e2e');
 docker('exec',project+'-pg18','createdb','-U','migration_fixture','doctorchart_m6_e2e');
 compose('up','-d','backend','gateway');
}else if(command==='go-gates'){
 const s=owned(),major=Number(process.argv[3]),race=process.argv[4]==='race';
 if(![17,18].includes(major)||process.argv[4]&&!race)throw Error('Use go-gates 17|18 [race]');
 if(s.database!=='fixture_m6_specialties')throw Error('Unexpected disposable database identity');
 const databases={DOCTORCHART_SCHEMA_TEST_DATABASE_URL:'doctorchart_schema_test',DOCTORCHART_INTEGRITY_TEST_DATABASE_URL:'doctorchart_integrity_test',DOCTORCHART_GROWTH_TEST_DSN:'doctorchart_growth_test',DOCTORCHART_CLINICS_TEST_DATABASE_URL:'doctorchart_clinics_test',DOCTORCHART_FOLLOWUP_TEST_DATABASE_URL:'doctorchart_followups_query_test',DOCTORCHART_STARTUP_TEST_DATABASE_URL:'doctorchart_startup_test',DOCTORCHART_ENTITLEMENTS_TEST_DATABASE_URL:'doctorchart_entitlements_test'};
 const container=project+'-pg'+major,port=major===17?55468:55470;
 for(const name of Object.values(databases))if(capture('docker',['exec',container,'psql','-U','migration_fixture','-d','postgres','-Atc',"SELECT 1 FROM pg_database WHERE datname='"+name+"'"])!=='1')docker('exec',container,'createdb','-U','migration_fixture',name);
 writeFileSync(join(root,'proxy.go'),`package main
import("io";"net";"os";"log")
func main(){l,e:=net.Listen("tcp",os.Args[1]);if e!=nil{log.Fatal(e)};for{a,e:=l.Accept();if e!=nil{log.Fatal(e)};go func(){defer a.Close();b,e:=net.Dial("tcp",os.Args[2]);if e!=nil{return};defer b.Close();go func(){io.Copy(b,a);if c,ok:=b.(*net.TCPConn);ok{c.CloseWrite()}}();io.Copy(a,b)}()}}
`);
 const dsn=name=>`postgres://migration_fixture:${s.database}@127.0.0.1:${port}/${name}?sslmode=disable`;
 writeFileSync(join(root,'gates-'+major+'.sh'),`set -eu
cd /src
go version
go build -o /tmp/fixture-proxy /lab/proxy.go
/tmp/fixture-proxy 127.0.0.1:${port} 127.0.0.1:5432 &
proxy_pid=$!
trap 'kill "$proxy_pid"' EXIT
test -z "$(gofmt -l cmd db internal ops)"
go build ./...
go vet ./...
export BASELINE_TEST_ADMIN_DSN='${dsn('postgres')}'
${Object.entries(databases).map(([key,name])=>`export ${key}='${dsn(name)}'`).join('\n')}
go test ${race?'-race ':''}-p 1 ./... -count=1
`);
 docker('run','--rm','--label','codex.task='+tag,'--network','container:'+container,'--mount',`type=bind,source=${backend},target=/src,readonly`,'--mount',`type=bind,source=${join(repos,'bycode-image-email-service-go/pkg/media')},target=/bycode-image-email-service-go/pkg/media,readonly`,'--mount',`type=bind,source=${root},target=/lab,readonly`,'--mount',`type=volume,source=${tag}-gomod,target=/go/pkg/mod`,'--mount',`type=volume,source=${tag}-gocache,target=/root/.cache/go-build`,'-e','GOMAXPROCS=2','golang:1.27.3','sh','/lab/gates-'+major+'.sh');
}else if(command==='status'){owned();compose('ps');
}else if(command==='down'){
 owned();compose('down');for(const suffix of ['gomod','gocache']){const name=tag+'-'+suffix;const [v]=JSON.parse(capture('docker',['volume','inspect',name]));if(v.Labels?.['codex.task']!==tag)throw Error('Cache ownership mismatch');docker('volume','rm',name);}
 console.log('Only owned M6 Docker resources removed; evidence retained in the temporary lab folder.');
}else if(command==='browser-info'){
 owned();const cert=new X509Certificate(readFileSync(join(root,'certs/cert.pem')));console.log(JSON.stringify({root,origin:'https://localhost:4353',spki:createHash('sha256').update(cert.publicKey.export({type:'spki',format:'der'})).digest('base64')}));
}else throw Error('Use prepare, build, up, reset-e2e, go-gates 17|18 [race], status, browser-info or down');
