// Owned, disposable M6 validation only. Never reads a development/production DSN.
import {readFileSync,writeFileSync,mkdirSync,existsSync,renameSync,lstatSync,realpathSync} from 'node:fs';
import {resolve,join,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {tmpdir} from 'node:os';
import {randomBytes,X509Certificate,createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {restoreSP04E} from './recovery-sp04e.mjs';
import {manualHealth, seedManual, storeManualAccess} from '../local/manual-fixtures.mjs';
const backend=resolve(dirname(fileURLToPath(import.meta.url)),'../..'),repos=resolve(backend,'..');
const frontend=join(repos,'doctorchart-frontend'),gateway=join(repos,'api-ai-gateway-go');
const manual=process.argv.includes('--manual');
const tag=manual?'doctorchart-manual-sp05a':'doctorchart-specialty-m6-20261110';
const project=manual?'doctorchart-manual':'doctorchart-specialty-m6';
const root=manual?join(process.env.USERPROFILE,'Documents','DoctorChartManual'):join(tmpdir(),tag);
const origin=manual?'https://localhost:4354':'https://localhost:4353';
const command=process.argv[2]??'status';
const run=(exe,args,options={})=>execFileSync(exe,args,{stdio:'inherit',...options});
const capture=(exe,args)=>execFileSync(exe,args,{encoding:'utf8'}).trim();
const json=(p,value)=>writeFileSync(p,JSON.stringify(value,null,2),{mode:0o600});
const docker=(...args)=>run('docker',args);
const compose=(...args)=>docker('compose','-p',project,'-f',join(root,'compose.json'),...args);
const mount=p=>p.replaceAll('\\','/');
const appPassword='fixture_m6_doctorchart_app';
const mediaPassword='fixture_m6_doctorchart_media';
const ownerPassword='fixture_m6_doctorchart_owner';
function initialize(s){
 if(s.database!=='fixture_m6_specialties')throw Error('Unexpected disposable database identity');
 const owner=`postgres://doctorchart_owner:${ownerPassword}@pg18:5432/doctorchart_m6_e2e?sslmode=disable`;
 compose('run','--rm','--no-deps','-e',`DATABASE_URL=${owner}`,'--entrypoint','/app/backend','backend','init-db');
 run('docker',['exec','-i',project+'-pg18','psql','-X','-v','ON_ERROR_STOP=1','-U','migration_fixture','-d','doctorchart_m6_e2e'],
  {input:readFileSync(join(backend,'internal/testfixture/synthetic-template-approvals.sql')),stdio:['pipe','inherit','inherit']});
}
function owned(){
 const state=JSON.parse(readFileSync(join(root,'settings.json')));if(state.task!==tag)throw Error('Lab identity mismatch');
 for(const suffix of manual?['default','edge']:['default']){
  const network=project+'_'+suffix;
  if(capture('docker',['network','ls','--filter','name='+network,'--format','{{.Name}}']).split('\n').includes(network)){
   const [info]=JSON.parse(capture('docker',['network','inspect',network]));if(info.Labels?.['codex.task']!==tag)throw Error('Network ownership mismatch');
  }
 }
 for(const name of ['pg17','pg18','pg18-restore','support','backend','gateway','converter','mailpit','web']){
  const found=capture('docker',['ps','-aq','--filter',`name=^${project}-${name}$`]);if(!found)continue;
  const [info]=JSON.parse(capture('docker',['inspect',found]));if(info.Config.Labels['codex.task']!==tag)throw Error('Container ownership mismatch');
  const expected=(manual?{web:'4354',mailpit:'18030'}:{pg17:'55468',pg18:'55470',support:'19447',gateway:'18087',mailpit:'18029'})[name];
  const bindings=Object.values(info.HostConfig.PortBindings??{}).flat();
  if(expected&&!bindings.some(p=>p?.HostIp==='127.0.0.1'&&p.HostPort===expected)||manual&&bindings.length!==(expected?1:0))throw Error('Lab port mismatch');
  if(manual&&name==='pg18'&&!info.Mounts.some(m=>m.Name===project+'_pgdata'&&m.Destination==='/var/lib/postgresql'))throw Error('Manual lab volume mismatch');
 }
 return state;
}
if(command==='prepare'){
 mkdirSync(root,{recursive:true});for(const d of ['bin','certs','storage'])mkdirSync(join(root,d),{recursive:true});
 if(!existsSync(join(root,'settings.json')))json(join(root,'settings.json'),{task:tag,origin,email:manual?'medicina@manual.local.test':'doctor-m6@local.test',password:randomBytes(24).toString('hex'),refresh:randomBytes(32).toString('hex'),gateway:randomBytes(32).toString('hex'),billing:randomBytes(32).toString('hex'),image:randomBytes(32).toString('hex'),database:'fixture_m6_specialties',notifications:randomBytes(32).toString('base64')});
 const s=owned();
 const sp02b=process.argv.includes('--sp02b')||process.argv.includes('--sp03b')||process.argv.includes('--sp03c')||process.argv.includes('--sp03d')||process.argv.includes('--sp03f');
 const doctorEmail=manual?'medicina-dos@manual.local.test':'doctor-sp02b@local.test';
 const addAdmin=(manual||process.argv.includes('--sp01')||sp02b)&&!s.admin;
 const addDoctor=(manual||sp02b)&&!s.doctors?.some(doctor=>doctor.email===doctorEmail);
 if(addAdmin){
  s.admin={email:manual?'catalogo@manual.local.test':'admin-sp01@local.test',password:randomBytes(24).toString('hex'),refresh:randomBytes(32).toString('hex')};
 }
 if(addDoctor){
  s.doctors=[...(s.doctors??[]),{id:'33333333-3333-4333-8333-333333333333',name:manual?'Profesional sintético 2':'Profesional de prueba SP-02B',email:doctorEmail,password:randomBytes(24).toString('hex'),refresh:randomBytes(32).toString('hex')}];
 }
 if(addAdmin||addDoctor)json(join(root,'settings.json'),s);
 for(const name of ['gomod','gocache']){const volume=tag+'-'+name;const found=capture('docker',['volume','ls','-q','--filter',`name=^${volume}$`]);if(found){const [v]=JSON.parse(capture('docker',['volume','inspect',volume]));if(v.Labels?.['codex.task']!==tag)throw Error('Cache ownership mismatch');}else docker('volume','create','--label','codex.task='+tag,volume);}
 writeFileSync(join(root,'cert.sh'),'set -eu\ncd /src\ngo run ./ops/local/cert.go /lab/certs\n');
 if(!existsSync(join(root,'certs/cert.pem')))docker('run','--rm','--label','codex.task='+tag,'--mount',`type=bind,source=${backend},target=/src,readonly`,'--mount',`type=bind,source=${root},target=/lab`,'--mount',`type=volume,source=${tag}-gocache,target=/root/.cache/go-build`,'golang:1.27.4','sh','/lab/cert.sh');
 const labels={'codex.task':tag},certs=mount(join(root,'certs'))+':/certs:ro',bin=mount(join(root,'bin'))+':/app:ro';
 const postgres=major=>({image:'postgres:'+major,container_name:project+'-pg'+major,labels,environment:{POSTGRES_USER:'migration_fixture',POSTGRES_PASSWORD:s.database,POSTGRES_DB:major===18?'doctorchart_m6_e2e':'postgres'},ports:[`127.0.0.1:${major===17?55468:55470}:5432`],tmpfs:[major===17?'/var/lib/postgresql/data':'/var/lib/postgresql'],healthcheck:{test:['CMD','pg_isready','-U','migration_fixture','-d','postgres'],interval:'2s',timeout:'3s',retries:30}});
 const services={pg17:postgres(17),pg18:postgres(18),
 support:{image:'node:24.20.0',container_name:project+'-support',labels,command:['node','/support.mjs'],ports:['127.0.0.1:19447:9443'],volumes:[mount(join(backend,'ops/local/support.mjs'))+':/support.mjs:ro',mount(root)+':/local',certs]},
 backend:{image:'golang:1.27.4',container_name:project+'-backend',labels,command:['/app/backend'],volumes:[bin,mount(join(root,'storage'))+':/data',certs],environment:{PORT:'8083',DATABASE_URL:`postgres://doctorchart_app:${appPassword}@pg18:5432/doctorchart_m6_e2e?sslmode=disable`,MEDIA_DATABASE_URL:`postgres://doctorchart_media:${mediaPassword}@pg18:5432/doctorchart_m6_e2e?sslmode=disable`,GATEWAY_SHARED_SECRET:s.gateway,SERVICES_BILLING_SHARED_SECRET:s.billing,SERVICES_BILLING_URL:'http://support:9000',STORAGE_TYPE:'local',STUDIES_STORAGE_DIR:'/data/studies',MEDIA_PROVIDER:'local',MEDIA_ENVIRONMENT:'development',MEDIA_LOCAL_DIR:'/data/media',MEDIA_RECOVERY_LOCAL_DIR:'/data/recovery',IMAGE_CONVERTER_URL:'http://converter:8086',IMAGE_CONVERTER_API_KEY:s.image,STUDIES_ALLOW_IMAGE_TRANSFORM:'true',SCHEDULES_ENABLED:'true',BOOKINGS_ENABLED:'true',PUBLIC_BOOKINGS_ENABLED:'true',STAFF_ENABLED:'true',STAFF_ORIGIN:s.origin,NOTIFICATIONS_ENABLED:'true',NOTIFICATIONS_KEY:s.notifications,NOTIFICATIONS_PUBLIC_ORIGIN:s.origin,NOTIFICATIONS_SMTP_HOST:'mailpit',NOTIFICATIONS_SMTP_PORT:'1025',NOTIFICATIONS_SMTP_FROM:'turnos@doctorchart.local',NOTIFICATIONS_SMTP_TLS:'starttls',SSL_CERT_FILE:'/certs/cert.pem'},depends_on:{pg18:{condition:'service_healthy'},support:{condition:'service_started'},mailpit:{condition:'service_started'}}},
 gateway:{image:'golang:1.27.4',container_name:project+'-gateway',labels,command:['/app/gateway'],ports:['127.0.0.1:18087:8000'],volumes:[bin,certs],environment:{PORT:'8000',GATEWAY_SHARED_SECRET:s.gateway,POINTLY_SERVICE_URL:'http://support:9000',BILLING_SERVICE_URL:'http://support:9000',DOCTORCHART_SERVICE_URL:'http://backend:8083',SUPABASE_JWT_ISSUER:'https://local-support:9443/auth/v1',SUPABASE_JWKS_URL:'https://local-support:9443/auth/v1/.well-known/jwks.json',SUPABASE_JWT_AUDIENCE:'authenticated',CORS_ALLOWED_ORIGINS:s.origin,SSL_CERT_FILE:'/certs/cert.pem'},depends_on:['backend','support']},
 converter:{image:'bycode/image-email:local',container_name:project+'-converter',labels,environment:{LISTEN_ADDR:':8086',APPLICATIONS:'doctorchart',PROFILES:'doctorchart-study,avatar',APP_DOCTORCHART_API_KEY:s.image,APP_DOCTORCHART_PROFILES:'doctorchart-study,avatar',PROFILE_DOCTORCHART_STUDY_MODE:'transform',PROFILE_DOCTORCHART_STUDY_FORMAT:'webp',PROFILE_DOCTORCHART_STUDY_QUALITY:'75',PROFILE_DOCTORCHART_STUDY_MAX_WIDTH:'2048',PROFILE_DOCTORCHART_STUDY_MAX_HEIGHT:'2048',MAIL_ENABLED:'false'}},
 mailpit:{image:'axllent/mailpit:v1.27.10',container_name:project+'-mailpit',labels,ports:['127.0.0.1:18029:8025'],volumes:[certs],environment:{MP_SMTP_TLS_CERT:'/certs/cert.pem',MP_SMTP_TLS_KEY:'/certs/key.pem',MP_SMTP_REQUIRE_STARTTLS:'true'}}};
 Object.assign(services.converter.environment,{PROFILE_AVATAR_MAX_WIDTH:'512',PROFILE_AVATAR_MAX_HEIGHT:'512',PROFILE_AVATAR_FORMAT:'webp',PROFILE_AVATAR_FIT:'cover',PROFILE_AVATAR_QUALITY:'80'});
 services.support.networks={default:{aliases:['local-support']}};
 services.backend.environment.DOCTORCHART_SYNTHETIC_TEMPLATE_REVIEWER='22222222-2222-4222-8222-222222222222';
 const volumes={};
 if(manual){
  delete services.pg17;
  delete services.pg18.tmpfs;
  services.pg18.volumes=['pgdata:/var/lib/postgresql'];
  for(const name of ['pg18','support','gateway']) delete services[name].ports;
  services.mailpit.ports=['127.0.0.1:18030:8025'];
  services.mailpit.volumes.push('maildata:/data');
  services.mailpit.environment.MP_DATABASE='/data/mailpit.db';
  services.mailpit.networks=['default','edge'];
  volumes.pgdata={labels};
  volumes.maildata={labels};
  mkdirSync(join(root,'web','browser'),{recursive:true});
  services.web={image:services.support.image,container_name:project+'-web',labels,
   command:['node','/manual-web.mjs'],ports:['127.0.0.1:4354:4354'],depends_on:['gateway','support'],networks:['default','edge'],
   volumes:[certs,mount(join(root,'web/browser'))+':/site:ro',
    mount(join(backend,'ops/local/manual-web.mjs'))+':/manual-web.mjs:ro',
    mount(join(frontend,'scripts/preview-server.mjs'))+':/preview-server.mjs:ro',
    mount(join(frontend,'src/app/core/config/site.config.json'))+':/site-config.json:ro']};
 }
 json(join(root,'compose.json'),{services,volumes,networks:{default:{labels,...(manual?{internal:true}:{})},...(manual?{edge:{labels}}:{})}});
 const values={supabase:{url:s.origin,publishableKey:'sb_publishable_doctorchart_local_only'}};
 // Reuse the existing development adapter's public client identifier only in
 // the opt-in lab build. It is never used to pass production validation.
 const generated=join(frontend,manual?'src/environments/environment.manual.generated.ts':'src/environments/environment.m6.generated.ts');if(existsSync(generated)&&!readFileSync(generated,'utf8').startsWith('// Owned M6 lab only'))throw Error('Refusing to overwrite foreign generated config');
 writeFileSync(generated,"// Owned M6 lab only; never deploy.\nimport {baseEnvironment} from './environment.base';\nexport const environment={...baseEnvironment,production:false,appUrl:"+JSON.stringify(s.origin)+",apiUrl:"+JSON.stringify(s.origin+'/api/doctorchart')+",aiGatewayUrl:"+JSON.stringify(s.origin+'/api/ai/doctorchart/chat')+",bycodePlansUrl:"+JSON.stringify(s.origin+'/dashboard')+",supabase:"+JSON.stringify(values.supabase)+"};\n");
 writeFileSync(join(root,'build.sh'),`set -eu\ncd /src\ngo build -tags doctorchart_template_lab -mod=readonly -o /lab/bin/backend ./cmd/api\ncd /gateway\ngo build -mod=readonly -o /lab/bin/gateway .\n`);
 compose('up','-d','--wait',...(manual?[]:['pg17']),'pg18','support','converter','mailpit');
 owned();
 if(addAdmin||addDoctor)compose('restart','support');
 for(const major of manual?[18]:[17,18]){
  const sql=`DO $$ BEGIN
   IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='doctorchart_app') THEN CREATE ROLE doctorchart_app LOGIN PASSWORD '${appPassword}'; END IF;
   IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='doctorchart_media') THEN CREATE ROLE doctorchart_media LOGIN PASSWORD '${mediaPassword}'; END IF;
   IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='doctorchart_owner') THEN CREATE ROLE doctorchart_owner LOGIN PASSWORD '${ownerPassword}'; END IF;
  END $$;`;
  docker('exec',project+'-pg'+major,'psql','-X','-v','ON_ERROR_STOP=1','-U','migration_fixture','-d','postgres','-c',sql);
 }
 docker('exec',project+'-pg18','psql','-X','-v','ON_ERROR_STOP=1','-U','migration_fixture','-d','postgres','-c','ALTER DATABASE doctorchart_m6_e2e OWNER TO doctorchart_owner');
 if(manual)storeManualAccess(root,s);
 console.log('Owned M6 lab prepared; no existing database or frontend local config changed.');
}else if(command==='build'){
 owned();
 if(manual){
  compose('stop','web','gateway','backend');
  const env={...process.env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'};
  run('go',['build','-tags','doctorchart_template_lab','-mod=readonly','-o',join(root,'bin/backend'),'./cmd/api'],{cwd:backend,env});
  run('go',['build','-mod=readonly','-o',join(root,'bin/gateway'),'.'],{cwd:gateway,env});
  run(process.execPath,[join(frontend,'node_modules/@angular/cli/bin/ng.js'),'build','--configuration','manual-lab','--output-path',join(root,'web')],{cwd:frontend,env:{...process.env,NG_BUILD_MAX_WORKERS:'2'}});
 }else{
  docker('run','--rm','--label','codex.task='+tag,'--mount',`type=bind,source=${backend},target=/src,readonly`,'--mount',`type=bind,source=${gateway},target=/gateway,readonly`,'--mount',`type=bind,source=${join(repos,'bycode-image-email-service-go/pkg/media')},target=/bycode-image-email-service-go/pkg/media,readonly`,'--mount',`type=bind,source=${root},target=/lab`,'--mount',`type=volume,source=${tag}-gomod,target=/go/pkg/mod`,'--mount',`type=volume,source=${tag}-gocache,target=/root/.cache/go-build`,'-e','GOMAXPROCS=2','golang:1.27.4','sh','/lab/build.sh');
 }
}else if(manual&&command==='start'){
 const s=owned();
 compose('up','-d','--wait','pg18','support','converter','mailpit');
 const marker=capture('docker',['exec',project+'-pg18','psql','-XAt','-U','migration_fixture','-d','doctorchart_m6_e2e','-c',"SELECT to_regclass('public.app_schema_state')"]);
 if(!marker) initialize(s);
 compose('up','-d','web');
 await manualHealth(root);
 await seedManual(root,backend,s);
 console.log('DoctorChart manual: '+origin);
}else if(manual&&command==='stop'){owned();compose('stop');
}else if(manual&&command==='logs'){
 owned();compose('logs','--tail','100',...process.argv.slice(3).filter(arg=>arg!=='--manual'));
}else if(manual&&command==='restart'){
 owned();
 const service=process.argv[3];
 if(!['web','gateway','backend','pg18','support','converter','mailpit'].includes(service))throw Error('Indicá un servicio válido');
 compose('restart',service);await manualHealth(root);
}else if(manual&&command==='verify'){
 const s=owned();
 compose('exec','-T','pg18','psql','-X','-v','ON_ERROR_STOP=1','-U','migration_fixture','-d','doctorchart_m6_e2e','-c',
  "SELECT backend,baseline_version, current_setting('server_version') AS postgres FROM app_schema_state WHERE id=1");
 await manualHealth(root);await seedManual(root,backend,s,true);
}else if(manual&&command==='reset'){
 if(!process.argv.includes('--confirm-doctorchart-manual'))throw Error('Para restablecer, agregá --confirm-doctorchart-manual');
 const s=owned(), storage=join(root,'storage');
 const [volume]=JSON.parse(capture('docker',['volume','inspect',project+'_pgdata']));
 const seedState=JSON.parse(readFileSync(join(root,'seed-started.json')));
 if(seedState.task!==tag||volume.Labels?.['codex.task']!==tag||lstatSync(storage).isSymbolicLink()||realpathSync(storage)!==join(realpathSync(root),'storage'))throw Error('No coincide la identidad del destino sintético');
 compose('up','-d','--wait','pg18');
 owned();
 const identity=capture('docker',['exec',project+'-pg18','psql','-XAt','-U','migration_fixture','-d','doctorchart_m6_e2e','-c',"SELECT baseline_version FROM app_schema_state WHERE id=1"]);
 if(identity!=='20')throw Error('No coincide el baseline del laboratorio manual');
 compose('stop','web','gateway','backend');
 docker('exec',project+'-pg18','dropdb','-U','migration_fixture','--force','doctorchart_m6_e2e');
 docker('exec',project+'-pg18','createdb','-U','migration_fixture','-O','doctorchart_owner','doctorchart_m6_e2e');
 const suffix=Date.now();
 renameSync(storage,join(root,'storage-anterior-'+suffix));
 mkdirSync(storage);
 for(const name of ['seed-report.json','seed-started.json','Ficha-ejemplo.zip']){
  const current=join(root,name);
  if(existsSync(current))renameSync(current,join(root,suffix+'-anterior-'+name));
 }
 initialize(s);compose('up','-d','web');
 await manualHealth(root);await seedManual(root,backend,s);
}else if(manual&&!['status','credentials'].includes(command)){
 throw Error('Perfil manual: prepare | build | start | stop | status | verify | logs | restart | reset | credentials');
}else if(manual&&command==='credentials'){owned();console.log(join(root,'ACCESOS.txt'));
}else if(command==='up'){initialize(owned());compose('up','-d','backend','gateway');
}else if(command==='reset-e2e'){
 const s=owned();compose('stop','gateway','backend');
 docker('exec',project+'-pg18','dropdb','-U','migration_fixture','--force','doctorchart_m6_e2e');
 docker('exec',project+'-pg18','createdb','-U','migration_fixture','-O','doctorchart_owner','doctorchart_m6_e2e');
 initialize(s);
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
 const sourceFixture=[17,18,19].flatMap(version=>{
  const key='DOCTORCHART_BASELINE'+version+'_FIXTURE_SQL',source=process.env[key],target='/baseline'+version+'.sql';
  return source?['--mount',`type=bind,source=${resolve(source)},target=${target},readonly`,'-e',key+'='+target]:[];
 });
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
export DOCTORCHART_TEST_APP_PASSWORD='${appPassword}'
export DOCTORCHART_TEST_MEDIA_PASSWORD='${mediaPassword}'
export BASELINE_TEST_ADMIN_DSN='${dsn('postgres')}'
${Object.entries(databases).map(([key,name])=>`export ${key}='${dsn(name)}'`).join('\n')}
go test ${race?'-race ':''}-p 1 ./... -count=1
`);
 docker('run','--rm','--label','codex.task='+tag,'--network','container:'+container,'--mount',`type=bind,source=${backend},target=/src,readonly`,'--mount',`type=bind,source=${join(repos,'bycode-image-email-service-go/pkg/media')},target=/bycode-image-email-service-go/pkg/media,readonly`,'--mount',`type=bind,source=${root},target=/lab,readonly`,'--mount',`type=volume,source=${tag}-gomod,target=/go/pkg/mod`,'--mount',`type=volume,source=${tag}-gocache,target=/root/.cache/go-build`,...sourceFixture,'-e','GOMAXPROCS=2','golang:1.27.4','sh','/lab/gates-'+major+'.sh');
}else if(command==='restore-sp04e'){
 const state=owned();
 if(state.database!=='fixture_m6_specialties')throw Error('Unexpected disposable database identity');
 restoreSP04E({root,backend,project,tag,compose,run,capture,appPassword,mediaPassword,ownerPassword});
}else if(command==='status'){owned();compose('ps');
}else if(command==='down'){
 owned();compose('down');for(const suffix of ['gomod','gocache']){const name=tag+'-'+suffix;const [v]=JSON.parse(capture('docker',['volume','inspect',name]));if(v.Labels?.['codex.task']!==tag)throw Error('Cache ownership mismatch');docker('volume','rm',name);}
 console.log('Only owned M6 Docker resources removed; evidence retained in the temporary lab folder.');
}else if(command==='browser-info'){
 owned();const cert=new X509Certificate(readFileSync(join(root,'certs/cert.pem')));console.log(JSON.stringify({root,origin:'https://localhost:4353',spki:createHash('sha256').update(cert.publicKey.export({type:'spki',format:'der'})).digest('base64')}));
}else throw Error('Use prepare [--sp01|--sp02b|--sp03b|--sp03c|--sp03d|--sp03f], build, up, reset-e2e, go-gates 17|18 [race], restore-sp04e, status, browser-info or down');
