# DoctorChart — M5: multiespecialidad real

Implementación del 10/11/2026. Fuentes: solicitud explícita de M5, AGENTS.md
de ambos repositorios y handoffs `specialty-contexts-m1.md` hasta `m4.md`.
Las decisiones de esos handoffs prevalecen sobre auditorías históricas de
consultorios. No se implementó M6.

## Modelo y estados

Un usuario y un workspace administrativo por médico; varios contextos clínicos
en ese workspace. Catálogo seleccionable limitado a `pediatrics`,
`general_medicine`, `dermatology`, activos y con wizard publicado.
Unicidad `(workspace_id,specialty_id)`; no se crean identidades ni catálogos
personales adicionales. No hay cuotas comerciales nuevas por contexto.

Cada contexto conserva definición efectiva, borrador, versiones y onboarding.
Recién agregado: borrador publicado independiente, onboarding pendiente,
sin definición efectiva. Apto para atención: onboarding completo y definición
efectiva presente. Editar un nuevo borrador de un contexto ya completo conserva
su aptitud y su configuración efectiva. Un wizard pendiente no bloquea otros
contextos ni la administración global cuando existe uno apto.

## Migración 15 y conservación

`db/migrations/0015_multiple_specialty_contexts.sql`, aplicada por el runner
existente después de 14. Retira exclusivamente la unicidad transitoria
`doctor_specialty_contexts_single_context_key`. Conserva unicidad de workspace
por usuario y todas las FK, identidades y protecciones de M1–M4.

Agrega `specialty_context_preferences`: columnas visibles y notas rápidas,
con versión propia. Copia los valores y versión administrativos originales
al contexto existente. Un trigger inicializa preferencias vacías para cada
contexto nuevo. Las columnas antiguas del workspace se conservan como valores
históricos congelados; el código M5 sólo lee/escribe las preferencias del
contexto. No son una preferencia activa global ni una segunda autoridad.
Guardar preferencias no incrementa la versión del wizard.

Agrega `publication_specialty_contexts`, con pertenencia compuesta al workspace
y FK a la publicación principal. Migra la selección del contexto existente
sólo si está listo. Agregar otro contexto no lo publica automáticamente.
Un trigger rechaza creación o reprogramación de reservas en contextos sin
configuración efectiva; permite conservar y administrar las reservas históricas.

Resultado: **49 tablas y 61 FK**. Baseline, catálogo uniforme, migraciones
0002–0014, seed de las tres especialidades y AGENTS.md contrastados por SHA256
contra el preflight de M5: intactos. No se modificaron wizard predeterminados.

Fingerprints derivados independientemente, validando cada versión anterior:

| PostgreSQL | Catálogo 15 |
| --- | --- |
| 17 | `b63538131c81d2c6a3cf22664535537739fa27d5105c7dd4a6099a1a24f59f12` |
| 18 | `ec6569a87c61e4def77d26d74da5d978de842608d391fcd5b5a1d679379e5e27` |

El runner reconoce 15 y valida historial/checksums y estado previo; conserva
pgx, READ COMMITTED, contexto 30 s, lock_timeout 10 s, statement_timeout 20 s
y advisory lock transaccional 17209/3. Confirma esquema, seeds y marcador
atómicamente antes de HTTP/jobs. No adopta ni repara esquemas con drift.

## Contratos y autorización

| Operación | Contrato |
| --- | --- |
| Listar propios | `GET /api/doctorchart/workspace/contexts` |
| Agregar | `POST /api/doctorchart/workspace/contexts`, `{specialtyId}`; 201 con setup nuevo |
| Setup/preferencias/notas | Rutas existentes con `?specialtyContextId=<id>` |
| Mutaciones del wizard | Body existente con `specialtyContextId`, `expectedVersion` y campos de la operación |
| Clínica | Contexto explícito en query, incluyendo multipart/PDF/impresión |
| Disponibilidad administrativa | Filtro opcional `specialtyContextId`; slots incluyen su contexto |
| Publicación | `specialtyContextIds: string[]` en el contrato administrativo existente |
| Perfil público | `specialties: [{id,code,name,clinics}]`; una identidad pública principal |
| Disponibilidad/reserva pública | `specialtyContextId`; selección automática únicamente si existe una sola oferta apta |

Cada resumen propio incluye `id`, `workspaceId`, `version`, `specialty`,
`onboardingCompleted`, `ready`, `hasDraft`. Duplicados: 409
`SPECIALTY_ALREADY_ADDED`. La creación usa lock de catálogo, lock de workspace
y unicidad SQL, incluso con solicitudes concurrentes.

`readWorkspace` selecciona un ID perteneciente al usuario. Sin ID, sólo admite
el caso inequívoco de un único contexto; varios producen 400
`SPECIALTY_CONTEXT_REQUIRED`. No se usa LIMIT 1 para resolver ambigüedad.
Un contexto ajeno produce 404 `SPECIALTY_CONTEXT_NOT_FOUND`. El perfil global
multiespecialidad devuelve `specialty:null` y los contextos, sin elegir uno.

Las entradas HTTP clínicas exigen ID explícito, pertenencia y aptitud. Las
consultas internas heredadas con ID cero sólo admiten un único contexto del
propietario; nunca enumeran todos cuando hay varios. Se conserva la autorización
del paciente, consulta, receta, estudio, archivo, impresión y capacidades.

## Pacientes, wizard y navegación

El mismo DNI puede existir en contextos distintos del mismo médico, con IDs e
historias independientes. No hay matching, copias ni fusiones automáticas.
El frontend usa `/patients?specialtyContextId=...`, `/records/:id?...`,
`/dashboard?...`, `/onboarding/setup?...` y demás rutas clínicas explícitas.
Reutiliza íntegramente el editor/renderer/validadores actuales.

La URL es la autoridad de selección de la pantalla. El store captura IDs en
las escrituras; el interceptor y los scopes descartan respuestas anteriores.
La preferencia inicial usa sessionStorage por workspace, independiente por
pestaña y subordinada a la URL. Los guards se ejecutan en orden explícito:
sesión/plan, contexto autorizado, capacidades. No asumir que Angular ordena
secuencialmente un array de guards. La restauración de sesión conserva query.

El selector del encabezado, también visible en móvil, cambia contexto. Los
formularios ofrecen guardar primero, descartar o cancelar; guardar primero
mantiene la pantalla para utilizar su botón de guardado. Un guardado pendiente
impide el cambio. Cambiar una pantalla clínica abre el dashboard destino,
evitando reutilizar un ID de paciente del contexto anterior.

## Horarios, agenda y reservas

Consultorios compartidos; cada bloque conserva contexto, identidad y duración.
Mis horarios elige entre contextos aptos, autoseleccionando el único si existe.
Un borrador de agenda puede contener bloques de varias especialidades. Se
mantienen excepciones, zonas IANA, conflictos y verificaciones de reservas.

Agenda global con filtros independientes por consultorio, especialidad y estado.
Cambiar el selector clínico conserva esos filtros y todos los turnos. Las fichas
y el alta desde una reserva usan su contexto persistido. Para buscar/vincular
una ficha, se entra explícitamente en la especialidad de la reserva.

El slot autorizado determina contexto, bloque y duración de creación. Reprogramar
conserva especialidad y rechaza bloques de otra. La FK paciente-contexto impide
vínculos cruzados. Se conservan snapshots históricos, versiones, eventos,
idempotencia, notificaciones y exclusión global por workspace. No se reconstruye
el bloque desconocido de las reservas antiguas.

## Directorio y secretaría

La publicación selecciona explícitamente especialidades aptas. Las ofertas
públicas requieren además sede activa/publicada y bloques válidos. El directorio
no duplica la identidad médica por contexto. El visitante selecciona especialidad
antes de sede/slot; un enlace `?specialtyContextId=...` la preselecciona y una
única oferta evita el paso adicional. Se conserva formulario administrativo,
prueba de trabajo, constancias e idempotencia; reservar no crea pacientes.
Reintentos de reservas ya confirmadas siguen funcionando tras despublicar.

Secretaría mantiene permisos por sede y frontera administrativa. No se agregan
permisos clínicos ni buscador; ocupación global y snapshots siguen vigentes.

## Evidencia de ejecución

Laboratorios propios Docker: PostgreSQL **17.11 y 18.6**, puertos loopback
55468/55470, label `codex.task=doctorchart-specialty-m5-20261110`; Go **1.27.3
linux/amd64**. Ningún binario HTTP Go se ejecutó en Windows.

| Comprobación | Resultado |
| --- | --- |
| Go build y vet | PASS en Linux |
| Suite completa PostgreSQL 17/18 | PASS; incluye suites completas con `-race` en ambos |
| Migración 14→15 y vacío | PASS, conservación, fingerprints, reinicio, checksum alterado, rollback SQL/drift/seed tardío y reintento |
| Concurrencia | PASS, inicialización concurrente existente y alta duplicada con un solo ganador |
| Mismo médico, varios contextos | PASS: wizard/preferencias independientes, DNI repetido, primeras consultas, resúmenes, recetas, negación cruzada de recursos y HTTP |
| Horarios/reservas/publicación | PASS: dos contextos por sede, duraciones, reprogramación compatible/incompatible, ficha correcta, selección pública, replay tras despublicar |
| Regresiones existentes | PASS: integridad SQL, exclusión global, privacidad staff, notificaciones e idempotencia |
| TypeScript app y spec | PASS |
| Build Angular desarrollo | PASS |
| Angular completo | 841 PASS / 3 FAIL preexistentes, 844 tests; sin errores no manejados |
| Navegador Chrome | PASS con API SIMULADA; dos pestañas, decisiones de cambios, agenda/filtros, horarios, alta pendiente, enlace y reserva pública, móvil 390 px |

Tests nuevos: `db/multiple_context_catalog_test.go`,
`db/multiple_contexts_migration_test.go`,
`internal/workspace/multiple_contexts_integration_test.go`,
`internal/clinical/multiple_contexts_integration_test.go`,
`internal/clinics/multiple_contexts_test.go`. Los fixtures y assertions históricos
se actualizaron para 15 sin debilitar los pins anteriores.

Comandos Go dentro de los laboratorios: `go build ./...`, `go vet ./...`,
`go test -p 1 ./... -count=1`, `go test -race -p 1 ./... -count=1`, con DSN
explícitas de ensayo para schema/integrity/growth/clinics/followups/startup/
entitlements y BASELINE_TEST_ADMIN_DSN. No ejecutar esos tests con DSN reales:
las suites de integración reinicializan sus bases descartables identificadas.

Logs locales: `%TEMP%/doctorchart-specialty-m5-{full17-final,full18,race17,race18}.log`,
`doctorchart-m5-{angular-final,browser-final,build-final}.log`. El frontend conserva
el smoke reproducible `scripts/specialty-contexts-m5-browser.cjs`; usa el build de
desarrollo, Chrome y respuestas sintéticas. Esto **no es E2E real con el backend**.
Los tests Go sí ejercitan HTTP/SQL reales sobre los laboratorios; Billing usa
fixtures locales y las integraciones externas opt-in sin servicios siguen SKIP.

## Riesgos, operación y límites

Los tres fallos antiguos de flags son calendar.guard, schedules.guard y
public-appointments.guard. No se cambiaron flags/guards para ocultarlos.
Build productivo: BLOCKED por `supabase.publishableKey` ausente. Smoke estático
productivo: no acreditado; contra desarrollo falla la expectativa del bundle
productivo (`assert.ok(script)`), tras comprobar rutas. Smoke productivo y E2E
con servicios externos siguen pendientes; no se inventaron credenciales.

El nuevo backend aplicaría 15 al arrancar. **No iniciarlo contra bases reales
sin autorización independiente.** Revisar `vps-operations.md`, respaldar base y
medios y probar restauración aislada antes de una futura aplicación aprobada.
Un fallo de migración revierte toda la transacción. Después de confirmar 15,
un binario 14 rechaza el marcador: no existe DOWN ni rebaja de versión automática.
La recuperación requiere binario compatible o restauración autorizada, con
conservación explícita de las escrituras posteriores. No se desplegó ni restauró.

No hay eliminación/archivo/suspensión de contextos, identidad compartida de
pacientes, módulos clínicos nuevos ni ampliación del catálogo. M6 requiere
una nueva misión: partir del esquema 15 y estos contratos, preservar las tres
especialidades y resolver sus criterios propios sin reinterpretar M1–M5.

## Estado de los repositorios

Backend main HEAD `6869461`; frontend main HEAD `ff14f0c`. El diff contra HEAD
incluye trabajo previo y M1–M4 sin commit, preservado; no atribuirlo entero a M5.
M5 agrega 0015, selección/alta real, preferencias/publicación por contexto,
guards/selector/rutas y las adaptaciones/pruebas descritas. No hay commits,
pushes, worktrees, resets, despliegues ni cambios en bases reales. Los recursos
Docker de ensayo se retiraron al cerrar la ejecución, verificando label y puertos.
El handoff equivalente del frontend detalla las pantallas y pruebas Angular.

**M5 termina aquí. No iniciar M6 automáticamente.**
