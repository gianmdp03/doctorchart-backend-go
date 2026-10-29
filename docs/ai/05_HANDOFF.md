# Handoff — correcciones M1 completadas
**29/10/2026 (Argentina) · v0.3 · rama `codex/m1-clinics-migrations`.**
**M1 listo para aprobación del propietario; revalidación de Argon sobre las correcciones pendiente.**

## Autorización y auditoría recibida
El propietario informó el veredicto independiente de Gemini Argon: **REQUIERE CORRECCIONES, 0 críticos, 1 obligatorio y 2 observaciones menores**. Autorizó corregir exclusivamente M1, ejecutar pruebas descartables, crear un commit específico y actualizar coordinación.
El test de arranque había quedado omitido en la fase anterior por faltar su DSN; la suite previa no acreditaba esa integración. Se reprodujo su fallo real con 25 tablas frente a la expectativa heredada de 23 y se corrigió.
No se autorizó M2, merge a main, push, bases reales o despliegues. Tampoco cambiar mecanismo o esquema por observaciones sin defecto comprobado.

## Commits y estado
- Baseline previo: `c2c30c1142b39519532eb986dcad811fbaa2619b`.
- 26 documentos independientes: `d5a89e05eebf70de1dca72ed0b01a74467b6b1a4`, donde permanece main.
- Implementación M1: `48f87b1d929e8496aecf203284cc4ca4d0c95820`.
- Coordinación inicial: `32d03681e44b79d742da3e25c4cfbd122f343a81`, base de esta corrección.
- Corrección: único commit específico posterior a `32d0368`, que contiene este handoff; obtener SHA final con `git log -1`.
Se conserva la rama M1 sin merge/push. Árbol limpio al cierre. No se recuperó trabajo descartado ni se crearon worktrees.

## Correcciones y archivos
- `cmd/api/main_test.go`: expectativa 23 → 25 tablas en `TestStartupCreatesSchemaAndServesHealth`.
- `db/README.md`: añadir base `doctorchart_startup_test`, sexta variable `DOCTORCHART_STARTUP_TEST_DATABASE_URL`, comando focal y comprobación explícita de PASS en vez de SKIP.
- `internal/bootstrap/migration_test.go`: nueva prueba `TestBaselineUpgradeWithRealSeedsPreservesAdminConfiguration`. Parte de baseline 1 real, usa seeds reales, conserva ocho tablas de catálogos/configuración administrada y verifica `bootstrap.Run` 1 → 2 y reinicio idempotente sin lugares inventados.
- Coordinación actualizada: inicio, decisiones, pendientes, tablero, este handoff, protocolo, roadmap, matriz, prompt de reinicio, índice y ticket M1; entrega original Argon marcada como histórica y nuevo [registro de correcciones](reviews/M1_CORRECTIONS.md).
- M2 solo registra validación IANA en Go adicional a SQL; no se implementó.
Sin cambios en `AGENTS.md`, `db/schema.sql`, `db/catalog.go`, `db/init.go`, `db/migrations.go`, SQL de migración ni lógica productiva. Pacientes/historias siguen por médico; ninguna relación clínica se modifica.

## Pruebas ejecutadas
Go 1.27.3. Fixtures propios autorizados con label `codex.task=doctorchart-m1-fixes` y role `migration_fixture`:
- `doctorchart-m1fix-pg18-20261029`: PostgreSQL 18.6, loopback 55440, tmpfs.
- `doctorchart-m1fix-pg17-20261029`: PostgreSQL 17.11, loopback 55442, tmpfs.
- `doctorchart-m1fix-race-20261029`: Linux golang:1.27.3/GCC, fuentes readonly y red del fixture 17, proxy loopback temporal como en M1.
Bases de schema, integrity, growth, followups y **startup** separadas; admin baseline crea/elimina exclusivamente sus bases con prefijo controlado. Se habilitaron las **seis DSN**. Contenedores propios retirados al finalizar.

| Comando / entorno | Resultado |
|---|---|
| Test startup focal antes de corregir, Windows/PG18.6 | FAIL reproducido: health served with 25 tables and 1 specialties |
| go test ./cmd/api ./internal/bootstrap -run '^(TestStartupCreatesSchemaAndServesHealth\|TestBaselineUpgradeWithRealSeedsPreservesAdminConfiguration)$' -count=1 -v, Windows/PG18.6 | PASS ambos, sin SKIP |
| go test ./... -count=1 -json, Windows/PG18.6 | PASS, exit 0, startup habilitado |
| go test -race ./... -count=1 -json, Linux/PG17.11 | PASS, exit 0; 395 casos/subcasos PASS, 5 omitidos |
| go build ./...; go vet ./...; gofmt -l; git diff --check | PASS |

En race, startup PASS (21.39s) y nueva transición con seeds PASS (0.35s). El test de startup verificó health 503 al perder PostgreSQL, ping 200, y recuperación health/ping 200. La primera ejecución focal Windows también acreditó ambos PASS.
Omitidos por falta de fixtures externos:
- `TestLiveConverterLocalStorage`
- `TestLiveConverterOrientationAndSpecialPreservation`
- `TestLiveConverterS3AndApplicationIsolation`
- `TestLiveStudyHTTPConverterAndPersistence`
- `TestLimitsSyncDoctorChartFixture`
No se omitieron startup, bootstrap ni las suites PostgreSQL de M1. Paquetes sin tests no cuentan como pruebas omitidas.
No se reejecutó govulncheck en esta corrección: no cambian dependencias ni código productivo; su resultado previo no se presenta como nuevo. No hubo pruebas de servicios reales de Supabase/Gateway ni producción.

## Resultado y próximo paso
Corrección obligatoria resuelta; observación de seeds implementada y probada; observación horaria registrada para M2. **M1 listo para aprobación**, sin afirmar que Argon haya revalidado este commit o que el propietario haya aceptado el cierre.
Revalidar con Argon según protocolo y recibir aceptación. Luego el propietario definirá la reorganización de Sol/Argon para tareas independientes en paralelo sin worktrees. Esa reorganización y M2 no se iniciaron.
El contrato operativo y recuperación de M1 no cambian: futuro arranque migra una versión 1 válida; el binario anterior rechaza versión 2. Todo acceso/migración real o despliegue requiere aprobación independiente.
