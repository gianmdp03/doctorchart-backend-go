# M8: esquema10 en desarrolloaislado

Migración0010_notifications.sql;44tablas/45FK. Baseline/0002..0009inmutables. Outboxdurable, accesoaconstancias y recuperaciónemailstaff; sin pacientes/clínica modificados. Fresh/upgrade9→10 y fallosSQL/drift/seed/rollback/reintento probados enPG17/18descartables. No ejecutar en basesreales. Binarioprevio rechaza10; recuperación con binariocompatible o respaldo autorizado, nunca bajar marcador.

M8usa fixtures BASELINE_TEST_ADMIN_DSN y DOCTORCHART_CLINICS_TEST_DATABASE_URL ya validados porownership/nombre/loopback. NOTIFICATIONS_ENABLED=false; activar requiere claveestable32bytesbase64 y SMTPconTLS/OriginHTTPS exacto. Parámetros y límites en docs/ai/M8_DELIVERY.md.

Inspecciónoperativa read-only: SELECT id,kind,state,attempts,last_code,due_at,lease_until FROM notification_outbox ORDER BY id; nunca imprimir payloadcifrado, correosdestinatarios, tokens o credenciales. DEAD no reenvíaautomáticamente: revisarcausa y autorización antes de reponer PENDING; conservarjob/historial, sin borrar ni cambiarpayload.

---

Antecedentes anteriores a M8:

# Esquema de desarrollo M7: versión9

Migración0009_secretaries.sql,41 tablas/40 FK. Baseline y0002..0008 inmutables. Cuenta local, relaciones/sedes, sesiones, invitaciones, contadores y auditoría; sin modificar pacientes. Fresh/upgrade8→9/rollback transaccional/reintento/reinicio y conservación probados en PG17/18 descartables. Suite staff usa DOCTORCHART_CLINICS_TEST_DATABASE_URL, migración BASELINE_TEST_ADMIN_DSN; conservar validaciones de ownership y nombres de fixtures existentes.

STAFF_ENABLED=false por defecto; opt-in de ensayo requiere BOOKINGS_ENABLED/SCHEDULES_ENABLED y STAFF_ORIGIN HTTPS exacto. Ninguna autorización para ejecutar en bases reales. Un binario previo no reconoce9: recuperación mediante binario compatible o restauración de respaldo autorizada, nunca modificar marcador ni borrar tablas. Informe docs/ai/M7_DELIVERY.md.

---

Antecedentes anteriores al cierre M7:

# PostgreSQL — baseline 1 y esquema 4 (M2)

`db/schema.sql` permanece inmutable (23 tablas). `migrations/0002_clinics.sql` agrega `clinics`, `app_schema_migrations`, un índice y el validador IANA; el esquema 2 tiene 25 tablas y 21 FK. M2 agrega clinic_entitlement_epoch mediante 0003_entitlement_epoch.sql. La corrección 0004_clinic_timezone_policy.sql alinea la función IANA y revalida el CHECK: esquema 4 conserva 26 tablas y 21 FK. Baseline y migraciones 0002/0003 intactos. No modifica pacientes ni relaciones clínicas. `AGENTS.md` conserva `production: true`.

## Arranque y contrato

`Ensure` conserva pgxpool, READ COMMITTED, contexto 30s, lock_timeout 10s, statement_timeout 20s, search_path public/pg_catalog y advisory lock transaccional (17209, 3).
- Vacío real: baseline → catálogo 1 → migración 2 → catálogo 2 → migración 3 → catálogo 3 → migración 4 → catálogo 4 → seeds → marcador.
- Versión 1: verifica identidad, checksum del baseline y catálogo anterior esperado antes de DDL; migra 1 → 2 → 3 → 4 conservando initialized_at y datos administrados.
- Versiones 2/3: valida catálogo/historial publicados antes de aplicar las transiciones restantes hasta 4.
- Versión 4: verifica identidad, checksums, historial y catálogo; seeds idempotentes sin reescribir marcador/historial.
- Sin marcador con objetos, versiones desconocidas, historial ausente/extra/alterado, checksum incompatible o drift: rechazo sin adopción ni reparación.

**Este binario aplica el upgrade en el próximo arranque sobre versión 1/2/3 válida.** No iniciarlo contra una base real sin autorización específica de migración. Solo se ejecutó en bases descartables propias.
DDL, seeds y metadatos confirman en una sola transacción antes de HTTP/jobs. Error/cancelación revierte todo; rollback usa contexto de limpieza independiente acotado a 5s.

## Catálogo e historial

`app_schema_state.sql_sha256` identifica el baseline inmutable; la columna histórica `baseline_version` registra ahora la versión instalada (1..4). `app_schema_migrations` registra versiones 2, 3 y 4, checksum SQL, catálogo anterior/final y applied_at.
Hashes SQL equivalentes para CRLF/LF. Los hashes de catálogo esperados están fijados en `migrations.go`, obtenidos de bases vacías independientes desde los SQL publicados; nunca recalcularlos desde una base existente para ocultar drift.

`catalog.go` permanece intacto: estructura, constraints/índices, secuencias, funciones, tipos y extensiones; excluye filas, OIDs, valores de secuencias, propietarios y permisos. Las constraints heredadas pueden repetir nombre; las pruebas comparan definiciones completas como multiconjunto.
Se verificaron PostgreSQL 17.11 y 18.6 con contratos distintos: 18 incluye constraints NOT NULL adicionales y coincide con el motor documentado del VPS. Otras versiones mayores se rechazan antes de DDL. **El cambio de motor 17 → 18 no forma parte de M1:** no reescribir automáticamente marcadores/fingerprints de una restauración entre versiones.

## Lugares

`clinics.workspace_id` referencia el workspace del médico sin cascada de borrado. Nombre y ubicación no vacíos; zona IANA real (UTC o región nombrada); estados ACTIVE/INACTIVE/ARCHIVED; versión no negativa; timestamps ordenados.
Índice `(workspace_id, status, id)` para consultas futuras por propietario y FK.
Sin lugares inventados durante migración ni asociación clínica a lugares. M2 agrega gestión de lugares/capacidades, sin horarios, reservas ni staff. [Derechos efectivos/fence](../docs/ai/contracts/M2_EFFECTIVE_ENTITLEMENTS.md).

## Pruebas descartables

Entornos autorizados y ejecutados:
- `doctorchart-m1-20261029`: PostgreSQL 17.11, 127.0.0.1:55442.
- `doctorchart-m1-pg18-20261029`: PostgreSQL 18.6, 127.0.0.1:55440.
- Label `codex.task=doctorchart-m1`, rol `migration_fixture`, datos temporales sin volumen persistente. Verificar identidad antes de crear/destruir fixtures; nunca usar DSN de desarrollo.

`db/init_test.go` crea/elimina únicamente `stage3_baseline_doctorchart_*`. Las suites M1 requieren cinco bases vacías separadas: `doctorchart_schema_test`, `doctorchart_integrity_test`, `doctorchart_growth_test`, `doctorchart_followups_query_test` y `doctorchart_startup_test`.
Para M2 agregar doctorchart_clinics_test y doctorchart_entitlements_test (total siete bases fijas y ocho DSN contando admin). Con el servidor propio autorizado y esas bases creadas, definir variables solo para el proceso de pruebas (contraseña del fixture, no de una base real):

```powershell
$fixtureRoot = 'postgres://migration_fixture:<fixture-password>@127.0.0.1:55442'
$env:BASELINE_TEST_ADMIN_DSN = "$fixtureRoot/postgres?sslmode=disable"
$env:DOCTORCHART_SCHEMA_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_schema_test?sslmode=disable"
$env:DOCTORCHART_INTEGRITY_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_integrity_test?sslmode=disable"
$env:DOCTORCHART_GROWTH_TEST_DSN = "$fixtureRoot/doctorchart_growth_test?sslmode=disable"
$env:DOCTORCHART_FOLLOWUP_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_followups_query_test?sslmode=disable"
$env:DOCTORCHART_STARTUP_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_startup_test?sslmode=disable"
$env:DOCTORCHART_CLINICS_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_clinics_test?sslmode=disable"
$env:DOCTORCHART_ENTITLEMENTS_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_entitlements_test?sslmode=disable"
go test ./cmd/api -run '^TestStartupCreatesSchemaAndServesHealth$' -count=1 -v
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
govulncheck ./...
```

Race requiere CGO y compilador C. Windows tenía CGO=0; race se ejecutó realmente en `golang:1.27.3` Linux con GCC 14.2, fuentes montadas en solo lectura y red del PostgreSQL 17 propio. Un proxy loopback temporal del contenedor llevó 55442 a 5432; no se modificaron guards del fixture.
No ejecutar dos suites simultáneas contra las mismas bases con nombres fijos. Growth exige empezar vacío y se limpia al terminar.

La prueba de arranque debe mostrar `--- PASS: TestStartupCreatesSchemaAndServesHealth`; un `SKIP` por ausencia de `DOCTORCHART_STARTUP_TEST_DATABASE_URL` no valida esta integración. `TestBaselineUpgradeWithRealSeedsPreservesAdminConfiguration` usa `bootstrap.Run` y los seeds reales sobre baseline 1, verifica transición hasta 4, conservación de configuraciones administradas y reinicio idempotente.

## M6: esquema 8, reservas públicas

0008 agrega cuatro tablas de publicación, sedes públicas, detalles administrativos versionados y rate-limits anónimos; 34 tablas/32 FK. Baseline y 0002..0007 inmutables. `go test ./db -run '^TestPublicBookingMigration' -count=1` verifica upgrade M5→8, conservación de clínica/reservas, SQL fallido/drift/seed tardío, rollback/reintento, reinicio y fresh=upgrade; ejecutado en PostgreSQL 17/18 descartables. Ver entrega M6 para fingerprints independientes y evidencia.

El rollback es transaccional ante fallo, no un DOWN después de aceptar reservas públicas. M5 rechaza el marcador 8; cualquier recuperación real exige autorización independiente, respaldo/restauración probada o binario compatible que conserve escrituras nuevas. No rebajar versión ni borrar datos personales/contadores/reservas para eludir la validación. Ninguna migración real fue ejecutada.

## M5: esquema 7, persona atendida administrativa

0007 agrega attendee_name nullable con CHECK e inmutabilidad, sin backfill ni cambios clínicos. El lector usa COALESCE(attendee_name,contact_name) para reservas previas. Eventos y recibos anteriores no se reescriben. Instalación vacía y upgrade M4→7 convergen; `go test ./db -run '^TestAttendeeMigration' -count=1` comprueba conservación, fallo tardío, rollback/reintento y reinicio. Ejecución aprobada sobre PostgreSQL 17/18 descartables, además de suite completa. Baseline/0002..0006 permanecen intactos; catálogos fijados desde SQL independiente, registrados en entrega M5.

Sin DOWN destructivo: una vez confirmado esquema 7, M4 rechaza ese marcador. Antes de una aplicación real futura exigir respaldo/restauración verificada y permiso separado; recuperación mediante binario compatible o restauración autorizada que preserve las escrituras nuevas. No rebajar marcador, reescribir checksums ni eliminar reservas/pacientes para volver atrás.

## M4: esquema 6 y pruebas de reservas

Migración `0006_bookings.sql` posterior a M3/esquema 5; no modifica baseline ni migraciones publicadas. Instalación vacía y upgrade conservador llegan a 30 tablas/28 FK y el mismo catálogo. Exclusión GiST por workspace e intervalo `[start,end)` para estados distintos de CANCELLED, sin extensión nueva. FK clínica opcional RESTRICT: no se eliminan silenciosamente vínculos a pacientes.

Con las DSN descartables anteriores (nunca una base real):
```sh
go test -p 1 ./... -count=1
go test -race ./internal/clinics -run '^TestBookings' -count=1
go test ./db -run '^TestBookingMigration' -count=1
go vet ./...
go build ./...
```
La suite de migración comprueba M3→6, SQL fallido/drift/seed tardío, rollback integral, reintento, datos clínicos y agendas conservados, catálogo fresh/upgrade, reinicio con reserva/evento/recibo y rechazo de checksum alterado. El rollback es transaccional ante fallo, no un DOWN destructivo después de confirmar reservas.

Antes de una aplicación real futura se requiere respaldo/restauración probada y aprobación independiente. Un binario M3 rechaza esquema 6: no usarlo como rollback tras confirmar 0006. Conservar las reservas nuevas exige un binario compatible o recuperación autorizada; no rebajar marcador ni borrar tablas. Este documento no autoriza migraciones o despliegues reales.

## Política horaria de consultorios y upgrade 3 → 4

UTC explícito y nombres regionales IANA exactos; Go carga tzdata y SQL consulta pg_timezone_names. Se excluyen offsets, abreviaturas sin región, Local, posix/, right/ y todo Etc/ (incluye Etc/GMT±N y aliases Etc/UTC/Etc/GMT; usar UTC explícito al crear una sede). La política es del campo timezone del consultorio, no cambia el contexto clínico global timeZone.

Migración 4 toma lock de clinics contra escrituras durante el preflight. Si **cualquier fila**, incluso archivada, contiene Etc/, aborta con conteo y mensaje de conciliación explícita del propietario. No actualiza, normaliza ni borra datos. La transacción preserva catálogo, filas, secuencias, historial y marcador de versión 3 al fallar; no alcanza readiness. No se debe sortear el rechazo editando fingerprints o migraciones históricas. Resolver datos incompatibles requiere decisión/autorización independiente; nunca reparación automática.

El CHECK se recrea y valida dentro de la misma transacción porque reemplazar su función no revalida filas antiguas por sí solo. Las pruebas cubren datos clínicos/consultorios, estado de secuencia, rechazo de filas excluidas, fallo SQL, drift, seed tardío, rollback/reintento y catálogo idéntico entre instalación vacía y upgrade. Fingerprints independientes de esquema 4: PG17 85cbcbb463c81876f40babf1d156444bb64793e41388c511bd5c661159b362d6; PG18 1eb0e7bb7b9e3f10aa39c0eaa0101727f304d834ecd5ce9fdbbf77764e73ae7f.

El rollback probado es transaccional ante un upgrade fallido. No hay DOWN automático tras confirmar versión 4: binarios previos rechazan ese marcador; recuperación operativa requiere binario compatible o respaldo/restauración autorizada y verificada. No se ejecutó ninguna recuperación real.

Cobertura: vacío/upgrade convergentes; todos los objetos originales intactos; snapshots de tablas originales con pacientes, consultas, recetas, estudios, medios y vacunas representativos; IDs, referencias, secuencias y marcador preservados; reinicios; 16 inicializadores; cancelación; errores SQL/seed; rollback y reintento; drift/historial/checksums y restricciones de lugares. Regresiones clínicas/wizard se ejecutan sin lugares.
Converter externo y sincronización Billing requieren servicios opt-in adicionales y no se ejecutaron en M1.

## Aplicación futura y recuperación — no ejecutadas ni autorizadas

Leer `../docs/vps-operations.md` y revalidar identidad/versión/servicio. Este procedimiento no autoriza ejecución:
1. Obtener revisión de Argon y aprobación del propietario. Validar el contrato sobre copia aislada representativa y permisos DDL del propietario.
2. Respaldar consistentemente la base DoctorChart, medios referenciados y configuración; probar restauración aislada y conservar imagen/SHA previos.
3. Planificar ventana sin escrituras y detener únicamente consumidores DoctorChart, preservando PostgreSQL compartido y servicios vecinos.
4. Solo con autorización específica y preflight de zonas aprobado, iniciar imagen nueva: bootstrap aplicará las transiciones pendientes hasta 4. Verificar salud, versión 4, tres filas de historial, catálogo, datos clínicos/consultorios y reinicio antes de reabrir escrituras.
5. Si falla dentro de la transacción, versión anterior (1/2/3), catálogo y datos permanecen intactos; corregir causa y reintentar. Nunca reparar hashes manualmente. Filas Etc/ requieren conciliación explícita, no conversión automática.
6. Tras confirmar versión 4, M1 y el binario M2 anterior rechazan el marcador 4. No sirve como rollback directo: se necesita imagen compatible o restauración autorizada del respaldo verificado, antes de reabrir escrituras. Si ya hubo escrituras nuevas, resolver su conservación antes de restaurar.

No hay DOWN destructivo ni eliminación de lugares. No se actualizaron servicios reales, configuración de conexión ni autenticación Supabase.

M2 conserva pruebas desde esquemas publicados 1/2/3 hasta 4, rollback y conservación. El fence de invalidación comparte generación entre réplicas; el marcador nunca se rebaja para volver a M1. La recuperación real exige procedimiento y permiso separados. El esquema Billing no se modificó.
