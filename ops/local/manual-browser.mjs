import assert from 'node:assert/strict';
import {createHash, X509Certificate, randomInt} from 'node:crypto';
import {readFileSync, writeFileSync} from 'node:fs';
import {join} from 'node:path';

const {chromium} = await import(new URL('../../../doctorchart-frontend/node_modules/playwright/index.mjs', import.meta.url));
const root = join(process.env.USERPROFILE, 'Documents', 'DoctorChartManual');
const read = name => JSON.parse(readFileSync(join(root, name)));
const state = read('settings.json'), seed = read('seed-report.json');
const origin = 'https://localhost:4354';
assert.equal(state.task, 'doctorchart-manual-sp05a');
assert.equal(seed.task, state.task);
const cert = new X509Certificate(readFileSync(join(root, 'certs/cert.pem')));
const pin = createHash('sha256').update(cert.publicKey.export({type: 'spki', format: 'der'})).digest('base64');
const browser = await chromium.launch({channel:'chrome', headless:true, args:['--ignore-certificate-errors-spki-list=' + pin]});
const checks = [];
const report = name => { checks.push(name); console.log('OK ' + name); };

async function withPage(exercise) {
  const context = await browser.newContext();
  try {
    // The smoke test proves the flows with access restricted to the local site.
    await context.route(url => /^https?:$/.test(url.protocol) && url.origin !== origin, route => route.abort());
    const page = await context.newPage();
    page.setDefaultTimeout(30000);
    try {
      await exercise(page);
    } catch (error) {
      console.error('Ruta del navegador:', new URL(page.url()).pathname);
      console.error((await page.getByRole('alert').allTextContents()).join(' '));
      throw error;
    }
  } finally {
    await context.close();
  }
}

async function login(page, account) {
  await page.goto(origin + '/login');
  await page.locator('#login-email').fill(account.email);
  await page.locator('#login-password').fill(account.password);
  await page.getByRole('button', {name:'Ingresar a DoctorChart', exact:true}).click();
  await page.waitForURL(url => url.pathname !== '/login');
}

try {
  await withPage(async page => {
    await login(page,state);
    await page.goto(origin + '/specialties');
    await page.getByRole('heading', {name:'Mis especialidades'}).waitFor();
    await page.getByRole('link', {name:'Personalizar ficha de Cardiología'}).click();
    await page.waitForURL(url => url.searchParams.has('specialtyContextId'));
    await page.goto(origin + '/records/' + seed.patient + '?specialtyContextId=' + seed.pediatrics);
    await page.getByRole('button', {name:'Documentos emitidos y exportación'}).click();
    await page.getByRole('heading', {name:'Exportar ficha contextual'}).waitFor();
    report('Médico 1, selector, personalización y ficha con documentos y exportación');
  });
  await withPage(async page => {
    await login(page,state.doctors[0]);
    await page.goto(origin + '/specialties');
    await page.getByRole('heading', {name:'Mis especialidades'}).waitFor();
    await page.getByRole('link', {name:'Personalizar ficha de Pediatría'}).waitFor();
    report('Médico 2, espacio propio');
  });
  await withPage(async page => {
    await login(page,state.admin);
    await page.getByRole('heading', {name:'Especialidades y wizard'}).waitFor();
    report('Administrador, catálogo de especialidades');
  });
  await withPage(async page => {
    const staff = read('staff-account.json');
    await page.goto(origin + '/staff/login');
    await page.getByLabel('Correo electrónico', {exact:true}).fill(staff.email);
    await page.getByLabel('Contraseña', {exact:true}).fill(staff.password);
    await page.getByRole('button', {name:'Ingresar', exact:true}).click();
    await page.getByRole('heading', {name:'Agenda de consultorios'}).waitFor();
    await page.getByLabel('Consultorio asignado').getByRole('option', {name:/Consultorio Centro Ficticio/}).waitFor({state:'attached'});
    assert.equal(await page.getByLabel('Consultorio asignado').getByRole('option', {name:/Norte/}).count(), 0);
    report('Secretaría, agenda delegada y consultorio Norte excluido');
  });
  await withPage(async page => {
    await page.goto(`${origin}/appointments/${seed.publicId}?specialtyContextId=${seed.pediatrics}`);
    const date = new Date(Date.now() + 2 * 86400000).toISOString().slice(0,10);
    await page.locator('#public-date').fill(date.split('-').reverse().join('/'));
    await page.locator('#public-date').press('Tab');
    await page.locator('[data-public-slot]').first().click();
    await page.locator('#public-contact').fill('Solicitante ficticio de navegador');
    await page.locator('#public-document').fill(String(randomInt(90002000,90009999)));
    await page.locator('#public-phone').fill('2235550199');
    await page.locator('#public-email').fill('visitante@manual.local.test');
    await page.locator('#public-different').check();
    await page.locator('#public-attendee').fill('Paciente distinto ficticio');
    await page.locator('#public-consent').check();
    const pending = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/bookings') &&
      response.request().method() === 'POST', {timeout:90000});
    pending.catch(() => {});
    await page.locator('#public-confirm').click();
    const response = await pending;
    assert.equal(response.status(),201);
    const result = await response.json();
    await page.getByRole('heading', {name:'Tu próxima consulta, organizada.'}).waitFor();
    assert.ok(result.reference, 'La referencia sigue disponible en la respuesta de la reserva');
    assert.equal(response.request().postDataJSON().attendeeName,'Paciente distinto ficticio');
    report('Reserva pública real con solicitante distinto del paciente');
  });
  writeFileSync(join(root,'browser-report.json'),JSON.stringify({result:'PASS',checks},null,2));
} finally {
  await browser.close();
}
