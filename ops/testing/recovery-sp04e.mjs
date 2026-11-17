// Owns only the SP-04E restoration inside the disposable M6 laboratory.
import assert from 'node:assert/strict';
import { closeSync, cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, openSync, readFileSync, readSync, readdirSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join } from 'node:path';
import { performance } from 'node:perf_hooks';

function hash(file) {
  const digest = createHash('sha256');
  const fd = openSync(file, 'r');
  const buffer = Buffer.alloc(64 * 1024);
  try {
    for (;;) {
      const n = readSync(fd, buffer);
      if (n === 0) return digest.digest('hex');
      digest.update(buffer.subarray(0, n));
    }
  } finally {
    closeSync(fd);
  }
}

function files(directory, prefix = '') {
  const entries = {};
  for (const name of readdirSync(directory).sort()) {
    const absolute = join(directory, name);
    const relative = prefix + name;
    const info = lstatSync(absolute);
    if (info.isDirectory()) Object.assign(entries, files(absolute, relative + '/'));
    else if (info.isFile()) entries[relative] = { bytes: info.size, sha256: hash(absolute) };
    else throw Error('Unexpected nonregular entry in owned laboratory storage');
  }
  return entries;
}

function checkedBackup(directory, manifest) {
  assert.equal(manifest.format, 1);
  assert.equal(manifest.baseline, 20);
  assert.equal(manifest.postgres, 18);
  assert.equal(hash(join(directory, 'doctorchart.dump')), manifest.dumpSHA256);
  assert.equal(hash(join(directory, 'baseline.sql')), manifest.baselineSHA256);
  assert.equal(hash(join(directory, 'restore-definitions.sql')), manifest.definitionsSHA256);
  assert.deepEqual(files(join(directory, 'storage')), manifest.files);
}

const inventoryQuery = `
SELECT (jsonb_build_object(
 'baseline',(SELECT baseline_version FROM app_schema_state),
 'patients',(SELECT count(*) FROM patients),
 'encounters',(SELECT count(*) FROM clinical_encounters),
 'workspaces',(SELECT count(*) FROM doctor_workspaces),
 'contexts',(SELECT count(*) FROM doctor_specialty_contexts),
 'bookings',(SELECT count(*) FROM bookings),
 'studies',(SELECT count(*) FROM patient_medical_studies),
 'media',(SELECT count(*) FROM media_assets),
 'revisions',(SELECT count(*) FROM clinical_revisions),
 'audit',(SELECT count(*) FROM clinical_access_events),
 'approvals',(SELECT count(*) FROM wizard_approvals),
 'revocations',(SELECT count(*) FROM wizard_approval_revocations),
 'documents',(SELECT count(*) FROM clinical_document_issues),
 'annulments',(SELECT count(*) FROM clinical_document_annulments),
 'exports',(SELECT count(*) FROM clinical_export_evidence),
 'pdfIntegrity',(SELECT bool_and(sha256=encode(sha256(pdf),'hex')) FROM clinical_document_issues),
 'invalidReferences',(SELECT count(*) FROM pg_constraint WHERE contype='f' AND NOT convalidated)
) || jsonb_build_object('contentSHA256',jsonb_build_object(
 'patients',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM patients r),
 'encounters',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM clinical_encounters r),
 'revisions',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM clinical_revisions r),
 'audit',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM clinical_access_events r),
 'approvals',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM wizard_approvals r),
 'contexts',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM doctor_specialty_contexts r),
 'documents',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r)-'pdf' ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM clinical_document_issues r),
 'exports',(SELECT encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text,'UTF8')),'hex') FROM clinical_export_evidence r)
))
)::text`;

function originalExpressions(schema) {
  const checks = {
    booking_events: ['booking_events_kind_check'],
    bookings: ['bookings_status_check'],
    booking_public_details: ['booking_public_details_document_type_check'],
    clinics: ['clinics_status_check'],
    notification_outbox: ['notification_outbox_kind_check', 'notification_outbox_state_check'],
    schedule_blocks: ['schedule_blocks_check'],
  };
  const statements = [];
  for (const [table, constraints] of Object.entries(checks)) {
    for (const name of constraints) {
      const line = schema.split('\n').find(line => line.trimStart().startsWith('CONSTRAINT ' + name + ' CHECK '));
      if (!line) throw Error('Missing baseline restoration definition');
      statements.push('ALTER TABLE public.' + table + ' DROP CONSTRAINT ' + name + ';');
      statements.push('ALTER TABLE public.' + table + ' ADD ' + line.trim().replace(/,$/, '') + ';');
    }
  }
  const index = schema.split('\n').find(line => line.startsWith('CREATE INDEX notification_outbox_due_idx '));
  if (!index) throw Error('Missing baseline notification index');
  statements.push('DROP INDEX public.notification_outbox_due_idx;', index);
  return 'SET LOCAL ROLE doctorchart_owner; SET LOCAL search_path=public,pg_catalog;\n' + statements.join('\n') + '\n';
}

export function restoreSP04E({ root, backend, project, tag, compose, run, capture, appPassword, mediaPassword, ownerPassword }) {
  const start = performance.now();
  const source = project + '-pg18';
  const target = project + '-pg18-restore';
  const file = join(root, 'compose.json');
  const config = JSON.parse(readFileSync(file, 'utf8'));
  if (config.services['pg18-restore'] || !config.services.backend.environment.DATABASE_URL.includes('@pg18:5432/doctorchart_m6_e2e?')) {
    throw Error('Restore requires the original, isolated SP-04E laboratory topology');
  }
  if (!existsSync(join(root, 'storage'))) throw Error('Missing owned laboratory storage');
  function psql(container, sql, database = 'doctorchart_m6_e2e') {
    return capture('docker', ['exec', container, 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1',
      '-U', 'migration_fixture', '-d', database, '-Atc', sql]);
  }
  const schema = readFileSync(join(backend, 'db/schema.sql'), 'utf8').replaceAll('\r\n', '\n');
  const baselineSHA256 = createHash('sha256').update(schema).digest('hex');
  assert.equal(psql(source, 'SELECT sql_sha256 FROM app_schema_state'), baselineSHA256);
  const catalogQuery = readFileSync(join(backend, 'db/catalog.go'), 'utf8').split('`')[1].replaceAll('$1', 'ARRAY[]::text[]');
  const liveCatalog = container => createHash('sha256').update(psql(container, 'SET search_path=public,pg_catalog; ' + catalogQuery)).digest('hex');
  const expectedCatalog = psql(source, 'SELECT catalog_sha256 FROM app_schema_state');
  assert.equal(liveCatalog(source), expectedCatalog);
  function tableInventory(container) {
    const tables = JSON.parse(psql(container, `SELECT json_agg(tablename ORDER BY tablename)
      FROM pg_tables WHERE schemaname='public'`));
    return Object.fromEntries(tables.map(table => {
      if (!/^[a-z_]+$/.test(table)) throw Error('Unexpected laboratory table name');
      return [table, JSON.parse(psql(container, `SELECT json_build_object('rows',count(*),
        'sha256',encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text,'[]'),'UTF8')),'hex'))
        FROM public.${table} r`))];
    }));
  }
  const directory = mkdtempSync(join(root, 'recovery-sp04e-'));
  const backupStorage = join(directory, 'storage');
  const dump = join(directory, 'doctorchart.dump');
  compose('stop', 'gateway', 'backend');
  const before = JSON.parse(psql(source, inventoryQuery));
  const beforeTables = tableInventory(source);
  assert.equal(before.baseline, 20);
  assert.ok(before.patients && before.studies && before.documents && before.revisions && before.audit && before.approvals);
  const fd = openSync(dump, 'wx', 0o600);
  try {
    run('docker', ['exec', source, 'pg_dump', '-U', 'migration_fixture', '-Fc', 'doctorchart_m6_e2e'],
      { stdio: ['ignore', fd, 'inherit'] });
  } finally {
    closeSync(fd);
  }
  cpSync(join(root, 'storage'), backupStorage, { recursive: true, force: false, errorOnExist: true });
  writeFileSync(join(directory, 'baseline.sql'), schema, { mode: 0o600 });
  const definitions = originalExpressions(schema);
  writeFileSync(join(directory, 'restore-definitions.sql'), definitions, { mode: 0o600 });
  const manifest = { format: 1, baseline: 20, postgres: 18, dumpSHA256: hash(dump), baselineSHA256,
    definitionsSHA256: hash(join(directory, 'restore-definitions.sql')), catalogSHA256: expectedCatalog,
    tables: beforeTables, files: files(backupStorage) };
  writeFileSync(join(directory, 'manifest.json'), JSON.stringify(manifest, null, 2), { mode: 0o600 });
  checkedBackup(directory, manifest);
  assert.throws(() => checkedBackup(directory, { ...manifest, baseline: 19 }));
  assert.throws(() => checkedBackup(directory, { ...manifest, dumpSHA256: '0'.repeat(64) }));
  const corrupt = join(directory, 'corrupt-fixture');
  mkdirSync(corrupt);
  writeFileSync(join(corrupt, 'doctorchart.dump'), Buffer.from('synthetic-corrupt-backup'));
  assert.throws(() => checkedBackup(corrupt, manifest));
  const incomplete = join(directory, 'incomplete-fixture');
  mkdirSync(incomplete);
  assert.throws(() => checkedBackup(incomplete, manifest));
  assert.deepEqual(JSON.parse(psql(source, inventoryQuery)), before);

  config.services['pg18-restore'] = {
    ...config.services.pg18, container_name: target, ports: [],
    environment: { ...config.services.pg18.environment, POSTGRES_DB: 'postgres' },
  };
  writeFileSync(file, JSON.stringify(config, null, 2), { mode: 0o600 });
  compose('up', '-d', '--wait', 'pg18-restore');
  const targetTag = capture('docker', ['inspect', '--format', '{{index .Config.Labels "codex.task"}}', target]);
  if (targetTag !== tag) throw Error('Restoration target ownership mismatch');
  const startedRestore = performance.now();
  psql(target, `CREATE ROLE doctorchart_app LOGIN PASSWORD '${appPassword}';
    CREATE ROLE doctorchart_media LOGIN PASSWORD '${mediaPassword}';
    CREATE ROLE doctorchart_owner LOGIN PASSWORD '${ownerPassword}';`, 'postgres');
  run('docker', ['exec', target, 'createdb', '-U', 'migration_fixture', '-O', 'doctorchart_owner', 'doctorchart_m6_e2e']);
  const input = openSync(dump, 'r');
  try {
    run('docker', ['exec', '-i', target, 'pg_restore', '-U', 'migration_fixture', '--single-transaction',
      '--exit-on-error', '-d', 'doctorchart_m6_e2e'], { stdio: [input, 'inherit', 'inherit'] });
  } finally {
    closeSync(input);
  }
  // pg_dump's expression deparser changes selected parse trees on re-entry.
  // Recreate those definitions from the verified baseline artifact before
  // checking the complete original catalog, with every CHECK validated again.
  psql(target, 'BEGIN;\n' + definitions + '\nCOMMIT;');
  assert.equal(liveCatalog(target), expectedCatalog);
  const restored = JSON.parse(psql(target, inventoryQuery));
  assert.deepEqual(restored, before);
  assert.deepEqual(tableInventory(target), beforeTables);
  assert.equal(restored.pdfIntegrity, true);
  assert.equal(restored.invalidReferences, 0);
  assert.equal(psql(target, `BEGIN; SET LOCAL ROLE doctorchart_app;
    SELECT (SELECT count(*) FROM public.patients)=0
      AND (SELECT count(*) FROM public.clinical_document_issues)=0
      AND NOT has_table_privilege('public.clinical_document_issues','UPDATE,DELETE')
      AND NOT has_table_privilege('doctorchart_media','public.clinical_document_issues','SELECT');
    ROLLBACK;`), 't');

  const restoredStorage = join(directory, 'restored-storage');
  cpSync(backupStorage, restoredStorage, { recursive: true, force: false, errorOnExist: true });
  assert.deepEqual(files(restoredStorage), manifest.files);
  for (const key of ['DATABASE_URL', 'MEDIA_DATABASE_URL']) {
    config.services.backend.environment[key] = config.services.backend.environment[key].replace('@pg18:', '@pg18-restore:');
  }
  config.services.backend.volumes = config.services.backend.volumes.map(volume => (
    volume.endsWith(':/data') ? restoredStorage.replaceAll('\\', '/') + ':/data' : volume
  ));
  config.services.backend.depends_on['pg18-restore'] = config.services.backend.depends_on.pg18;
  delete config.services.backend.depends_on.pg18;
  writeFileSync(file, JSON.stringify(config, null, 2), { mode: 0o600 });
  compose('up', '-d', 'backend', 'gateway');
  const evidence = { result: 'PASS', before, restored, files: Object.keys(manifest.files).length,
    allTableRowsAndHashesVerified: Object.keys(beforeTables).length,
    artifactBytes: lstatSync(dump).size,
    restoreMs: Math.round(performance.now() - startedRestore),
    elapsedMs: Math.round(performance.now() - start),
    missingOrIncompatibleOrCorruptBackupRejected: true, originalCatalogVerified: true };
  writeFileSync(join(root, 'browser', 'sp04e-recovery-inventory.json'), JSON.stringify(evidence, null, 2), { mode: 0o600 });
  console.log(JSON.stringify(evidence));
}
