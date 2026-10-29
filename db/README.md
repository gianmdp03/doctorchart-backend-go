# PostgreSQL — baseline 1 y esquema 2 (M1)

`db/schema.sql` permanece inmutable (23 tablas). `migrations/0002_clinics.sql` agrega `clinics`, `app_schema_migrations`, un índice y el validador IANA; el esquema 2 tiene 25 tablas y 21 FK. No modifica pacientes ni relaciones clínicas. `AGENTS.md` conserva `production: true`.

## Arranque y contrato

`Ensure` conserva pgxpool, READ COMMITTED, contexto 30s, lock_timeout 10s, statement_timeout 20s, search_path public/pg_catalog y advisory lock transaccional (17209, 3).
- Vacío real: baseline → validación catálogo 1 → migración 2 → validación catálogo 2 → seeds → marcador.
- Versión 1: verifica identidad, checksum del baseline y catálogo anterior esperado antes de DDL; migra 1 → 2 conservando initialized_at y datos administrados.
- Versión 2: verifica identidad, checksums, historial y catálogo; ejecuta seeds idempotentes sin reescribir marcador/historial.
- Sin marcador con objetos, versiones desconocidas, historial ausente/extra/alterado, checksum incompatible o drift: rechazo sin adopción ni reparación.

**Este binario aplica el upgrade en el próximo arranque sobre versión 1 válida.** No iniciarlo contra una base real sin autorización específica de migración. M1 solo se ejecutó en bases descartables propias.
DDL, seeds y metadatos confirman en una sola transacción antes de HTTP/jobs. Error/cancelación revierte todo; rollback usa contexto de limpieza independiente acotado a 5s.

## Catálogo e historial

`app_schema_state.sql_sha256` identifica el baseline inmutable; la columna histórica `baseline_version` registra ahora la versión instalada (1 o 2). `app_schema_migrations` registra versión 2, checksum SQL, catálogo anterior/final y applied_at.
Hashes SQL equivalentes para CRLF/LF. Los hashes de catálogo esperados están fijados en `migrations.go`, obtenidos de bases vacías independientes desde los SQL publicados; nunca recalcularlos desde una base existente para ocultar drift.

`catalog.go` permanece intacto: estructura, constraints/índices, secuencias, funciones, tipos y extensiones; excluye filas, OIDs, valores de secuencias, propietarios y permisos. Las constraints heredadas pueden repetir nombre; las pruebas comparan definiciones completas como multiconjunto.
Se verificaron PostgreSQL 17.11 y 18.6 con contratos distintos: 18 incluye constraints NOT NULL adicionales y coincide con el motor documentado del VPS. Otras versiones mayores se rechazan antes de DDL. **El cambio de motor 17 → 18 no forma parte de M1:** no reescribir automáticamente marcadores/fingerprints de una restauración entre versiones.

## Lugares

`clinics.workspace_id` referencia el workspace del médico sin cascada de borrado. Nombre y ubicación no vacíos; zona IANA real (UTC o región nombrada); estados ACTIVE/INACTIVE/ARCHIVED; versión no negativa; timestamps ordenados.
Índice `(workspace_id, status, id)` para consultas futuras por propietario y FK.
Sin lugares inventados durante migración, endpoints, altas comerciales, cuotas, agenda, staff ni asociación de pacientes a lugares.

## Pruebas descartables

Entornos autorizados y ejecutados:
- `doctorchart-m1-20261029`: PostgreSQL 17.11, 127.0.0.1:55442.
- `doctorchart-m1-pg18-20261029`: PostgreSQL 18.6, 127.0.0.1:55440.
- Label `codex.task=doctorchart-m1`, rol `migration_fixture`, datos temporales sin volumen persistente. Verificar identidad antes de crear/destruir fixtures; nunca usar DSN de desarrollo.

`db/init_test.go` crea/elimina únicamente `stage3_baseline_doctorchart_*`. Las otras suites requieren cuatro bases vacías separadas: `doctorchart_schema_test`, `doctorchart_integrity_test`, `doctorchart_growth_test` y `doctorchart_followups_query_test`.
Con el servidor propio autorizado y esas bases creadas, definir variables solo para el proceso de pruebas (contraseña del fixture, no de una base real):

```powershell
$fixtureRoot = 'postgres://migration_fixture:<fixture-password>@127.0.0.1:55442'
$env:BASELINE_TEST_ADMIN_DSN = "$fixtureRoot/postgres?sslmode=disable"
$env:DOCTORCHART_SCHEMA_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_schema_test?sslmode=disable"
$env:DOCTORCHART_INTEGRITY_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_integrity_test?sslmode=disable"
$env:DOCTORCHART_GROWTH_TEST_DSN = "$fixtureRoot/doctorchart_growth_test?sslmode=disable"
$env:DOCTORCHART_FOLLOWUP_TEST_DATABASE_URL = "$fixtureRoot/doctorchart_followups_query_test?sslmode=disable"
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
govulncheck ./...
```

Race requiere CGO y compilador C. Windows tenía CGO=0; race se ejecutó realmente en `golang:1.27.3` Linux con GCC 14.2, fuentes montadas en solo lectura y red del PostgreSQL 17 propio. Un proxy loopback temporal del contenedor llevó 55442 a 5432; no se modificaron guards del fixture.
No ejecutar dos suites simultáneas contra las mismas bases con nombres fijos. Growth exige empezar vacío y se limpia al terminar.

Cobertura: vacío/upgrade convergentes; todos los objetos originales intactos; snapshots de tablas originales con pacientes, consultas, recetas, estudios, medios y vacunas representativos; IDs, referencias, secuencias y marcador preservados; reinicios; 16 inicializadores; cancelación; errores SQL/seed; rollback y reintento; drift/historial/checksums y restricciones de lugares. Regresiones clínicas/wizard se ejecutan sin lugares.
Converter externo y sincronización Billing requieren servicios opt-in adicionales y no se ejecutaron en M1.

## Aplicación futura y recuperación — no ejecutadas ni autorizadas

Leer `../docs/vps-operations.md` y revalidar identidad/versión/servicio. Este procedimiento no autoriza ejecución:
1. Obtener revisión de Argon y aprobación del propietario. Validar el contrato sobre copia aislada representativa y permisos DDL del propietario.
2. Respaldar consistentemente la base DoctorChart, medios referenciados y configuración; probar restauración aislada y conservar imagen/SHA previos.
3. Planificar ventana sin escrituras y detener únicamente consumidores DoctorChart, preservando PostgreSQL compartido y servicios vecinos.
4. Solo con autorización específica, iniciar imagen nueva: el bootstrap aplicará 1 → 2. Verificar salud, versión 2, una fila de historial, catálogo, conteos clínicos y reinicio antes de reabrir escrituras.
5. Si falla dentro de la transacción, versión 1/datos permanecen intactos; corregir causa y reintentar. Nunca reparar hashes manualmente.
6. Tras confirmar versión 2, el binario anterior rechaza el marcador 2. No sirve como rollback directo: se necesita imagen compatible o restauración autorizada del respaldo verificado, antes de reabrir escrituras. Si ya hubo escrituras nuevas, resolver su conservación antes de restaurar.

No hay DOWN destructivo ni eliminación de lugares. No se actualizaron servicios reales, configuración de conexión ni autenticación Supabase.
