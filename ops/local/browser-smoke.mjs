import {readFileSync,mkdirSync} from 'node:fs';
import {join,resolve,dirname} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
const backend=resolve(dirname(fileURLToPath(import.meta.url)),'../..');
const {chromium}=await import(pathToFileURL(join(backend,'../doctorchart-frontend/node_modules/playwright/index.mjs')));
const root=join(process.env.USERPROFILE,'Documents','DoctorChartLocal');
const browser=await chromium.launch({channel:'chrome',headless:true});
try{
 for(const [name,port] of [['turnos',4302]]){
  const settings=JSON.parse(readFileSync(join(root,name,'settings.json')));
  for(const account of [{...settings,label:'doctor',role:'COMPANY'},...(settings.admin?[{...settings.admin,label:'admin',role:'SUPER_ADMIN'}]:[])]){
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  const page=await context.newPage();
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.goto(`https://localhost:${port}/login`);
  await page.locator('#login-email').fill(account.email);
  await page.locator('#login-password').fill(account.password);
  const loginResponse=page.waitForResponse(response=>response.url().includes('/auth/v1/token?grant_type=password'));
  await page.getByRole('button',{name:'Ingresar a DoctorChart'}).click();
  const session=await (await loginResponse).json();
  if(session.user?.app_metadata?.role!==account.role)throw Error(`${name}: rol de ${account.label} incorrecto`);
  await page.waitForURL(url=>!url.pathname.startsWith('/login'),{timeout:30000});
  await page.waitForLoadState('networkidle');
  mkdirSync(join(root,name,'evidence'),{recursive:true});
  if(account.label==='admin'){
   await page.waitForURL(`https://localhost:${port}/admin/specialties`);
   await page.getByRole('heading',{name:'Especialidades y wizard',exact:true}).waitFor();
   if(!await page.getByRole('button',{name:'Crear especialidad',exact:true}).isEnabled())throw Error(`${name}: panel administrativo deshabilitado`);
  }
  const checks=await page.evaluate(async ({access,refresh,auth})=>{
   const catalog=await fetch('/api/doctorchart/admin/specialties',{headers:{Authorization:`Bearer ${access}`}});
   const data=await catalog.json();
   const response=await fetch(`${auth}/auth/v1/token?grant_type=refresh_token`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({refresh_token:refresh})});
   const renewed=await response.json();
   const userResponse=await fetch(`${auth}/auth/v1/user`,{headers:{Authorization:`Bearer ${renewed.access_token}`}});
   const user=await userResponse.json();
   return {status:catalog.status,catalog:Array.isArray(data),role:renewed.user?.app_metadata?.role,userRole:user.app_metadata?.role,id:user.id};
  },{access:session.access_token,refresh:session.refresh_token,auth:`https://localhost:19444`});
  if(checks.status!==(account.label==='admin'?200:403))throw Error(`${name}: autorización ${account.label} incorrecta (${checks.status})`);
  if(account.label==='admin'&&!checks.catalog)throw Error(`${name}: catálogo administrativo inválido`);
  if(checks.role!==account.role||checks.userRole!==account.role||checks.id!==session.user.id)throw Error(`${name}: renovación mezcló las cuentas`);
  await page.screenshot({path:join(root,name,'evidence',account.label==='admin'?'admin-login.png':'first-login.png'),fullPage:true});
  if(errors.length)throw Error(`${name}: ${errors.join('; ')}`);
  console.log(`${name}/${account.label}: login Chrome, permisos de API y renovación OK; destino ${new URL(page.url()).pathname}`);
  await context.close();
  }
 }
}finally{await browser.close();}
