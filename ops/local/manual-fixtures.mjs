import assert from 'node:assert/strict';
import {randomBytes, randomUUID, createHash} from 'node:crypto';
import {existsSync, readFileSync, readdirSync, writeFileSync} from 'node:fs';
import {request} from 'node:https';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';

const origin = 'https://localhost:4354';
const prefix = '/api/doctorchart';
const staffEmail = 'secretaria@manual.local.test';
const writeJSON = (path, data) => writeFileSync(path, JSON.stringify(data, null, 2), {mode: 0o600});
const hours = (start, end, context) => ({start, end, slotMinutes: 30, specialtyContextId: String(context)});

function client(root) {
  const ca = readFileSync(join(root, 'certs/cert.pem'));
  return async (method, path, {data, headers = {}, status = 200} = {}) => {
    const encoded = data === undefined || Buffer.isBuffer(data) ? data : Buffer.from(JSON.stringify(data));
    return new Promise((done, fail) => {
      const req = request(origin + path, {ca, method, headers: {
        Origin: origin,
        ...(encoded ? {'Content-Type': 'application/json', 'Content-Length': encoded.length} : {}),
        ...headers,
      }}, res => {
        const chunks = [];
        res.on('data', chunk => chunks.push(chunk));
        res.on('error', fail);
        res.on('end', () => {
          const bytes = Buffer.concat(chunks);
          let json = null;
          try {
            if (res.headers['content-type']?.includes('application/json') && bytes.length) json = JSON.parse(bytes);
          } catch (error) {
            fail(error);
            return;
          }
          if (res.statusCode !== status) {
            fail(Error(`${method} ${path}: HTTP ${res.statusCode}, esperado ${status} (${json?.code ?? 'sin código'}) ${json?.message ?? ''}`));
            return;
          }
          done({json, bytes, headers: res.headers});
        });
      });
      req.setTimeout(15000, () => req.destroy(Error('Tiempo de espera agotado en ' + path)));
      req.on('error', fail);
      req.end(encoded);
    });
  };
}

export async function manualHealth(root) {
  const call = client(root);
  for (let attempt = 0; attempt < 45; attempt++) {
    try {
      await call('GET', '/');
      await call('GET', prefix + '/health');
      return;
    } catch (error) {
      if (attempt === 44) throw error;
      await delay(1000);
    }
  }
}

export function storeManualAccess(root, settings) {
  const staffFile = join(root, 'staff-account.json');
  if (!existsSync(staffFile)) writeJSON(staffFile, {email: staffEmail, password: 'Local-' + randomBytes(12).toString('hex')});
  const staff = JSON.parse(readFileSync(staffFile));
  writeFileSync(join(root, 'ACCESOS.txt'), [
    'DoctorChart manual — cuentas ficticias',
    origin + '/login',
    `Médico 1 (19 especialidades): ${settings.email}  ${settings.password}`,
    `Médico 2 (2 especialidades): ${settings.doctors[0].email}  ${settings.doctors[0].password}`,
    `Administración del catálogo: ${settings.admin.email}  ${settings.admin.password}`,
    `Secretaría (${origin}/staff/login): ${staff.email}  ${staff.password}`,
    'Correo de ensayo: http://localhost:18030',
    'Reservas: abrir /appointments sin sesión. El solicitante y el paciente pueden ser personas distintas.',
  ].join('\r\n') + '\r\n', {mode: 0o600});
}

export async function seedManual(root, backend, settings, verifyOnly = false) {
  const reportFile = join(root, 'seed-report.json');
  if (!verifyOnly && existsSync(reportFile)) return;
  const call = client(root);
  async function login(account) {
    const {json} = await call('POST', '/auth/v1/token?grant_type=password', {
      data: {email: account.email, password: account.password},
    });
    return json.access_token;
  }
  const [doctorToken, secondToken, adminToken] = await Promise.all([
    login(settings), login(settings.doctors[0]), login(settings.admin),
  ]);
  const requester = token => async (method, path, data, context, status = 200, headers = {}) => {
    const url = prefix + path + (context ? (path.includes('?') ? '&' : '?') + 'specialtyContextId=' + context : '');
    return (await call(method, url, {data, status, headers: {Authorization: 'Bearer ' + token, ...headers}})).json;
  };
  const owner = requester(doctorToken), other = requester(secondToken), admin = requester(adminToken);
  const staffAccount = JSON.parse(readFileSync(join(root, 'staff-account.json')));
  const staffSession = data => call('POST', prefix + '/staff/login', {data});

  async function verify(report) {
    const {json: staff} = await staffSession(staffAccount);
    const [catalog, contexts, second, admins, publicProfile] = await Promise.all([
      owner('GET', '/specialties'), owner('GET', '/workspace/contexts'), other('GET', '/workspace/contexts'),
      admin('GET', '/admin/specialties'), call('GET', prefix + '/public/appointments/doctors/' + report.publicId),
    ]);
    await owner('GET', '/patients/' + report.patient, undefined, report.pediatrics);
    const assertFile = async (route, hash) => {
      const {bytes} = await call('GET', prefix + '/patients/' + report.patient + route + '?specialtyContextId=' + report.pediatrics, {
        headers: {Authorization: 'Bearer ' + doctorToken},
      });
      assert.equal(createHash('sha256').update(bytes).digest('hex'), hash);
    };
    await assertFile('/documents/' + report.document + '/pdf', report.documentHash);
    await assertFile('/studies/' + report.study + '/file', report.studyHash);
    assert.equal((await owner('GET', '/bookings/' + report.booking)).id, report.booking);
    await other('GET', '/patients/' + report.patient, undefined, report.pediatrics, 404);
    await admin('GET', '/patients/' + report.patient, undefined, report.pediatrics, 404);
    assert.ok(staff.csrfToken);
    assert.ok(admins && publicProfile.json);
    console.log(`Login y permisos OK. Catálogo: ${catalog.length}; contextos del médico 1: ${contexts.length}; médico 2: ${second.length}.`);
  }
  if (existsSync(reportFile)) {
    if (verifyOnly) await verify(JSON.parse(readFileSync(reportFile)));
    return;
  }
  if (verifyOnly) throw Error('Falta completar la preparación de datos ficticios');
  const started = join(root, 'seed-started.json');
  if (existsSync(started)) throw Error('La preparación anterior quedó incompleta. Revisá los logs y usá reset con confirmación explícita.');
  assert.equal((await owner('GET', '/workspace/contexts')).length, 0, 'La preparación requiere un espacio sintético vacío');
  writeJSON(started, {format: 1, task: settings.task});
  const initialCatalog = await admin('GET', '/admin/specialties');
  for (const file of readdirSync(join(backend, 'templates')).filter(file => file.endsWith('.provisional.json')).sort()) {
    const template = JSON.parse(readFileSync(join(backend, 'templates', file)));
    assert.equal(template.status, 'PROVISIONAL_TEST_ONLY');
    const created = initialCatalog.find(item => item.code === template.createSpecialty.code) ??
      await admin('POST', '/admin/specialties', template.createSpecialty, null, 201);
    const path = '/admin/specialties/' + created.id;
    let experience = await admin('GET', path + '/experience');
    for (const [key, suffix] of Object.entries({
      navigation: '/experience', patientRecordTabs: '/experience/patient-record-tabs', patientRecordActions: '/experience/patient-record-actions',
    })) {
      experience = await admin('PUT', path + suffix, {expectedVersion: experience.specialtyVersion, [key]: template.experience[key]});
    }
    const draft = await admin('GET', path + '/wizard/reviews');
    if (file === 'cardiology.provisional.json') {
      await admin('POST', path + '/wizard/publish', {expectedVersion: draft.revision}, null, 409);
    }
    await admin('POST', path + '/wizard/reviews', {expectedVersion: draft.revision}, null, 204);
    await admin('POST', path + '/wizard/publish', {expectedVersion: draft.revision});
  }
  const catalog = await owner('GET', '/specialties');
  assert.equal(catalog.length, 19);
  async function setup(api, item, first) {
    const pending = first
      ? await api('PUT', '/workspace/specialty', {specialtyId: item.id, expectedVersion: null})
      : await api('POST', '/workspace/contexts', {specialtyId: item.id}, null, 201);
    const context = pending.specialtyContextId;
    return api('POST', '/workspace/finish-onboarding', {specialtyContextId: context, expectedVersion: pending.version}, context);
  }
  async function patient(api, workspace, index, pediatric, history) {
    const values = {sys_firstName: pediatric ? 'Paciente Pediatría' : 'Paciente ' + workspace.specialty.name,
      sys_lastName: 'Ejemplo Ficticio', sys_dni: String(90000100 + index),
      sys_dob: pediatric ? '2020-01-10' : '1985-05-10', sys_sex: 'FEMALE'};
    const fields = workspace.definition.patientForm.categories.flatMap(c => c.enabled ? c.fields : []);
    if (history && fields.some(field => field.key === 'sys_conditionStatus')) {
      values.sys_conditionStatus = 'PRESENT';
      values.sys_conditions = 'Antecedente declarado de demostración';
    }
    return api('POST', '/patients', {workspaceVersion: workspace.version, values}, workspace.specialtyContextId, 201);
  }
  const contexts = {};
  const pediatric = catalog.find(item => item.code === 'pediatrics');
  for (const [index, item] of [pediatric, ...catalog.filter(item => item !== pediatric)].entries()) {
    const workspace = await setup(owner, item, index === 0);
    const example = await patient(owner, workspace, index, index === 0, true);
    const context = workspace.specialtyContextId;
    const templatesRes = await owner('GET', '/workspace/encounter-templates', undefined, context);
    const preferredTemplate = templatesRes.templates.find(t => t.sourceDefaultCode === 'pediatrics_early_childhood') ?? templatesRes.templates[0];
    const fields = new Set(workspace.definition.encounterForm.categories.flatMap(c => c.enabled ? c.fields.map(f => f.key) : []));
    const values = Object.fromEntries(Object.entries({
      chiefComplaint: 'Primera consulta ficticia en ' + item.name, diagnosis: 'Ejemplo sintético para probar la interfaz',
      weightKg: index === 0 ? 21 : 69, heightCm: index === 0 ? 116 : 168,
      followUpInstructions: 'Seguimiento registrado para el ensayo',
    }).filter(([field]) => fields.has(field)));
    const first = await owner('POST', '/encounters', {patientId: example.id, workspaceVersion: workspace.version,
      consultationType: 'FIRST_VISIT', ...(templatesRes.templates.length > 1 ? {templateId: preferredTemplate.id} : {}), values}, context, 201);
    contexts[item.code] = {context, workspace, patient: example.id, visit: first, values, templateId: preferredTemplate?.id};
  }
  for (const [index, code] of ['pediatrics', 'cardiology'].entries()) {
    const workspace = await setup(other, catalog.find(item => item.code === code), index === 0);
    await patient(other, workspace, 50 + index, index === 0, false);
  }
  console.log('Catálogo aprobado y fichas de las 19 especialidades preparadas.');
  const report = await seedClinicalAndAgenda({root, call, owner, doctorToken, contexts, staffAccount, patient});
  await verify(report);
  writeJSON(reportFile, report);
}

async function seedClinicalAndAgenda({root, call, owner, doctorToken, contexts, staffAccount, patient}) {
  const pediatric = contexts.pediatrics;
  const general = contexts.general_medicine;
  const dermatology = contexts.dermatology;
  const scope = pediatric.context;
  const path = '/patients/' + pediatric.patient;
  const extra = await patient(owner, pediatric.workspace, 99, true, false);
  const followup = await owner('POST', '/encounters', {
    patientId: pediatric.patient, workspaceVersion: pediatric.workspace.version, consultationType: 'FOLLOW_UP',
    templateId: pediatric.templateId,
    values: {...pediatric.values, chiefComplaint: 'Seguimiento ficticio', weightKg: 22, heightCm: 118},
  }, scope, 201);
  await owner('PUT', '/encounters/' + followup.id, {
    expectedVersion: followup.version, consultationType: 'FOLLOW_UP',
    values: {...followup.formValues, chiefComplaint: 'Seguimiento corregido de demostración'}, reason: 'Corrección de ejemplo',
  }, scope);
  const duplicate = await owner('POST', '/encounters', {
    patientId: pediatric.patient, workspaceVersion: pediatric.workspace.version, consultationType: 'FOLLOW_UP',
    templateId: pediatric.templateId,
    values: {...pediatric.values, chiefComplaint: 'Registro de ejemplo para anular'},
  }, scope, 201);
  await owner('DELETE', '/encounters/' + duplicate.id, {reason: 'Duplicado ficticio'}, scope, 204);
  const vaccines = await owner('GET', path + '/vaccinations', undefined, scope);
  const birthDose = vaccines.doses.find(item => item.dueFrom === 0);
  assert.ok(birthDose, 'Calendario pediátrico sin dosis al nacimiento');
  await owner('PUT', path + '/vaccinations/' + birthDose.id, {
    expectedVersion: vaccines.patientVersion, calendarId: vaccines.calendarId, applied: true, appliedAt: '2020-01-10',
  }, scope);
  const prescription = await owner('POST', path + '/prescriptions', {
    type: 'PRESCRIPTION', diagnosticOrders: [], prescriptions: [{medicationName: 'Producto ficticio de demostración',
      dosage: 'Texto de ejemplo', administrationRoute: 'Vía de prueba', frequency: 'Frecuencia de prueba'}],
  }, scope, 201);
  const order = await owner('POST', path + '/prescriptions', {
    type: 'STUDY_ORDERS', prescriptions: [], diagnosticOrders: ['Orden ficticia para demostrar el PDF'],
  }, scope, 201);
  const document = await owner('POST', path + '/prescriptions/' + order.id + '/documents', {}, scope, 201);
  const pdf = await call('GET', prefix + path + '/documents/' + document.id + '/pdf?specialtyContextId=' + scope, {
    headers: {Authorization: 'Bearer ' + doctorToken},
  });
  assert.equal(createHash('sha256').update(pdf.bytes).digest('hex'), document.sha256);
  const boundary = 'SP05A-' + randomUUID();
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
  const attachment = Buffer.concat([
    Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="name"\r\n\r\nEstudio ficticio de muestra\r\n--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="muestra.png"\r\nContent-Type: image/png\r\n\r\n`),
    png, Buffer.from(`\r\n--${boundary}--\r\n`),
  ]);
  const study = (await call('POST', prefix + path + '/studies?specialtyContextId=' + scope, {
    data: attachment, status: 201,
    headers: {Authorization: 'Bearer ' + doctorToken, 'Content-Type': 'multipart/form-data; boundary=' + boundary},
  })).json;
  const file = await call('GET', prefix + path + '/studies/' + study.id + '/file?specialtyContextId=' + scope, {
    headers: {Authorization: 'Bearer ' + doctorToken},
  });
  assert.ok(file.bytes.length);
  const archive = await call('POST', prefix + path + '/export?specialtyContextId=' + scope, {
    headers: {Authorization: 'Bearer ' + doctorToken},
  });
  assert.equal(archive.bytes.subarray(0, 2).toString(), 'PK');
  writeFileSync(join(root, 'Ficha-ejemplo.zip'), archive.bytes, {mode: 0o600});
  const summary = await owner('GET', path + '/clinical-summary', undefined, scope);
  assert.equal(summary.lastEncounter.id, followup.id);
  assert.ok((await owner('GET', path + '/revisions', undefined, scope)).items.length >= 2);
  assert.ok((await owner('GET', '/workspace/clinical-access?patientId=' + pediatric.patient, undefined, scope)).items.length);
  console.log('Documentos, adjunto convertido, vacunación, revisiones, auditoría y exportación preparados.');

  const timezone = 'America/Argentina/Buenos_Aires';
  const clinic = (name, specialtyIds) => owner('POST', '/clinics', {
    name, timezone, country: 'Argentina', province: 'Buenos Aires', city: 'Mar del Plata', address: 'Dirección ficticia para pruebas',
    specialtyContextIds: specialtyIds.map(String),
  }, null, 201);
  const centro = await clinic('Consultorio Centro Ficticio', Object.values(contexts).map(item => item.context));
  const norte = await clinic('Consultorio Norte Ficticio', [dermatology.context]);
  const virtualSetting = await owner('PUT', '/teleconsultations/' + general.context, {enabled: true});
  const virtual = await owner('GET', '/clinics/' + virtualSetting.clinicId);
  const rest = Object.values(contexts).filter(item => item !== pediatric).map(item => item.context);
  const weekly = ranges => Array.from({length: 7}, (_, index) => ({weekday: index + 1, ranges: ranges(index)}));
  async function schedule(clinicId, ranges) {
    const path = '/clinics/' + clinicId + '/schedule';
    const before = await owner('GET', path);
    await owner('PUT', path, {expectedVersion: before.version, minNoticeMinutes: 0,
      maxAdvanceDays: 30, weekly: weekly(ranges), exceptions: []});
  }
  await schedule(centro.id, index => [
    hours('09:00', '11:00', scope),
    ...rest.slice(index * 3, index * 3 + 3).map((context, n) => hours(`${11 + n}:00`, `${12 + n}:00`, context)),
  ]);
  await schedule(norte.id, () => [hours('14:00', '15:00', dermatology.context)]);
  await schedule(virtual.id, () => [hours('16:00', '18:00', general.context)]);
  const date = new Date(Date.now() + 86400000).toISOString().slice(0, 10);
  async function slot(clinicId, context) {
    const availability = await owner('GET', '/clinics/' + clinicId + '/availability?from=' + date + '&days=1');
    const slot = availability.days.flatMap(day => day.slots).find(slot => slot.specialtyContextId === String(context));
    assert.ok(slot, 'Faltan horarios de ejemplo');
    return slot.start;
  }
  async function book(clinic, context, patientId) {
    return owner('POST', '/bookings', {clinicId: clinic.id, specialtyContextId: String(context),
      start: await slot(clinic.id, context), contactName: 'Solicitante Ficticio', attendeeName: 'Paciente Ejemplo',
      contactPhone: '2235550100', ...(patientId ? {patientId: String(patientId)} : {})}, null, 201,
    {'Idempotency-Key': randomUUID()});
  }
  const confirmed = await book(centro, scope, pediatric.patient);
  const remote = await book(virtual, general.context);
  const arrived = await book(centro, scope);
  await owner('PATCH', '/bookings/' + arrived.id + '/status', {expectedVersion: arrived.version, status: 'CHECKED_IN'},
    null, 200, {'Idempotency-Key': randomUUID()});
  const publication = await owner('GET', '/booking-publication');
  const published = await owner('PUT', '/booking-publication', {
    expectedVersion: publication.version, name: 'Profesional Ficticio DoctorChart', description: 'Perfil de laboratorio con datos ficticios',
    published: true, clinics: [
      {clinicId: centro.id, enabled: true, address: 'Dirección pública ficticia'},
      {clinicId: virtual.id, enabled: true, address: ''},
    ], questions: [], specialtyContextIds: [scope, general.context].map(String),
  });
  const invited = await owner('POST', '/secretaries/invitations', {
    email: staffEmail, name: 'Secretaría de Prueba', clinicIds: [centro.id], permissions: ['view', 'create', 'reschedule', 'cancel', 'status'],
  }, null, 201);
  const staff = await call('POST', prefix + '/staff/activate', {
    data: {...staffAccount, invitationCode: invited.invitationCode},
  });
  const cookie = staff.headers['set-cookie'][0].split(';')[0];
  const assignments = (await call('GET', prefix + '/staff/assignments', {headers: {Cookie: cookie}})).json;
  assert.equal(assignments.items.length, 1);
  assert.equal(assignments.items[0].clinicId, centro.id);
  const staffPrefix = prefix + '/staff/owners/' + assignments.items[0].ownerId;
  await call('GET', staffPrefix + '/clinics/' + centro.id + '/availability?from=' + date + '&days=1', {headers: {Cookie: cookie}});
  await call('GET', staffPrefix + '/clinics/' + norte.id + '/availability?from=' + date + '&days=1', {headers: {Cookie: cookie}, status: 404});
  await call('GET', prefix + path + '?specialtyContextId=' + scope, {headers: {Cookie: cookie}, status: 401});
  return {task: 'doctorchart-manual-sp05a', baseline: 20, specialties: Object.keys(contexts),
    pediatrics: scope, general: general.context, patient: pediatric.patient, extraPatient: extra.id,
    prescription: prescription.id, document: document.id, documentHash: document.sha256, study: study.id,
    studyHash: createHash('sha256').update(file.bytes).digest('hex'), publicId: published.publicId, date,
    booking: confirmed.id, remoteBooking: remote.id, clinic: centro.id};
}
