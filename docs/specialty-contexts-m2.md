# DoctorChart — M2: pacientes e historias por contexto

Fecha: 10/11/2026. Continuación de [M1 de especialidades](specialty-contexts-m1.md).
Las auditorías antiguas de `docs/ai/sources/` corresponden a consultorios, ya
implementados. El pedido actual y M1 son la fuente de decisiones para esta M2.
Este documento permite iniciar la revisión de M3 sin recuperar el chat.

## Decisiones y modelo final

Una cuenta y un workspace administrativo por médico. Consultorios compartidos
y agenda global siguen en el workspace. Cada paciente tiene un único contexto
clínico, independiente de otros registros de la misma persona física. No hay
identidad maestra, matching, fusión ni sincronización por DNI/contacto.

Sólo Pediatría, Medicina General y Dermatología; se conserva el mismo motor de
wizard. No se cambió `seed.json` durante M2 ni se habilitaron nuevas
especialidades. Se mantiene `doctor_specialty_contexts_single_context_key`:
exactamente un contexto por workspace durante esta etapa. No hay selector ni
alta de un segundo contexto.

Los campos clínicos, respuestas, antecedentes, fórmulas, consultas y snapshots
permanecen en sus registros actuales. M2 agrega a `patients`:

- `specialty_context_id NOT NULL`: identidad estable del contexto propietario.
- `workspace_id NOT NULL`: puente de integridad para conservar `user_id` y
  comprobar su concordancia mediante claves foráneas, sin duplicar el usuario
  propietario en la configuración del contexto.

El mismo DNI puede seguir apareciendo en registros independientes. No se agrega
unicidad global ni se deduplican datos. Los IDs de pacientes y recursos, UUID
de medios, fechas, versiones y snapshots previos se conservan.

## Integridad PostgreSQL: esquema 12

`0012_patient_specialty_contexts.sql` evoluciona 11 → 12. Baseline y migraciones
0002–0011 permanecen intactos. Resultado: 45 tablas, 51 FK.

| Relación/restricción | Garantía |
|---|---|
| `patients(workspace_id,user_id)` → `doctor_workspaces(id,user_id)` | El usuario del paciente es propietario del workspace |
| `patients(specialty_context_id,workspace_id)` → `doctor_specialty_contexts(id,workspace_id)` | El contexto pertenece al mismo workspace |
| `patient_prescription_records(patient_id,author_user_id)` → `patients(id,user_id)` | El autor concuerda con el propietario del paciente |
| `patient_calculation_logs(patient_id,user_id)` → `patients(id,user_id)` | Los cálculos no cambian de propietario |
| `patient_calculation_logs(encounter_id,patient_id)` → `clinical_encounters(id,patient_id)` | La consulta del cálculo pertenece a su paciente |

Se agregan las claves únicas compuestas necesarias para esas FK. Los índices
`patients_context_name_id_idx` y `patients_context_id_idx` permiten filtrar y
ordenar por contexto. No se introduce un motor nuevo de búsqueda.

Los triggers impiden cambiar usuario/workspace/contexto de un paciente y mover
consultas, recetas, estudios, vacunas o cálculos a otro paciente. Una consulta
debe guardar la especialidad del contexto de su paciente. El vínculo del contexto
al workspace es inmutable y su especialidad no se puede cambiar cuando ya tiene
pacientes. Las inserciones toman un lock compartido del contexto para excluir
un cambio simultáneo de especialidad; una prueba real comprueba el bloqueo y el
rechazo después del commit de la inserción.

Los demás recursos heredan contexto desde su paciente. No se agregaron columnas
de contexto indiscriminadamente ni cascadas nuevas de borrado. Las referencias
del paciente al workspace/contexto son `ON DELETE RESTRICT`.

La migración comprueba que cada propietario tenga contexto y que las consultas
históricas concuerden con él; las FK nuevas validan autores y asociaciones
adicionales. Un caso inconsistente falla y revierte, sin inventar propietarios,
crear contextos, cambiar especialidades ni reescribir snapshots. El backfill
asocia por `patients.user_id` al único contexto existente de su workspace.

Se conserva el runner de M1: pgx, READ COMMITTED, contexto 30s, lock_timeout 10s,
statement_timeout 20s, advisory lock transaccional (17209,3), identidad/checksum
del baseline, historial y catálogo previo esperado. DDL, backfill, seeds y
marcador confirman juntos antes de HTTP/jobs. Reinicios validan, no reparan.

Pins de catálogo derivados desde SQL publicado en bases vacías independientes,
verificando todos los pins anteriores:

| PostgreSQL | Catálogo 12 SHA-256 |
|---|---|
| 17 | `4f8d8d11fe7587d7142b881e829e3fd797bbf02f241088fb430c45437512b9c1` |
| 18 | `7bc2b3b4b1d32e5f21a5ff8c875d11ecfc64978c2cb50b636ed11f6c7c720499` |

## Contrato y autorización

Toda ruta registrada por `internal/clinical` exige un único query parameter
positivo `specialtyContextId`. Ejemplo:

```http
GET /api/doctorchart/patients/42/clinical-summary?specialtyContextId=7
```

Incluye listados, búsqueda, dashboard, pacientes, consultas, recetas, archivos,
PDF, curvas, vacunas, cálculos y catálogos/configuración clínica. Las rutas
administrativas, reservas, staff, login, perfil y wizard conservan sus contratos;
el wizard de M1 sigue enviando su ID en el cuerpo, como antes.

- Ausente o repetido: 400 `SPECIALTY_CONTEXT_REQUIRED`.
- Inválido/no positivo: 400 `INVALID_REQUEST`.
- Contexto inexistente o de otro usuario: 404 `SPECIALTY_CONTEXT_NOT_FOUND`.
- Paciente/consulta/documento fuera del ámbito autorizado: 404 sin contenido
  clínico. Las denegaciones de recursos que antes distinguían propietario ajeno
  con 403 ahora ocultan su existencia con 404.
- Concurrencia de registros y configuración: se mantienen los tokens/códigos
  existentes. `workspaceVersion` sigue siendo la versión clínica de M1.

Gateway autentica igual que antes. Go resuelve el contexto a través del
workspace del usuario autenticado, vincula el ID al `context.Context` de la
petición y aplica propietario + contexto al paciente y agregados. No hay estado
global de especialidad autorizante. `Effective` obtiene la configuración de ese
contexto al crear registros y no acepta uno ajeno.

Las llamadas internas Go sin scope explícito conservan compatibilidad con las
fixtures y la cardinalidad de M1; no son un contrato HTTP alternativo. Al
habilitar varios contextos deberán recibir scope o derivarlo explícitamente del
recurso. Ningún endpoint clínico admite el fallback: el binder exige ID positivo.

La carga de estudios conserva admisión/límite de tamaño antes de adquirir DB;
después autoriza el paciente/contexto antes de convertir y vuelve a comprobar
antes de persistir. Una respuesta 413/503 de admisión no requiere consultar una
historia. El flujo de medios y las asociaciones por UUID de paciente permanecen.

`PatientDetailDTO`, `PatientListDTO` y consultas devuelven `specialtyContextId`.
Las escrituras no aceptan cambiarlo en el cuerpo. Los hijos sin columna de
contexto se identifican por su paciente y el scope de la solicitud.

## Recursos clínicos y conservación

Lectura/edición/eliminación de pacientes y consultas se autorizan en el ámbito
correcto. Recetas, órdenes, estudios, archivos, vacunación, crecimiento,
cálculos, antecedentes y resúmenes pasan por el paciente autorizado. Dashboard,
directorio, búsqueda de consultas y próximos controles filtran por contexto.
La primera consulta sigue siendo única por paciente, como antes.

La impresión de una consulta usa su especialidad guardada y la de una receta se
deriva del contexto inmutable del paciente. No depende de una especialidad
administrativa actualmente seleccionada. Los formularios históricos se siguen
leyendo con su snapshot; M2 no los convierte a los defaults vigentes.

Se investigó `DeletePatient`: mantiene el borrado explícito y transaccional
existente, con paciente bloqueado, hijos concretos y cola de archivos; los
cálculos conservan su cascada previa. Ahora la autorización contextual ocurre
antes de tocar hijos. No se agrega ninguna cascada ni se usa borrado para
trasladar pacientes. Se evaluó archivo: no es necesario para separar contextos y
se deja fuera de M2 un subsistema de retención. El borrado existente sigue siendo
destructivo y requiere la acción explícita de la UI; esto no autoriza ejecutar
borrados sobre datos reales durante la implementación.

Las cuotas clínicas siguen globales por médico y con las reglas existentes.
No se agregan límites comerciales por especialidad. Consultorios, disponibilidad,
exclusión de solapamientos, reservas públicas y permisos de secretaría conservan
su funcionamiento; sólo sus fixtures de pacientes incorporan la pertenencia.

## Angular

`ClinicalContextService` recibe el ID validado del `WorkspaceStore` de M1.
El interceptor clínico captura cuenta/contexto, obtiene el setup inicial si hace
falta y agrega el query parameter a la petición inmutable. Rechaza un ID
explícito diferente, cambios durante la resolución y respuestas tardías de otro
contexto/cuenta. No reenvía escrituras bajo un scope nuevo y no envía el scope a
servicios externos ni rutas administrativas. Opera con los interceptores de auth
y zona horaria existentes, sin agregar headers CORS.

La selección de paciente se revalida por API, comprueba el contexto de la
respuesta y guarda sólo su ID bajo cuenta + contexto. Las claves antiguas sin
contexto no se usan para restaurar una selección clínica. Se cancelan lecturas
y se limpian selección/cachés al cambiar de ámbito.

Directorio/dashboard, caché de consultas, borradores de ficha/estudios/recetas,
vacunación, curvas y previews de impresión incorporan contexto a sus claves de
vigencia. Las pantallas mantienen sus formularios y protecciones de versión;
los guards de identidad de alta/edición también incluyen el contexto. No se
añaden pantallas, selector, motor de wizard, HTML ni estilos nuevos para M2.

## Dependencias para M3–M5

- M3 podrá referenciar contexto y workspace coherentes al agregar bloques de
  horarios. Mantener consultorios compartidos y exclusión global por médico.
- M4 deberá decidir/persistir la contextualización de reservas y comprobar que
  cualquier paciente asociado pertenezca al contexto correcto. No convertir
  reservas en pacientes ni inferir vínculo por DNI.
- M5 retirará la unicidad transitoria sólo mediante una migración autorizada,
  luego de verificar todos los contratos. Selector y alta de segundo contexto
  todavía no existen. Los tests de dos contextos simultáneos del mismo workspace
  siguen fuera de M2; se prueban dos propietarios y rechazos cruzados.
- Revisar al abrir multiespecialidad los callers internos sin scope, wizard de
  M1 y preferencias auxiliares que usan propietario o `(workspace,specialty)`;
  no resolver arbitrariamente uno de varios contextos. Los endpoints clínicos de
  M2 ya transportan scope explícito. No agregar especialidades antes de cerrar M6.

## Aplicación futura y recuperación

La migración 12 sólo se preparó y ensayó en laboratorios propios. No iniciar el
binario contra una base real sin autorización explícita: `Ensure` aplicaría 12.
Antes de una aplicación autorizada, respaldar datos/esquema/archivos, comprobar
una restauración con el mismo major PostgreSQL, revisar inconsistencias en un
clon y detener instancias/jobs anteriores. Coordinar frontend y backend porque
los clientes clínicos anteriores no envían el query obligatorio.

Verificar versión/historial 12, catálogo, asociaciones, conteos e HTTP antes de
aceptar tráfico. Un fallo previo al commit conserva 11 y permite corregir la
causa y reintentar; datos inconsistentes requieren diagnóstico/corrección
autorizada, no reparación automática. Después del commit, usar binario compatible
o restauración autorizada que contemple escrituras nuevas. No rebajar marcadores,
reescribir checksums ni ejecutar un DOWN destructivo. El timeout uniforme implica
ensayar también el volumen real en un clon antes de autorizar el cambio.

## Verificación ejecutada

Go 1.27.3 `linux/amd64`, imagen `golang:1.27.3`; fuentes y dependencia local
de medios montadas en sólo lectura. PostgreSQL 17.11 y 18.6 Debian, con bases
creadas exclusivamente para esta validación. Contenedores
`doctorchart-specialty-m2-pg17-20261110` (127.0.0.1:55468) y
`doctorchart-specialty-m2-pg18-20261110` (127.0.0.1:55470), label
`codex.task=doctorchart-specialty-m2-20261110`, rol `migration_fixture`.
Las DSN sólo apuntaron a esos laboratorios. Se conservaron los guards de loopback
y nombres de bases usando proxies internos de Linux. No se ejecutaron servidores
DoctorChart nuevos en Windows ni se cambiaron reglas del firewall.

| Comando/verificación | Resultado |
|---|---|
| `gofmt -l cmd db internal` | PASS |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -p 1 ./... -count=1`, PostgreSQL 17 | PASS |
| `go test -p 1 ./... -count=1`, PostgreSQL 18 | PASS |
| `go test -race -p 1 ./... -count=1`, PostgreSQL 17 | PASS |
| Inserción vs cambio concurrente de especialidad, PG17 y PG18 | PASS |
| TypeScript `npx tsc -p tsconfig.spec.json --noEmit` | PASS |
| Angular, scope HTTP/selección/borradores/respuestas tardías | 22 PASS |
| Angular, cadena real de interceptores scope/auth/timeZone | 5 PASS, incluidos en la suite completa |
| `npx ng test --watch=false`, suite completa final | 818 PASS / 3 FAIL preexistentes, 100 archivos |
| `npx ng build --configuration development` | PASS, 2 rutas prerenderizadas |
| `npm run build`, producción | BLOCKED: falta clave pública Supabase |
| `git diff --check`, ambos repositorios | PASS |

Pruebas nuevas:

- `db/patient_context_catalog_test.go`: catálogo independiente desde esquema vacío.
- `db/patient_contexts_migration_test.go`: upgrade real 11 → 12 con datos,
  conservación, reinicio/idempotencia, igualdad fresh/upgrade, inconsistencias
  de propietario/contexto/especialidad/autor, SQL fallido, drift, seed tardío,
  rollback/reintento, NOT NULL/FK/inmutabilidad e inserción concurrente frente a
  cambio de especialidad. No se retira la restricción de un contexto.
- `internal/clinical/patient_context_test.go`: pacientes de igual DNI de dos
  propietarios, primeras consultas independientes, consultas posteriores,
  recetas, rechazo cruzado de lectura/escritura/borrado/documentos/PDF/estudios/
  vacunas/crecimiento/cálculos, búsqueda, listado y dashboard contextual, y
  rechazo de cambios de contexto en el cuerpo.
- Angular: `clinical-context.interceptor.spec.ts`, `patient-context-state.spec.ts`
  y extensiones de `active-patient.service.spec.ts` cubren setup inicial,
  peticiones inmutables, multipart/documentos/PDF, transiciones durante carga,
  respuestas tardías, cachés, storage, borradores y ausencia de scope en destinos
  administrativos/externos.

Se reutilizaron las suites de M1, bootstrap, Pediatría, Medicina General,
Dermatología, pacientes/formularios/historia, recetas/impresión, estudios/medios,
resúmenes, vacunas, curvas, cálculos, consultorios, horarios y reservas. Las
fixtures actuales ahora crean la pertenencia obligatoria y las comparaciones
históricas excluyen sólo las columnas nuevas, comprobadas por pruebas específicas.
No se relajaron catálogos, fingerprints ni constraints para hacer pasar tests.

## Limitaciones y riesgos pendientes

Los tres fallos Angular siguen siendo `calendar.guard.spec.ts`,
`schedules.guard.spec.ts` y `public-appointments.guard.spec.ts`: esperan flags
apagados aunque HEAD ya los tiene activos. No se modificaron esos archivos ni
`feature-flags.ts`. Los tests se ejecutaron con Node 26.13.0 y
`NODE_OPTIONS=--no-experimental-webstorage`, sólo en el proceso de validación,
igual que en M1. No se cambió código para ocultar esos fallos.

La build de producción requiere la clave pública Supabase ausente; no se
inventaron credenciales ni se omitió el validador. No hubo login/E2E con Supabase
real ni validación de despliegue. Las pruebas de conversor real, Billing real y
fixtures cross-service permanecen SKIP por servicios no configurados. Los flujos
internos usan PostgreSQL real y servicios HTTP sintéticos donde corresponde.
`govulncheck` no se ejecutó; no se presenta esta etapa como auditoría de seguridad.

La migración cambia contratos clínicos y el esquema reconocido: requiere
coordinación de versiones y autorización separada para una aplicación real.
Históricos inconsistentes impiden el arranque hasta corregirlos mediante un
procedimiento autorizado; no se reparan automáticamente. El borrado explícito
existente permanece destructivo y el archivo/retención no se implementó.

## Archivos y estado final

Cambios centrales de M2: `db/migrations/0012_patient_specialty_contexts.sql`,
`db/{init,migrations}.go`, `internal/api/clinical_context.go`,
`internal/workspace/store.go`, `internal/clinical/context.go` y registros de
rutas/lectores clínicos (`patient`, `directory`, `encounter`, `encounter_query`,
`follow_up`, `printing`, `calculator_http`, `catalog`, `growth`, `prescription`,
`studies`, `summary`, `vaccination`). Pruebas nuevas indicadas arriba y fixtures
de schema/bootstrap, migración y clínica/agenda adaptadas a los campos nuevos.

Frontend: `app.config.ts`, modelos de paciente/consulta, `workspace.store`,
`clinical-context.service`, interceptor clínico, selección/storage, barreras de
lectura/caché, impresión, estado del chat, formularios y componentes/stores de la
ficha. Se actualizaron fixtures y tests relacionados. Documentación: esta guía,
`db/README.md` y su equivalente en el frontend.

Backend permanece en `main`, HEAD `6869461`; frontend en `main`, HEAD `ff14f0c`.
M1 y los cambios locales anteriores siguen sin sobrescribir ni descartar; todos
los cambios de M2 quedan sin staged/commit/push. No hubo worktrees, resets,
despliegues ni modificaciones en bases reales. Los contenedores/volúmenes propios
se eliminaron tras verificar identidad, label y puertos. El trabajo se detiene
en M2; no se comenzó M3.
