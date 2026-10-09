// Owned Chrome profile, synthetic identities, actual Angular and Gateway/Go HTTP.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { chromium } = require('C:/Users/gcast/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const local = path.resolve('.migration/integration');
const evidence = path.resolve('integration/evidence');
const cert = new crypto.X509Certificate(fs.readFileSync(path.join(local, 'ca.pem')));
const pin = crypto.createHash('sha256').update(cert.publicKey.export({ type: 'spki', format: 'der' })).digest('base64');
const browser = await chromium.launch({ channel: 'chrome', headless: true, args: ['--ignore-certificate-errors-spki-list=' + pin] });
const saved = path.join(local, 'browser-state.json');
const context = await browser.newContext({ viewport: { width: 1440, height: 1050 }, storageState: fs.existsSync(saved) ? saved : undefined });
const page = await context.newPage();
const resultFile = path.join(evidence, 'browser.json');
const cases = [];
const check = (name, ok, detail = '') => { cases.push({ name, status: ok ? 'PASS' : 'FAIL', detail }); console.log(ok ? 'PASS' : 'FAIL', name, detail); if (!ok) throw new Error(name); };
const response = (method, fragment, status) => page.waitForResponse(r => r.request().method() === method && r.url().includes(fragment) && r.status() === status, { timeout: 30000 });
const screenshot = async name => page.screenshot({ path: path.join(evidence, name + '.png'), fullPage: true });
page.on('dialog', dialog => dialog.accept());
try {
  await page.goto('http://localhost:4202');
  await page.waitForTimeout(1000);
  if (page.url().includes('/login')) {
    await page.locator('#login-email').fill('doc-browser@fixture.test');
    await page.locator('#login-password').fill('FixtureOnly123!');
    await page.getByRole('button', { name: 'Ingresar a DoctorChart' }).click();
    await page.waitForURL('**/onboarding/select-specialty');
  }
  if (page.url().includes('select-specialty')) {
    await page.getByRole('button', { name: /Pediatría/ }).click();
    await page.getByRole('button', { name: 'Continuar', exact: true }).click();
    await page.waitForURL('**/onboarding/setup');
  }
  if (page.url().includes('/onboarding/setup')) {
    for (let i = 0; i < 8; i++) {
      await page.locator('.wizard-footer .primary-button:not([disabled])').waitFor();
      const finish = page.getByRole('button', { name: 'Finalizar configuración', exact: true });
      if (await finish.count()) { await finish.click(); break; }
      await page.getByRole('button', { name: 'Siguiente', exact: true }).click();
      await page.waitForTimeout(300);
    }
    await page.waitForURL('**/dashboard');
    check('Angular onboarding persists through real Gateway and Go', true);
    await screenshot('browser-dashboard');
  }
  await context.storageState({ path: saved });
  const progressFile = path.join(local, 'browser-progress.json');
  const progress = fs.existsSync(progressFile) ? JSON.parse(fs.readFileSync(progressFile)) : {};
  if (!progress.patientId) {
    await page.getByRole('link', { name: 'Pacientes', exact: true }).click();
    await page.getByRole('button', { name: '+ Nuevo paciente', exact: true }).click();
    console.log('PATIENT_FIELDS', await page.locator('input, select').evaluateAll(xs => xs.map(x => ({ id: x.id, type: x.type }))));
    await page.locator('#field-sys_firstName').fill('Paciente Navegador');
    await page.locator('#field-sys_lastName').fill('Prueba');
    await page.locator('#field-sys_dni').fill('40222333');
    await page.locator('[data-field-key="sys_dob"] input').fill('29/02/2020');
    await page.locator('[data-field-key="sys_dob"] input').press('Tab');
    await page.locator('#field-sys_sex').selectOption('FEMALE');
    console.log('VALIDATION', await page.locator('.field-error').allTextContents());
    await screenshot('browser-patient-form');
    const [created] = await Promise.all([response('POST', '/patients', 201), page.getByRole('button', { name: 'Crear Paciente', exact: true }).click()]);
    progress.patientId = (await created.json()).id;
    fs.writeFileSync(progressFile, JSON.stringify(progress));
    await page.waitForURL('**/pacientes/' + progress.patientId);
    check('Patient creation with Argentine leap-day date', true, '29/02/2020');
  } else {
    await page.getByRole('link', { name: 'Pacientes', exact: true }).click();
    await page.getByRole('button', { name: 'Ver Ficha', exact: true }).first().click();
  }
  await page.getByRole('button', { name: 'Nueva historia clínica', exact: true }).waitFor();
  console.log('TABS', await page.locator('.flattened-tabs-nav button').allTextContents());
  if (!progress.encounterId) {
    await page.getByRole('button', { name: 'Nueva historia clínica', exact: true }).click();
    await page.locator('#field-chiefComplaint').fill('Control pediátrico de integración');
    console.log('ENCOUNTER_FIELDS', await page.locator('[role="dialog"] input, [role="dialog"] textarea').evaluateAll(xs => xs.map(x => ({ id: x.id, type: x.type }))));
    for (const [key, value] of [['weightKg', '18.5'], ['heightCm', '105'], ['headCircumferenceCm', '49']]) {
      const field = page.locator('#field-' + key); if (await field.count()) await field.fill(value);
    }
    const [created] = await Promise.all([response('POST', '/encounters', 201), page.getByRole('button', { name: 'Guardar historia clínica', exact: true }).click()]);
    progress.encounterId = (await created.json()).id;
    fs.writeFileSync(progressFile, JSON.stringify(progress));
    check('Clinical encounter created from Angular', true);
    await page.getByRole('button', { name: 'Guardar historia clínica', exact: true }).waitFor({ state: 'hidden' });
  }
  console.log('BUTTONS', await page.getByRole('button').allTextContents());
  await screenshot('browser-patient');
  const tab = name => page.locator('.flattened-tabs-nav').getByRole('button', { name });
  await tab(/Evolución y Curvas/).click();
  await page.locator('canvas').first().waitFor();
  check('Growth chart renders persisted anthropometry', (await page.getByRole('button', { name: /Peso para la Edad/ }).innerText()).includes('1'));
  await screenshot('browser-growth');
  await tab(/Vacunas/).click();
  if (!progress.vaccination) {
    await page.locator('.pending-card input[type=checkbox]').first().waitFor();
    await Promise.all([response('PUT', '/vaccinations/', 200), page.locator('.pending-card input[type=checkbox]').first().click()]);
    progress.vaccination = true; fs.writeFileSync(progressFile, JSON.stringify(progress));
    check('Vaccination recorded from Angular', true);
  }
  await page.locator('.applied-card').first().waitFor();
  await screenshot('browser-vaccination');
  await tab(/Recetas y Órdenes/).click();
  if (!progress.prescriptionId) {
    await page.locator('#rx-med-name').fill('Paracetamol de prueba');
    await page.locator('#rx-med-dosage').fill('5 mg');
    await page.locator('#rx-med-days').fill('5');
    await page.locator('#rx-med-notes').fill('Con agua. Fixture sintético.');
    await page.getByRole('button', { name: 'Agregar medicamento a la receta', exact: true }).click();
    const [created] = await Promise.all([response('POST', '/prescriptions', 201), page.getByRole('button', { name: 'Guardar en Historial', exact: true }).click()]);
    progress.prescriptionId = (await created.json()).id; fs.writeFileSync(progressFile, JSON.stringify(progress));
    check('Prescription saved through Angular', true);
  }
  const [pdfResponse, pdfPopup] = await Promise.all([response('GET', '/pdf', 200), context.waitForEvent('page'), page.getByRole('button', { name: 'Imprimir', exact: true }).first().click()]);
  await pdfPopup.waitForURL('blob:**');
  // Read the app-created preview blob: Chromium may discard a no-store HTTP
  // response from its debugging buffer once the PDF viewer navigates.
  const pdf = Buffer.from(await page.evaluate(async url => Array.from(new Uint8Array(await (await fetch(url)).arrayBuffer())), pdfPopup.url()));
  fs.writeFileSync(path.join(evidence, 'browser-prescription.pdf'), pdf);
  check('Authenticated clinical PDF forbids HTTP cache storage', pdfResponse.headers()['cache-control'].includes('no-store') && pdfResponse.headers()['cache-control'].includes('private'));
  check('Authenticated PDF opens in an isolated preview', pdf.subarray(0, 4).toString() === '%PDF' && await pdfPopup.evaluate(() => window.opener === null));
  await pdfPopup.close();
  await screenshot('browser-prescription');
  await tab(/Estudios realizados/).click();
  const original = fs.readFileSync(path.join(local, 'original-exif.jpg'));
  if (!progress.studyId) {
    await page.locator('#studyFileInput').setInputFiles(path.join(local, 'original-exif.jpg'));
    await page.locator('#studyDescription').fill('Original EXIF sintético; conservar todos los bytes');
    const [created] = await Promise.all([response('POST', '/studies', 201), page.getByRole('button', { name: 'Subir y Guardar Estudio', exact: true }).click()]);
    const study = await created.json(); progress.studyId = study.id; fs.writeFileSync(progressFile, JSON.stringify(progress));
    check('Angular uploads the entire original medical image', study.fileSize === original.length);
  }
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: 'Descargar', exact: true }).first().click()]);
  const downloaded = path.join(local, 'browser-download.jpg'); await download.saveAs(downloaded);
  check('Browser study download is byte-for-byte original', original.equals(fs.readFileSync(downloaded)));
  const [preview] = await Promise.all([context.waitForEvent('page'), page.getByRole('button', { name: 'Ver', exact: true }).first().click()]);
  await preview.locator('img').waitFor();
  await preview.waitForFunction(() => document.querySelector('img').naturalWidth > 0);
  const dimensions = await preview.locator('img').evaluate(img => [img.naturalWidth, img.naturalHeight]);
  check('Chrome applies original EXIF orientation', dimensions[0] === 1000 && dimensions[1] === 3000, JSON.stringify(dimensions));
  check('Study preview has no opener', await preview.evaluate(() => window.opener === null));
  await preview.screenshot({ path: path.join(evidence, 'browser-original-preview.png') }); await preview.close();
  await screenshot('browser-studies');
  await Promise.all([response('DELETE', '/studies/' + progress.studyId, 204), page.getByRole('button', { name: 'Eliminar estudio', exact: true }).click()]);
  check('Angular study deletion completes', true); delete progress.studyId; fs.writeFileSync(progressFile, JSON.stringify(progress));
  await page.getByRole('button', { name: 'Editar datos', exact: true }).first().click();
  await page.locator('#field-sys_phone').fill('+54 11 5555 0101');
  const [updatedResponse] = await Promise.all([response('PUT', '/patients/' + progress.patientId, 200), page.getByRole('button', { name: 'Guardar Cambios', exact: true }).click()]);
  const updated = await updatedResponse.json();
  check('Angular patient edit preserves date and persisted records', updated.birthDate === '2020-02-29' && updated.phone === '+54 11 5555 0101');
  await page.locator('.flattened-tabs-nav').waitFor();
  await tab(/Historia Clínica/).click();
  await page.getByText('Control pediátrico de integración', { exact: false }).first().waitFor();
  check('History survives patient update', true);
  await context.storageState({ path: saved });

  await page.waitForTimeout(1100);
  const refreshed = await page.evaluate(async () => {
    // Development debug API reaches the actual SDK; no fake HttpClient injection.
    const auth = window.ng.getComponent(document.querySelector('app-root')).authService;
    const before = auth.session()?.access_token;
    const response = await auth.supabaseClient.auth.refreshSession();
    return { ok: !response.error, rotated: !!before && before !== auth.session()?.access_token, sameOwner: response.data.user?.id === auth.currentUser()?.id };
  });
  check('Actual Supabase SDK refresh rotates bearer and retains owner', refreshed.ok && refreshed.rotated && refreshed.sameOwner);

  const authContext = await browser.newContext({ viewport: { width: 1440, height: 1050 } });
  const authPage = await authContext.newPage();
  authPage.on('response', r => { if (r.url().includes('/auth/v1/token')) console.log('AUTH RESPONSE', r.status()); });
  const login = async (email, password = 'FixtureOnly123!') => {
    await authPage.locator('#login-email').fill(email);
    await authPage.locator('#login-password').fill(password);
    await authPage.getByRole('button', { name: 'Ingresar a DoctorChart' }).click();
  };
  const logout = async () => {
    await authPage.locator('.profile-avatar-btn').click();
    await authPage.getByRole('button', { name: 'Cerrar Sesión', exact: true }).click();
    await authPage.locator('#login-email').waitFor();
  };
  await authPage.goto('http://localhost:4202');
  const invalid = authPage.waitForResponse(r => r.url().includes('/auth/v1/token') && r.status() === 400);
  await login('doc-browser@fixture.test', 'IncorrectFixturePassword'); await invalid;
  check('Real browser invalid login stays unauthenticated', await authPage.locator('#login-email').isVisible());
  await login('doc-browser@fixture.test'); await authPage.waitForURL('**/dashboard');
  await authPage.getByRole('link', { name: 'Pacientes', exact: true }).click();
  await authPage.getByRole('button', { name: 'Ver Ficha', exact: true }).first().click();
  await authPage.getByText('Paciente Navegador Prueba', { exact: true }).first().waitFor();
  check('Browser session opens its persisted medical record', true);
  await authPage.reload();
  await authPage.getByText('Paciente Navegador Prueba', { exact: true }).first().waitFor();
  check('Protected patient deep link survives browser reload', authPage.url().endsWith('/pacientes/' + progress.patientId));
  await logout();
  check('Signout removes medical record from rendered UI', !(await authPage.locator('body').innerText()).includes('Paciente Navegador Prueba'));
  await login('doc-b@fixture.test'); await authPage.locator('#login-email').waitFor({ state: 'hidden' });
  await authPage.getByRole('link', { name: 'Pacientes', exact: true }).click();
  await authPage.waitForTimeout(500);
  check('Account change cannot reuse previous patient directory', !(await authPage.locator('body').innerText()).includes('Paciente Navegador Prueba') && await authPage.getByRole('button', { name: 'Ver Ficha', exact: true }).count() === 0);
  await authPage.screenshot({ path: path.join(evidence, 'browser-owner-b.png'), fullPage: true });
  await authPage.goto('http://localhost:4202/pacientes/' + progress.patientId);
  await authPage.waitForTimeout(1000);
  check('Foreign medical record is never displayed', !(await authPage.locator('body').innerText()).includes('Paciente Navegador Prueba'));
  await logout();
  await login('doc-expired@fixture.test');
  await authPage.waitForTimeout(1500);
  console.log('EXPIRED URL', authPage.url(), 'BODY', (await authPage.locator('body').innerText()).slice(0, 700));
  await authPage.waitForURL('http://localhost:18999/fixture-plans');
  check('Expired entitlement redirects to configured plans URL', true, 'Plans UI is simulated; external Billing UI not exercised');
  await authContext.close();
} catch (error) {
  cases.push({ name: 'Browser execution', status: 'FAIL', detail: String(error).slice(0, 1500) });
  await screenshot('browser-failure');
  console.error(String(error)); process.exitCode = 1;
} finally {
  fs.writeFileSync(resultFile, JSON.stringify({ scope: 'Installed Chrome, isolated profile, certificate pinned to owned TLS fixture, actual Angular/Gateway/Go; synthetic data only', cases }, null, 2));
  await context.storageState({ path: saved });
  await browser.close();
}
