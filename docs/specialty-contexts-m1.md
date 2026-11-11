# M1 de especialidades — workspace administrativo y contexto clínico

Fecha: 10/11/2026. Esta M1 es la primera etapa del rediseño de especialidades;
no es la antigua M1 de consultorios de `docs/ai/`. Las dos auditorías guardadas
en `docs/ai/sources/` y `docs/ai/02_DECISIONS.md` corresponden al trabajo anterior.
Las decisiones explícitas del propietario en el pedido actual prevalecen para
esta etapa. Consultorios, agenda, reservas y secretaría ya implementados se
conservan; aquí sólo se adaptan sus lecturas de especialidad.

## Decisiones y límite

- Una cuenta y un workspace administrativo por médico.
- Contextos clínicos pertenecientes al workspace; pacientes independientes por
  especialidad recién en M2.
- Un motor de wizard configurable existente, con configuraciones independientes.
- Consultorios compartidos y agenda global; los bloques por especialidad se
  resolverán en etapas posteriores.
- Sólo Pediatría, Medicina General y Dermatología durante M1–M6.
- M1 permite exactamente un contexto por workspace. No ofrece selector, alta de
  segunda especialidad, cambios de pacientes, bloques ni reservas nuevas.

## Modelo anterior y nuevo

Antes, `doctor_workspaces` reunía propietario, preferencias y configuración
clínica. Ahora conserva `id`, `user_id` único, fechas, versión administrativa,
columnas de pacientes y notas rápidas. Todas sus relaciones administrativas,
incluidas las de consultorios y exclusiones de turnos por workspace, mantienen
los mismos identificadores y restricciones.

`doctor_specialty_contexts` agrega identidad bigint propia, FK restrictiva al
workspace y a `specialties`, definición efectiva, borrador, versiones de origen
efectiva/borrador, versión de concurrencia clínica, paso, finalización del
onboarding y fechas. `onboarding_completed` es el estado de configuración
existente; el borrador puede existir tanto antes como después de finalizar.

`UNIQUE(workspace_id,specialty_id)` impide duplicar una especialidad. Todos los
contextos de M1 están vigentes: no hay ciclo de archivo/activación adicional.
`UNIQUE(workspace_id)` impone el límite transitorio de un contexto, incluso
ante escrituras concurrentes. Quitar esa última restricción requiere una etapa
posterior autorizada, después de contextualizar pacientes y demás operaciones.

No quedan columnas clínicas duplicadas en el workspace. La selección inicial
crea workspace y contexto en una transacción. Cambiar la selección antes de
finalizar reutiliza sus identidades; después se conserva `SPECIALTY_LOCKED`.
No se modificó `seed.json` como parte de M1: se conservaron los defaults y demás
cambios locales que ya existían al comenzar.

## PostgreSQL

`production=true` permanece. `db/schema.sql`, `db/catalog.go` y migraciones
0002–0010 permanecen inmutables. `0011_specialty_contexts.sql` transfiere los
valores clínicos existentes, incluidas versiones y fechas, y retira las siete
columnas transferidas. No altera pacientes, consultas, adjuntos, historial,
auditorías ni relaciones administrativas. Resultado: esquema 11, 45 tablas y
46 FK.

Se amplía el runner existente, sin introducir otro: identidad y checksum del
baseline, historial ordenado/checksums/catálogos previos, pgx, READ COMMITTED,
contexto 30s, lock_timeout 10s, statement_timeout 20s, advisory lock transaccional
(17209,3). Esquema, transferencia de datos, seeds y marcador confirman juntos
antes de HTTP/jobs. Un error revierte toda la operación y permite reintentar.
Un reinicio valida y no repara ni adopta drift.

Pins de esquema 11 derivados desde SQL en PostgreSQL vacío e independiente,
verificando cada pin anterior; nunca desde una base de desarrollo:

| Motor | SHA-256 de catálogo |
|---|---|
| PostgreSQL 17 | `4ca79751d5ab10aebc37ede7f2060428b028196c3e3dc0052b88d410f5f5dc77` |
| PostgreSQL 18 | `53f4f60b1b92053687ad14fed7dd5e73a3dd20c6f841e3647052a67ccbf89bf7` |

El binario anterior rechaza versión 11. No hay operación simultánea de binarios
anteriores/nuevos sobre el mismo esquema. El siguiente arranque del nuevo
binario prepara/aplica 11 mediante `Ensure`: **no iniciarlo contra una base real
sin autorización explícita de esa migración**. No se ejecutó sobre bases reales.

Para una aplicación futura autorizada: respaldar esquema/datos y archivos,
probar restauración en un entorno aislado con el mismo major PostgreSQL,
detener todas las instancias/jobs y verificar versión 10 intacta antes del
arranque nuevo. Comprobar marcador 11, historial, catálogo, conteos, lectura de
wizard y salud antes de volver a aceptar tráfico. Ante fallo previo al commit,
el rollback transaccional deja 10 y se puede reintentar con la causa corregida.
Después del commit, recuperar con binario compatible o restauración autorizada
que contemple las escrituras posteriores. Nunca rebajar marcadores/checksums ni
ejecutar un DOWN destructivo para volver al binario viejo.

## Go y contratos HTTP

Se conservan las rutas existentes. `GET /workspace` y respuestas de selección/
wizard siguen siendo una proyección para la UI actual:

- `id`: workspace administrativo, como antes.
- `specialtyContextId`: identidad estable del único contexto actual.
- `version`: concurrencia del contexto clínico.
- `specialty`, `experience`, definición, borrador, versiones de origen y estado:
  se leen del contexto y catálogo, usando el mismo motor/validadores.
- `/workspace/preferences.version`: concurrencia administrativa independiente.

Selección, draft, finish, reset y discard aceptan `specialtyContextId`. Angular
lo envía cuando existe contexto. Go resuelve siempre por el usuario autenticado
y compara el ID recibido; un contexto ajeno/inexistente devuelve 404
`SPECIALTY_CONTEXT_NOT_FOUND`. El campo es opcional para compatibilidad del
contrato de contexto único; omitirlo sólo opera sobre el contexto propio y nunca
permite seleccionar otro. No hay endpoint de creación de contextos adicionales.

El `workspaceVersion` histórico en pacientes/consultas sigue llamándose igual
para mantener el contrato y ahora referencia la versión de la configuración
clínica efectiva. No se reinterpretan ni reescriben snapshots históricos.
`Effective` conserva el lock compartido del workspace durante la escritura
clínica; las mutaciones de configuración toman el lock exclusivo del mismo
workspace antes de verificar/guardar la versión del contexto. La agenda global
y los locks de cupos permanecen por médico.

Los lectores de capacidades, directorio pediátrico, calculadoras e impresión
usan el contexto. Las consultas del perfil/directorio público sólo reemplazan
el origen de la especialidad por el JOIN al contexto; no cambian contratos ni
comportamientos de reservas. El catálogo visible/seleccionable se limita a los
tres códigos aprobados; el alta administrativa rechaza nuevos códigos.

## Angular

`WorkspaceSetup` identifica el contexto, `WorkspaceStore` lo conserva y envía en
mutaciones del wizard, y `WorkspaceService` tipa esas peticiones. Las preferencias
ya no fuerzan recargas ni invalidan la versión clínica cuando cambia su propia
versión. Login, limpieza de sesión y protección frente a respuestas tardías
permanecen. Se actualizaron fixtures/pruebas para el ID nuevo.

Onboarding, personalización, guards, renderer, editor y carga efectiva continúan
usando la proyección actual. No se añadieron pantallas ni otro wizard.

## Decisiones pendientes para M2

Agregar pertenencia de pacientes a contexto y autorización completa para sus
recursos; mantener independientes DNI, primera consulta e historias según las
reglas que se aprueben entonces. Todavía no persistir esa relación ni habilitar
otro contexto. Revisar posteriormente preferencias clínicas auxiliares que hoy
usan `(workspace_id,specialty_id)`, snapshots y contratos con nombre histórico
`workspaceVersion` antes de permitir multiespecialidad. No usar la proyección de
contexto único como resolución ambigua cuando se retire la restricción de M1.

## Archivos de M1

- `db/migrations/0011_specialty_contexts.sql`, `db/init.go`, `db/migrations.go`.
- `internal/workspace/{store,workspace,specialty}.go` y
  `contexts_integration_test.go`.
- `internal/clinical/{calculator_http,directory,printing}.go` y
  `specialty_context_integration_test.go`.
- `internal/clinics/public_profile.go` (sólo JOIN de especialidad).
- Regresiones y fixtures adaptados: `cmd/api/main_test.go`,
  `db/{init,migrations,bookings_migration,schedule_migration,timezone_migration}_test.go`,
  `db/specialty_{catalog_derivation,contexts_migration}_test.go`,
  `internal/bootstrap/{schema,migration}_test.go`,
  `internal/clinical/{growth,integrity,media,study_name,study_converter,summary}_integration_test.go`,
  `internal/clinical/pediatric_age_test.go`,
  `internal/clinics/{clinics,bookings_integrity}_test.go`.
- Frontend: `core/models/workspace.model.ts`,
  `core/services/workspace{.service,.store}.ts`, sus specs,
  `features/dynamic-form-renderer/clinical-dialog.fixture.ts`,
  `features/onboarding/{default-specialties,onboarding-wizard/onboarding-wizard.component}.spec.ts`.
- `db/README.md`, esta documentación y
  `doctorchart-frontend/docs/specialty-contexts-m1.md`.

Los demás archivos que ya estaban modified/untracked al iniciar son trabajo
previo preservado. No se hicieron commits, pushes, worktrees, resets ni despliegues.

## Pruebas ejecutadas y resultados

Go 1.27.3 `linux/amd64` en `golang:1.27.3`, con código y dependencia local de
medios montados en sólo lectura. PostgreSQL 17.11 y 18.6 Debian, bases propias
descartables. Se usaron exclusivamente los contenedores
`doctorchart-specialty-m1-pg17-20261110` (127.0.0.1:55468) y
`doctorchart-specialty-m1-pg18-20261110` (127.0.0.1:55470), label
`codex.task=doctorchart-specialty-m1-20261110`, rol `migration_fixture`.
Identidad/puertos verificados antes de las operaciones. Las DSN de las suites
apuntaron sólo a esas instancias; el proxy loopback dentro de Linux conservó
los guards existentes. No se cambiaron reglas del firewall.

| Verificación | Resultado |
|---|---|
| `gofmt -l cmd db internal` | PASS, sin archivos pendientes |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -p 1 ./... -count=1`, PostgreSQL 17.11 | PASS |
| `go test -race -p 1 ./... -count=1`, PostgreSQL 17.11 | PASS |
| `go test -p 1 ./... -count=1`, PostgreSQL 18.6 | PASS en la ejecución final |
| `npx tsc -p tsconfig.spec.json --noEmit` | PASS |
| Angular: workspace/onboarding/pacientes/consultas/historia | 228 PASS en 16 archivos |
| `npx ng test --watch=false`, suite completa | 809 PASS / 3 FAIL previos de flags |
| `npx ng build --configuration development` | PASS, 2 rutas prerenderizadas |
| `npm run build`, producción | BLOCKED: falta clave pública Supabase |
| `git diff --check`, ambos repositorios | PASS |

Las regresiones nuevas verifican: creación atómica, unicidad por usuario,
identidad y ciclo completo del wizard en las tres especialidades, guardar y
reanudar, aplicar/reset/discard, versiones obsoletas y operaciones concurrentes,
preferencias independientes, aislamiento entre médicos y rechazo HTTP de
contextos ajenos, conservación de consultorios y rechazo de un segundo contexto
de igual u otra especialidad. La clínica ejecuta alta/edición de pacientes,
primera consulta y posteriores, historia/resúmenes, recetas, estudios,
impresión y funciones pediátricas mediante las suites existentes, más un
recorrido nuevo de Dermatología y el recorrido previo de Medicina General.

Las pruebas de esquema incluyen vacío, reinicios, inicios concurrentes,
seeds administrados, identidad/checksums/drift, transferencia de configuración
con datos representativos, conservación de versiones/fechas, igualdad de
catálogo fresh/upgrade, fallos SQL/catálogo/seed tardío, rollback y reintento.
Se adaptó la comparación histórica para las constraints `NOT NULL` que
PostgreSQL 18 también cataloga al trasladar las columnas; los pins anteriores
y el validador de catálogo no se relajaron ni recalcularon desde datos vivos.

## Limitaciones y estado final

Los tres fallos Angular son tests antiguos de calendar/schedules/public
appointments que suponen flags apagados, aunque HEAD ya los tiene encendidos.
No se modificaron esos tests, guards ni flags. En Node 26.13.0 se usó
`NODE_OPTIONS=--no-experimental-webstorage` sólo en el proceso de tests para
que jsdom controle localStorage; ver detalles en la documentación del frontend.
La build de producción continúa bloqueada por configuración pública ausente,
sin inventar credenciales ni eludir la validación. No hubo E2E con Supabase real.

Las pruebas que requieren conversor externo o fixtures cruzados con Billing
real (`TestActualBillingEffectiveRights`, sincronización cross-service y
conversor real) quedaron SKIP por no disponer de esos servicios; no se reportan
como aprobadas. Las integraciones internas usan servicios HTTP sintéticos.
No se ejecutó `govulncheck`; ésta no es una auditoría de seguridad ni un despliegue.

Backend: `main`, HEAD `6869461`. Frontend: `main`, HEAD `ff14f0c`. Ambos conservan
sus cambios locales previos y los de M1 sin staged ni commits nuevos. Se
destruyeron únicamente los contenedores/volúmenes temporales propios de esta
validación, tras volver a comprobar label y puertos. Bases reales intactas.
La migración 11 queda preparada; su aplicación real requiere autorización
separada y coordinación de versiones. La implementación se detiene en M1.
