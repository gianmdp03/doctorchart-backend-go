import {existsSync,mkdirSync,readFileSync,writeFileSync,openSync,closeSync} from 'node:fs';
import {resolve,dirname,join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFileSync,spawn} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import https from 'node:https';

const script=dirname(fileURLToPath(import.meta.url));
const backend=resolve(script,'../..'), repos=dirname(backend), front=join(repos,'doctorchart-frontend');
const root=join(process.env.USERPROFILE,'Documents','DoctorChartLocal');
const command=process.argv[2]||'status';
// Keep the existing Compose project and state directory so its data and ports stay intact.
const variants=[{name:'turnos',port:4302,api:18084,db:15434,auth:19444,mail:18026,backend,front,gateway:join(repos,'api-ai-gateway-go')}];
const selected=process.argv[3]?variants.filter(v=>v.name===process.argv[3]):variants;
if(!selected.length)throw Error('El entorno local usa los repositorios principales y el puerto 4302.');
const run=(exe,args,cwd=backend,env={})=>execFileSync(exe,args,{cwd,env:{...process.env,...env},stdio:'inherit',windowsHide:true});
const capture=(exe,args,cwd=backend)=>execFileSync(exe,args,{cwd,encoding:'utf8',windowsHide:true}).trim();
const json=(path,value)=>writeFileSync(path,JSON.stringify(value,null,2)+'\n');
function background(args,cwd,log){
 const fd=openSync(log,'a');
 const child=spawn(process.execPath,args,{cwd,detached:true,windowsHide:true,stdio:['ignore',fd,fd]});child.unref();closeSync(fd);return child.pid;
}
function compose(v,...args){run('docker',['compose','-p',`doctorchart-local-${v.name}`,'-f',join(root,v.name,'compose.json'),...args]);}
function localPassword(role){
 for(const v of variants){
  const path=join(root,v.name,'settings.json');
  if(!existsSync(path))continue;
  const settings=JSON.parse(readFileSync(path));
  const password=role==='admin'?settings.admin?.password:settings.password;
  if(password)return password;
 }
 return `${role==='admin'?'Admin':'Doctor'}Local-`+randomBytes(9).toString('hex');
}
function ensureAdmin(v){
 const path=join(root,v.name,'settings.json');
 const settings=JSON.parse(readFileSync(path));
 if(!settings.admin){settings.admin={email:'admin@local.test',password:localPassword('admin'),refresh:randomBytes(32).toString('hex')};json(path,settings);}
 return settings;
}
function writeLaunchers(){
 const chrome=[join(process.env.ProgramFiles||'C:/Program Files','Google/Chrome/Application/chrome.exe'),join(process.env['ProgramFiles(x86)']||'C:/Program Files (x86)','Google/Chrome/Application/chrome.exe'),join(process.env.LOCALAPPDATA,'Google/Chrome/Application/chrome.exe')].find(existsSync);
 if(!chrome)throw Error('Google Chrome no está instalado en una ruta conocida.');
 const launcher=(action,openBrowser)=>`@echo off\r\n"${process.execPath}" "${fileURLToPath(import.meta.url)}" ${action}\r\nif errorlevel 1 (\r\n pause\r\n exit /b 1\r\n)\r\n${openBrowser?`start "" "${chrome}" --new-window "https://localhost:4302/"\r\n`:'pause\r\n'}`;
 writeFileSync(join(root,'Iniciar DoctorChart.cmd'),launcher('start',true));
 writeFileSync(join(root,'Detener DoctorChart.cmd'),launcher('stop',false));
 const configured=variants.filter(v=>existsSync(join(root,v.name,'settings.json')));
 const s=JSON.parse(readFileSync(join(root,configured[0].name,'settings.json')));
 writeFileSync(join(root,'ACCESOS.txt'),'ACCESOS DE DOCTORCHART LOCAL\n\nhttps://localhost:4302/'+`\n\nCUENTA MÉDICA\nUsuario: ${s.email}\nContraseña: ${s.password}\n${s.admin?`\nSUPER ADMIN — panel de especialidades\nUsuario: ${s.admin.email}\nContraseña: ${s.admin.password}\n`:''}`+'\nDoble clic en Iniciar DoctorChart.cmd para arrancar DoctorChart en Google Chrome.\nEn la cuenta médica, elegí tu especialidad en el primer ingreso.\nSuper admin entra directamente al panel de especialidades.\nCerrá sesión antes de cambiar de cuenta en la misma instancia.\nLas bases y los estudios se conservan al detener.\nCorreo local: http://localhost:18026\n');
}
async function wait(url,ca){
 for(let i=0;i<180;i++){
  try{await request(url,{ca});return;}catch{await new Promise(r=>setTimeout(r,1000));}
 }
 throw Error(`No responde ${url}; revisar logs en ${root}`);
}
function request(url,{ca,method='GET',body,token}={}){
 return new Promise((ok,fail)=>{
  const req=https.request(url,{ca,method,headers:{...(body?{'Content-Type':'application/json'}:{}),...(token?{Authorization:`Bearer ${token}`}:{})}},res=>{let data='';res.on('data',b=>data+=b);res.on('end',()=>res.statusCode>=200&&res.statusCode<300?ok(data?JSON.parse(data):null):fail(Error(`${url}: ${res.statusCode} ${data.slice(0,300)}`)));});req.setTimeout(5000,()=>req.destroy(Error('timeout')));req.on('error',fail);req.end(body?JSON.stringify(body):undefined);
 });
}
if(command==='prepare'){
 mkdirSync(root,{recursive:true});mkdirSync(join(root,'certs'),{recursive:true});
 if(!existsSync(join(root,'certs','cert.pem')))run('go',['run',join(script,'cert.go'),join(root,'certs')]);
 // Only this user's certificate store; no system-wide or production certificates.
 if(process.env.DOCTORCHART_SKIP_CERT_IMPORT!=='1')run('certutil',['-user','-addstore','Root',join(root,'certs','cert.pem')]);
 for(const v of selected){
  const dir=join(root,v.name);mkdirSync(join(dir,'bin'),{recursive:true});mkdirSync(join(dir,'storage'),{recursive:true});
  writeFileSync(join(dir,'dependencies-smoke.mjs'),readFileSync(join(script,'dependencies-smoke.mjs')));
  const settingsPath=join(dir,'settings.json');
  if(!existsSync(settingsPath))json(settingsPath,{email:'doctor@local.test',password:localPassword('doctor'),refresh:randomBytes(32).toString('hex'),gateway:randomBytes(32).toString('hex'),billing:randomBytes(32).toString('hex'),image:randomBytes(32).toString('hex'),database:randomBytes(24).toString('hex'),notifications:randomBytes(32).toString('base64'),origin:`https://localhost:${v.port}`});
  ensureAdmin(v);
  const s=JSON.parse(readFileSync(settingsPath));
  const localConfig=join(v.front,'config','local.public.json');mkdirSync(dirname(localConfig),{recursive:true});
  if(existsSync(localConfig)&&!existsSync(join(dir,'original-local.public.json')))writeFileSync(join(dir,'original-local.public.json'),readFileSync(localConfig));
  json(localConfig,{publicSiteUrl:s.origin,apiGatewayOrigin:s.origin,applicationUrls:{bycode:s.origin,pointly:s.origin,doctorchart:s.origin},supabase:{url:`https://localhost:${v.auth}`,publishableKey:'sb_publishable_doctorchart_local_only'}});
  run(process.execPath,['scripts/configure-local.mjs'],v.front);
  json(join(dir,'proxy.json'),{'/api':{target:`http://127.0.0.1:${v.api}`,secure:false,changeOrigin:true}});
  const volumes=[`${dir.replaceAll('\\','/')}/bin:/app:ro`,`${dir.replaceAll('\\','/')}/storage:/data`];
  const certs=`${join(root,'certs').replaceAll('\\','/')}:/certs:ro`;
  const services={
   postgres:{image:'postgres:17',environment:{POSTGRES_DB:'doctorchart',POSTGRES_USER:'doctorchart',POSTGRES_PASSWORD:s.database},ports:[`127.0.0.1:${v.db}:5432`],volumes:['postgres-data:/var/lib/postgresql/data'],healthcheck:{test:['CMD-SHELL','pg_isready -U doctorchart -d doctorchart'],interval:'2s',timeout:'3s',retries:30}},
   'local-support':{image:'node:24.20.0',command:['node','/support.mjs'],ports:[`127.0.0.1:${v.auth}:9443`],volumes:[`${script.replaceAll('\\','/')}/support.mjs:/support.mjs:ro`,`${dir.replaceAll('\\','/')}:/local`,certs]},
   backend:{image:'golang:1.27.3',command:['/app/backend'],volumes,depends_on:{postgres:{condition:'service_healthy'},'local-support':{condition:'service_started'}},environment:{PORT:'8083',DATABASE_URL:`postgresql://doctorchart:${s.database}@postgres:5432/doctorchart`,GATEWAY_SHARED_SECRET:s.gateway,SERVICES_BILLING_SHARED_SECRET:s.billing,SERVICES_BILLING_URL:'http://local-support:9000',STORAGE_TYPE:'local',STUDIES_STORAGE_DIR:'/data/studies',MEDIA_PROVIDER:'local',MEDIA_ENVIRONMENT:'development',MEDIA_LOCAL_DIR:'/data/media',MEDIA_RECOVERY_LOCAL_DIR:'/data/recovery',IMAGE_CONVERTER_URL:'http://converter:8086',IMAGE_CONVERTER_API_KEY:s.image,STUDIES_ALLOW_IMAGE_TRANSFORM:'true',...(v.name==='turnos'?{SCHEDULES_ENABLED:'true',BOOKINGS_ENABLED:'true',PUBLIC_BOOKINGS_ENABLED:'true',STAFF_ENABLED:'true',STAFF_ORIGIN:s.origin,NOTIFICATIONS_ENABLED:'true',NOTIFICATIONS_KEY:s.notifications,NOTIFICATIONS_PUBLIC_ORIGIN:s.origin,NOTIFICATIONS_SMTP_HOST:'mailpit',NOTIFICATIONS_SMTP_PORT:'1025',NOTIFICATIONS_SMTP_FROM:'turnos@doctorchart.local',NOTIFICATIONS_SMTP_TLS:'starttls',SSL_CERT_FILE:'/certs/cert.pem'}:{})}},
   gateway:{image:'golang:1.27.3',command:['/app/gateway'],ports:[`127.0.0.1:${v.api}:8000`],volumes:[volumes[0],certs],environment:{PORT:'8000',GATEWAY_SHARED_SECRET:s.gateway,POINTLY_SERVICE_URL:'http://local-support:9000',BILLING_SERVICE_URL:'http://local-support:9000',DOCTORCHART_SERVICE_URL:'http://backend:8083',SUPABASE_JWT_ISSUER:'https://local-support:9443/auth/v1',SUPABASE_JWKS_URL:'https://local-support:9443/auth/v1/.well-known/jwks.json',SUPABASE_JWT_AUDIENCE:'authenticated',CORS_ALLOWED_ORIGINS:s.origin,SSL_CERT_FILE:'/certs/cert.pem'},depends_on:['backend','local-support']},
   converter:{image:'bycode/image-email:local',environment:{LISTEN_ADDR:':8086',APPLICATIONS:'doctorchart',PROFILES:'doctorchart-study,avatar',APP_DOCTORCHART_API_KEY:s.image,APP_DOCTORCHART_PROFILES:'doctorchart-study,avatar',PROFILE_DOCTORCHART_STUDY_MODE:'transform',PROFILE_DOCTORCHART_STUDY_FORMAT:'webp',PROFILE_DOCTORCHART_STUDY_QUALITY:'75',PROFILE_DOCTORCHART_STUDY_MAX_WIDTH:'2048',PROFILE_DOCTORCHART_STUDY_MAX_HEIGHT:'2048',MAIL_ENABLED:'false'}}
  };
  Object.assign(services.converter.environment,{PROFILE_AVATAR_MAX_WIDTH:'512',PROFILE_AVATAR_MAX_HEIGHT:'512',PROFILE_AVATAR_FORMAT:'webp',PROFILE_AVATAR_FIT:'cover',PROFILE_AVATAR_QUALITY:'80'});
  if(v.name==='turnos'){
   services.backend.volumes.push(certs);
   services.mailpit={image:'axllent/mailpit:v1.27.10',ports:[`127.0.0.1:${v.mail}:8025`],volumes:[certs,'mail-data:/data'],environment:{MP_SMTP_TLS_CERT:'/certs/cert.pem',MP_SMTP_TLS_KEY:'/certs/key.pem',MP_SMTP_REQUIRE_STARTTLS:'true',MP_DATABASE:'/data/mailpit.db'}};
  }
  for(const service of Object.values(services))service.labels={'com.doctorchart.local.owner':'doctorchart-dual-local','com.doctorchart.local.instance':v.name};
  json(join(dir,'compose.json'),{services,volumes:{'postgres-data':{},...(v.name==='turnos'?{'mail-data':{}}:{})}});
  json(join(dir,'source.json'),{backend:v.backend,frontend:v.front,gateway:v.gateway,backendRevision:capture('git',['rev-parse','HEAD'],v.backend)});
 }
 writeLaunchers();
 console.log(`Preparado en ${root}. Ejecutar: node "${fileURLToPath(import.meta.url)}" start`);
}else if(command==='chrome'){
 writeLaunchers();console.log('Lanzadores preparados para Google Chrome.');
}else if(command==='accounts'){
 for(const v of selected){ensureAdmin(v);compose(v,'restart','local-support');}
 writeLaunchers();console.log('Cuenta super admin preparada en las instancias seleccionadas. Consultar ACCESOS.txt.');
}else if(command==='start'){
 for(const v of selected){
  const dir=join(root,v.name);if(!existsSync(join(dir,'compose.json')))throw Error('Ejecutar prepare primero');
  // Compile current files on every start; persistent data is never recreated.
  run('go',['build','-mod=readonly','-o',join(dir,'bin','backend'),'./cmd/api'],v.backend,{GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'});
  run('go',['build','-mod=readonly','-o',join(dir,'bin','gateway'),'.'],v.gateway,{GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'});
  compose(v,'up','-d');
  const ca=readFileSync(join(root,'certs','cert.pem'));
  await wait(`https://localhost:${v.auth}/health`,ca);
  let running=false;try{await request(`https://localhost:${v.port}/api/doctorchart/health`,{ca});running=true;}catch{}
  if(!running){
   const pid=background([join(front,'node_modules/@angular/cli/bin/ng.js'),'serve','--host','127.0.0.1','--port',String(v.port),'--ssl','--ssl-cert',join(root,'certs','cert.pem'),'--ssl-key',join(root,'certs','key.pem'),'--proxy-config',join(dir,'proxy.json')],v.front,join(dir,'frontend.log'));
   json(join(dir,'frontend-process.json'),{pid,createdAt:new Date().toISOString(),cwd:v.front});
  }
  await wait(`https://localhost:${v.port}/api/doctorchart/health`,ca);
  console.log(`${v.name}: https://localhost:${v.port} — listo`);
 }
}else if(command==='stop'){
 for(const v of selected){
  const pidFile=join(root,v.name,'frontend-process.json');
  if(existsSync(pidFile)){
   const {pid}=JSON.parse(readFileSync(pidFile));
   // Match executable and unique proxy path before terminating a recorded PID.
   const proxy=join(root,v.name,'proxy.json').replaceAll("'","''");
   run('powershell',['-NoProfile','-Command',`$p = Get-CimInstance Win32_Process -Filter 'ProcessId = ${Number(pid)}'; if ($p -and $p.Name -eq 'node.exe' -and $p.CommandLine.Contains('${proxy}')) { Stop-Process -Id $p.ProcessId }`]);
  }
  compose(v,'stop');
 }
 console.log('Detenido. Bases, archivos y correo conservados.');
}else if(command==='status'){
 for(const v of selected){if(existsSync(join(root,v.name,'compose.json')))compose(v,'ps');}
}else if(command==='credentials'){
 console.log(readFileSync(join(root,'ACCESOS.txt'),'utf8'));
}else if(command==='verify'){
 const ca=readFileSync(join(root,'certs','cert.pem'));
 for(const v of selected){
  const s=JSON.parse(readFileSync(join(root,v.name,'settings.json')));
  const session=await request(`https://localhost:${v.auth}/auth/v1/token?grant_type=password`,{ca,method:'POST',body:{email:s.email,password:s.password}});
  const token=session.access_token;
  const health=await request(`https://localhost:${v.port}/api/doctorchart/health`,{ca});
  const workspace=await request(`https://localhost:${v.port}/api/doctorchart/workspace`,{ca,token});
  console.log(`${v.name}: login, JWT, gateway, PostgreSQL, workspace y readiness OK (${health.status})`);
  if(workspace?.onboardingCompleted){await request(`https://localhost:${v.port}/api/doctorchart/clinics`,{ca,token});console.log('DoctorChart local: consultorios y permisos comerciales OK');}
  else if(!workspace?.onboardingCompleted)console.log(`${v.name}: falta elegir la especialidad en la primera entrada (onboarding normal)`);
  compose(v,'exec','-T','postgres','psql','-U','doctorchart','-d','doctorchart','-c','SELECT backend, baseline_version FROM app_schema_state;');
 }
}else throw Error('Comando: prepare | start | stop | status | credentials | verify');
