// Local-only authentication and commercial entitlements. Never deploy this adapter.
import http from 'node:http';
import https from 'node:https';
import {readFileSync,writeFileSync,existsSync} from 'node:fs';
import {generateKeyPairSync,createPrivateKey,createPublicKey,sign,createHmac,timingSafeEqual} from 'node:crypto';

const config=JSON.parse(readFileSync('/local/settings.json'));
const keyPath='/local/auth-key.pem';
if(!existsSync(keyPath)) writeFileSync(keyPath,generateKeyPairSync('rsa',{modulusLength:2048}).privateKey.export({type:'pkcs8',format:'pem'}),{mode:0o600});
const key=createPrivateKey(readFileSync(keyPath));
const jwk={...createPublicKey(key).export({format:'jwk'}),kid:'doctorchart-local',alg:'RS256',use:'sig'};
const user={id:'11111111-1111-4111-8111-111111111111',aud:'authenticated',role:'authenticated',email:config.email,created_at:'2026-01-01T00:00:00Z',updated_at:'2026-01-01T00:00:00Z',email_confirmed_at:'2026-01-01T00:00:00Z',identities:[],user_metadata:{full_name:'Dra. Desarrollo Local',license:'MN LOCAL',clinic_name:'Consultorio local'},app_metadata:{role:'COMPANY',app_modules:{doctorchart:{active:true,tier:'UNLIMITED',expires_at:'2099-01-01T00:00:00Z'}}}};
const accounts=[{user,password:config.password,refresh:config.refresh}];
for(const doctor of config.doctors||[])accounts.push({user:{...user,id:doctor.id,email:doctor.email,user_metadata:{full_name:doctor.name,license:'MN LOCAL',clinic_name:'Consultorio local'}},password:doctor.password,refresh:doctor.refresh});
if(config.admin)accounts.push({user:{...user,id:'22222222-2222-4222-8222-222222222222',email:config.admin.email,user_metadata:{full_name:'Administración Local'},app_metadata:{role:'SUPER_ADMIN',app_modules:{}}},password:config.admin.password,refresh:config.admin.refresh});
function session(account){
 const user=account.user;
 const now=Math.floor(Date.now()/1000), payload={iss:'https://local-support:9443/auth/v1',aud:'authenticated',sub:user.id,role:'authenticated',email:user.email,iat:now,exp:now+3600,app_metadata:user.app_metadata,user_metadata:user.user_metadata};
 const data=[{alg:'RS256',kid:jwk.kid,typ:'JWT'},payload].map(x=>Buffer.from(JSON.stringify(x)).toString('base64url')).join('.');
 return {access_token:data+'.'+sign('RSA-SHA256',Buffer.from(data),key).toString('base64url'),token_type:'bearer',expires_in:3600,expires_at:now+3600,refresh_token:account.refresh,user};
}
const equal=(a,b)=>typeof a==='string'&&typeof b==='string'&&Buffer.byteLength(a)===Buffer.byteLength(b)&&timingSafeEqual(Buffer.from(a),Buffer.from(b));
async function handler(req,res){
 res.setHeader('Access-Control-Allow-Origin',config.origin);res.setHeader('Access-Control-Allow-Headers','authorization,apikey,content-type,x-client-info,x-supabase-api-version');res.setHeader('Access-Control-Allow-Methods','GET,POST,OPTIONS');res.setHeader('Cache-Control','no-store');
 const send=(status,data)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data));};
 if(req.method==='OPTIONS')return send(200,{});
 const url=new URL(req.url,'http://local');
 if(url.pathname==='/health')return send(200,{local:true});
 if(url.pathname==='/auth/v1/.well-known/jwks.json')return send(200,{keys:[jwk]});
 if(url.pathname==='/auth/v1/token'&&req.method==='POST'){
  let raw='';for await(const part of req){raw+=part;if(raw.length>8192)return send(413,{});}
  let body;try{body=JSON.parse(raw);}catch{return send(400,{});}
  const account=accounts.find(account=>url.searchParams.get('grant_type')==='refresh_token'?equal(body.refresh_token,account.refresh):url.searchParams.get('grant_type')==='password'&&body.email===account.user.email&&equal(body.password,account.password));
  if(account)return send(200,session(account));
  return send(400,{error_code:'invalid_credentials',msg:'Credenciales locales inválidas'});
 }
 if(url.pathname==='/auth/v1/logout')return send(200,{});
 if(url.pathname==='/auth/v1/user'){
  const token=req.headers.authorization?.replace(/^Bearer /,'');
  try {const {verify}=await import('node:crypto');const [h,p,s]=token.split('.');const claims=JSON.parse(Buffer.from(p,'base64url'));const account=accounts.find(account=>account.user.id===claims.sub);if(account&&verify('RSA-SHA256',Buffer.from(h+'.'+p),createPublicKey(key),Buffer.from(s,'base64url'))&&claims.exp>Date.now()/1000&&claims.iss==='https://local-support:9443/auth/v1'&&claims.aud==='authenticated')return send(200,account.user);}catch{}
  return send(401,{});
 }
 if(url.pathname==='/api/billing/limits/DOCTORCHART')return send(200,{BASIC:{max_patients:50,max_records_per_patient:100},PRO:{max_patients:500,max_records_per_patient:1000},UNLIMITED:{max_patients:-1,max_records_per_patient:-1}});
 if(/^\/internal\/doctorchart\/owners\/[^/]+\/entitlements(?:-v[23])?$/.test(url.pathname)){
  if(!equal(req.headers['x-internal-secret'],config.billing))return send(401,{});
  const now=new Date(), snapshot={ownerId:decodeURIComponent(url.pathname.split('/')[4]),module:'DOCTORCHART',revision:'a'.repeat(64),issuedAt:now.toISOString(),validUntil:new Date(+now+55000).toISOString(),coverageExpiresAt:'2099-01-01T00:00:00Z',clinicManagement:true,max_clinics:20};
  if(/-v[23]$/.test(url.pathname))snapshot.onlineBooking=true;
  if(/-v3$/.test(url.pathname))snapshot.secretaryManagement=true;
  const body=JSON.stringify(snapshot);res.setHeader('X-Entitlements-Signature',createHmac('sha256',config.billing).update(body).digest('hex'));res.writeHead(200,{'Content-Type':'application/json'});return res.end(body);
 }
 return send(404,{message:'Servicio auxiliar local: operación no disponible'});
}
const safe=(req,res)=>handler(req,res).catch(()=>{if(!res.headersSent)res.writeHead(500);res.end();});
http.createServer(safe).listen(9000,'0.0.0.0');
https.createServer({cert:readFileSync('/certs/cert.pem'),key:readFileSync('/certs/key.pem')},safe).listen(9443,'0.0.0.0');
