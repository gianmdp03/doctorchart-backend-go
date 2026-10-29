# M1 — correcciones posteriores a Gemini Argon
**29/10/2026 · CORRECTIONS_COMPLETE / READY_FOR_APPROVAL.**
Revalidación Argon y aceptación final del propietario pendientes. M2 no iniciado.

## Auditoría y autorización
Fuente: resumen de auditoría independiente comunicado por el propietario en esta conversación: **REQUIERE CORRECCIONES; 0 críticos, 1 obligatorio, 2 observaciones menores**. No se recibió un archivo adicional del informe.
Se autorizó corregir exclusivamente M1 y ejecutar ensayos descartables. Base de corrección `32d03681e44b79d742da3e25c4cfbd122f343a81`, rama `codex/m1-clinics-migrations`. Un único commit específico contiene correcciones y coordinación; identificar su SHA con `git log -1`.

## Hallazgos atendidos
| Hallazgo | Resolución |
|---|---|
| Obligatorio: startup esperaba 23 tablas y faltaba su DSN en instrucciones | `cmd/api/main_test.go` espera 25; `db/README.md` incorpora `doctorchart_startup_test`, `DOCTORCHART_STARTUP_TEST_DATABASE_URL` y ejecución focal que exige PASS |
| Menor: transición con bootstrap.Run y seeds reales | Nuevo `internal/bootstrap/migration_test.go`: baseline 1 verdadero, seeds reales, configuración administrada modificada; Run migra a 2 y conserva ocho tablas de catálogos/configuración, initialized_at y checksum; reinicio idempotente, una migración, cero lugares inventados |
| Menor: zonas horarias en Go | Registrado en tablero y contratos pendientes para M2, adicional al CHECK SQL. No se implementa M2 |

El mecanismo de migraciones, SQL publicado, hashes, catálogo, AGENTS y aplicación productiva permanecen intactos. Ningún defecto comprobado requirió alterarlos.

## Reproducción y pruebas reales
Go 1.27.3; label `codex.task=doctorchart-m1-fixes`; role `migration_fixture`:
- PostgreSQL 18.6: `doctorchart-m1fix-pg18-20261029`, loopback 55440, tmpfs.
- PostgreSQL 17.11: `doctorchart-m1fix-pg17-20261029`, loopback 55442, tmpfs.
- Race Linux/GCC en `doctorchart-m1fix-race-20261029`, fuentes readonly; red del PG17 propio y proxy loopback temporal, sin modificar guards.
Se usaron seis variables de BD: admin baseline, schema, integrity, growth, followups y startup. Ver [instrucciones corregidas](../../../db/README.md). Contenedores propios retirados al cierre.

1. **Antes de corregir:** startup focal Windows/PG18.6 falla con `health served with 25 tables and 1 specialties`. Confirma el hallazgo; la suite anterior lo omitía por falta de DSN.
2. **Después:** `go test ./cmd/api ./internal/bootstrap -run '^(TestStartupCreatesSchemaAndServesHealth|TestBaselineUpgradeWithRealSeedsPreservesAdminConfiguration)$' -count=1 -v`: PASS ambos en PG18.6, sin SKIP.
3. `go test ./... -count=1 -json`, Windows/PG18.6: exit 0.
4. `go test -race ./... -count=1 -json`, Linux/PG17.11: exit 0, 10 paquetes con pruebas PASS, 395 casos/subcasos PASS y 5 omitidos. Startup PASS 21.39s; transición con seeds PASS 0.35s.
5. `go build ./...`, `go vet ./...`, `gofmt -l` y `git diff --check`: PASS.

Startup ejecutó health 503 ante desconexión PostgreSQL, ping 200 y recuperación health/ping 200. No se omitió esa integración en las ejecuciones corregidas.

## Omitidos explícitos
Faltaban fixtures externos de converter y Billing, fuera del alcance de esta corrección:
- `TestLiveConverterLocalStorage`
- `TestLiveConverterOrientationAndSpecialPreservation`
- `TestLiveConverterS3AndApplicationIsolation`
- `TestLiveStudyHTTPConverterAndPersistence`
- `TestLimitsSyncDoctorChartFixture`

Paquetes sin archivos de test no cuentan como tests omitidos. No se reejecutó govulncheck (dependencias y código productivo intactos), ni se usaron servicios reales Supabase/Gateway, bases reales o producción.

## Revisión y cierre
Comparar `git diff 32d0368 HEAD`; cambios de código limitados a tests. Para verificar integridad, `git diff 32d0368 HEAD -- AGENTS.md db/schema.sql db/catalog.go db/init.go db/migrations.go db/migrations/0002_clinics.sql cmd/api/main.go internal/bootstrap/bootstrap.go` debe estar vacío.
Los documentos actualizan el estado y registran evidencia, sin aprobar fases posteriores.
**M1 listo para aprobación del propietario.** No afirmar que Argon revalidó este commit. No hubo merge a main, push, worktrees, migraciones reales ni despliegues. La reorganización paralela de Sol/Argon queda para después de aceptar M1, según orden del propietario.
