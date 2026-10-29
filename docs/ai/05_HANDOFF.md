# Handoff — M1 implementado, revisión pendiente
**29/10/2026 (Argentina) · v0.3 · rama `codex/m1-clinics-migrations`.**

## Autorización y límites
El propietario aprobó implementar M1 según ticket, crear primero un commit con los 26 documentos, trabajar en rama específica y crear/ejecutar pruebas automatizadas en bases descartables.
Se preservaron pacientes/historias por médico, baseline y AGENTS.md (`production: true`). Sin cambios Angular, Gateway, Billing, autenticación Supabase, turnos/agenda/staff ni altas comerciales. No hubo bases reales, migraciones productivas, despliegues, pushes o merge.
El trabajo anterior fue descartado intencionalmente: no recuperarlo ni reconstruirlo. Ruta pública futura confirmada `/appointments`, sin alias/redirecciones; segmentos de médico/consultorio pendientes.

## Commits y repositorios
Raíz común `C:/Users/gcast/Documents/GitHub/`. Inspección local sin fetch.

| Repo | Rama | HEAD / referencia |
|---|---|---|
| doctorchart-backend-go, baseline previo | main al iniciar | c2c30c1142b39519532eb986dcad811fbaa2619b |
| Doctor Chart, 26 docs independientes | main; base de rama M1 | d5a89e05eebf70de1dca72ed0b01a74467b6b1a4 |
| Doctor Chart, implementación M1 | codex/m1-clinics-migrations | 48f87b1d929e8496aecf203284cc4ca4d0c95820 |
| doctorchart-frontend | main, limpio e intacto | 14196974125e5769c5a95e309896397acad96e63 |
| api-ai-gateway-go | main, limpio e intacto | 55197fa5701d15ed67a69e395bee4e3699c5a705 |
| bycode-backend-go | main, limpio e intacto | bf65c042cc7480cbf4603447becfeb14002dbde1 |

Este handoff y la actualización de coordinación se confirman en un commit documental posterior a `48f87b1`; consultar HEAD de la rama para su SHA final. `main` queda en `d5a89e0`, sin implementación M1. Árbol limpio al finalizar; sin PR o push.

## Resultado y archivos
Commit funcional: siete archivos, 653 líneas agregadas / 34 eliminadas.
- `db/init.go`: valida estado previo, migra versión 1 → 2 y seeds en una transacción, valida resultado y actualiza marcador; rollback cancelable con limpieza acotada.
- `db/migrations.go`: migración embebida, checksum, historial estricto y catálogos esperados por PostgreSQL 17/18.
- `db/migrations/0002_clinics.sql`: `clinics`, FK a workspace, datos de ubicación, zona IANA, estados, versión, timestamps, índice y ledger.
- `db/migrations_test.go`: conservación, convergencia, rollback/reintento, concurrencia/cancelación, historial/drift e integridad.
- `db/init_test.go` y `internal/bootstrap/schema_test.go`: actualizar conteos esperados (25 tablas / 21 FK).
- `db/README.md`: contrato, pruebas y procedimiento futuro de respaldo/recuperación.
Sin cambios en `AGENTS.md`, `db/schema.sql`, `db/catalog.go` ni código clínico de aplicación. El test de bootstrap cambia un único conteo.

Coordinación de cierre: actualizados `00_START_HERE.md`, `02_DECISIONS.md`, `03_PENDING_CONTRACTS.md`, `04_TASK_BOARD.md`, `05_HANDOFF.md`, `06_PROTOCOL.md`, `07_SESSION_STARTERS.md`, `08_ROADMAP.md`, `09_TEST_MATRIX.md`, `10_REPOSITORY_MAP.md`, `NEW_CHAT_SOL_PROMPT.md`, `README.md`, `tasks/R0_CONTEXT_RESET.md` y `tasks/M1_FOUNDATION.md`; creado `reviews/M1_ARGON_BRIEF.md`. Sin modificar los históricos originales de R0 durante M1.

## Contrato operativo
En próximo arranque autorizado, este binario migra automáticamente una versión 1 intacta antes de HTTP/jobs. Vacío y upgrade llegan al mismo catálogo 2; no crea lugares por defecto. Versión 2 valida historial/checksums y catálogo; no reescribe metadatos en reinicios.
Se conservan pgx, READ COMMITTED, contexto 30s, lock_timeout 10s, statement_timeout 20s y advisory lock (17209, 3). Baseline inmutable; la columna histórica `baseline_version` registra versión instalada 2.
La guía del VPS indica PostgreSQL 18.6, distinto del objetivo inicial 17: se verificaron ambos en contenedores propios y se fijaron contratos distintos porque 18 expone constraints NOT NULL adicionales. Otras versiones mayores se rechazan antes de DDL; cambiar de motor requiere fase independiente.

## Pruebas ejecutadas y resultados
Go 1.27.3. PostgreSQL descartable propio, role `migration_fixture`, label `codex.task=doctorchart-m1`:
- `doctorchart-m1-20261029`: 17.11, loopback 55442, datos tmpfs.
- `doctorchart-m1-pg18-20261029`: 18.6, loopback 55440, datos tmpfs.
- Race: `doctorchart-m1-race-20261029`, golang:1.27.3 Linux/GCC 14.2, repos montados solo lectura y red del PostgreSQL 17 propio. Proxy loopback temporal para puerto 55442, sin cambiar guards de tests.
Los tres contenedores propios se retiran al cerrar M1; no se reutilizan servicios ajenos. No persistieron datos reales ni fixtures.

| Comando / control | Resultado |
|---|---|
| gofmt -l sobre fuentes Go; git diff --check | PASS, sin salida |
| go build ./... | PASS |
| go vet ./... | PASS |
| go test ./db -count=1, PostgreSQL 17.11 | PASS (ensayo focal) |
| go test ./... -count=1, Windows/PG17.11 | PASS (antes de agregar compatibilidad 18) |
| go test ./... -count=1, Windows/PG18.6 | PASS, código final |
| go test -race ./... -count=1, Linux/PG17.11 | PASS, código final |
| govulncheck ./... y -show verbose | exit 0; 0 vulnerabilidades alcanzables/importadas |

Las cinco variables de BD se configuraron explícitamente para admin, schema, integrity, growth y followups. Las suites de PostgreSQL correspondientes no fueron omitidas. Reproducción y nombres de bases en [db/README](../../db/README.md).
Se probó que los objetos clínicos originales y sus filas permanecen intactos (pacientes, consultas, recetas, estudios, medios, vacunas, IDs/referencias/secuencias), sin lugares obligatorios. La comparación de catálogo del test se corrigió para tratar constraints de nombre repetido como multiconjunto; no fue necesario cambiar el catálogo de producción.
Python no estaba disponible: se ejecutaron manualmente los equivalentes de build/vet/tests/race/formato del checker Go.
No ejecutados: converter HTTP externo, fixture end-to-end Billing/webhooks y servicios Supabase/Gateway reales, por no ser parte de M1. Fixtures sintéticos/locales sí ejecutados.
Govulncheck informa GO-2026-5932 en módulo indirecto `golang.org/x/crypto/openpgp`, sin paquetes importados ni llamadas afectadas. No se cambiaron dependencias por ese aviso ajeno a M1.

## Riesgos y revisión pendiente
- **Argon no auditó todavía.** No declarar aceptación de fase ni ausencia de hallazgos independientes.
- El binario anterior rechaza marcador 2; rollback después del commit SQL requiere imagen compatible o restauración autorizada, no basta revertir imagen. Rollback transaccional por errores sí probado.
- Ejecución real/ventana de mantenimiento, permisos DDL, respaldo y restauración se documentaron, no se ejecutaron ni autorizaron.
- No hay API de lugares: autorización de negocio y cupos se implementan en M2 tras contrato Billing, nunca desde acceso SQL al fixture.

## Próximo paso
Gemini Argon revisa el rango `d5a89e0..48f87b1` en solo lectura con [brief](reviews/M1_ARGON_BRIEF.md); después el propietario evalúa resultados. **M2 no autorizado ni iniciado.**
