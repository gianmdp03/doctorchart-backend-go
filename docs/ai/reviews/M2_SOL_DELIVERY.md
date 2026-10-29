# M2-SOL — entrega auditable
**29/10/2026 · IMPLEMENTED / READY_FOR_REVIEW. Argon aún no auditó M2-SOL; M3 no autorizado.**

## Bases, permisos y límites
- Commit documental independiente previo: **6d7b0d60b946b9451eea294b98c0724530edb792** (19 archivos), descendiente del M1 aprobado 6152867.
- DoctorChart: codex/m2-clinics-backend, base 6d7b0d60b946b9451eea294b98c0724530edb792 → **1dd09bc2f5a94128150440c7c5524a44594f1358**, 17 archivos; implementación congelada.
- Billing: codex/m2-doctorchart-entitlements, base bf65c042cc7480cbf4603447becfeb14002dbde1 → **80c171c86fcae15a7d52868c5960da340bb9d179**, 11 archivos; implementación congelada, árbol limpio.
- Commits de aplicación autorizados expresamente al consolidar: scope staged comprobado por lista explícita; 26 huellas de código/tests coinciden con la entrega probada. Cierre de coordinación posterior exclusivamente documental; no cambia esos commits.
- Gateway 55197fa intacto. Frontend codex/m2-clinics-frontend/ea4f4d7b217134cfb7e3f9ccd8902dcd4beae898 auditado después en solo lectura: [informe cruzado](M2_ARGON_CROSS_REVIEW.md); sin modificaciones de Sol. No se modificó login Supabase.
- Sol libera los dos backends para revisión de los commits exactos. Sin worktrees, merge, push, producción, migraciones reales o despliegue.

## Contrato congelado y límites comerciales
[M2-CLINICS-v1](../contracts/M2_CLINICS_API.md) conserva DTO/endpoints desde congelación: SHA256 `70e096102182fb5d0880997b6ff428e058d756b987c3d8c2238c8743f780e2fd`.
[Contrato técnico](../contracts/M2_EFFECTIVE_ENTITLEMENTS.md) reutiliza metadata administrada, catálogo Billing, secreto interno existente y outbox/callback. No implementa suscripciones nuevas.
Snapshot autenticado/firmado por propietario, lease máximo 60s sin superar cobertura y generación compartida: puede autorizar con Billing caído si todavía es apto. Firma/propietario/módulo/tipos/tiempos/generación inválidos: no hay permisos. No renovar desde caché ni extender cobertura.
La respuesta 503 ENTITLEMENTS_UNAVAILABLE significa que no hay derechos aptos ni online ni en snapshot; no equivale por sí sola a toda caída remota.
**max_clinics real no configurado:** no se agregaron cantidades a planes/seeds ni se ejecutaron PUT reales. Sin clave/tier apto, mutaciones dependientes bloqueadas; lecturas seguras siguen. Trial sin catálogo apto también bloquea esas mutaciones. Requiere decisión/configuración expresa del propietario, no permiso ilimitado por defecto.

## Resumen del diff
DoctorChart:
- internal/clinics: seis rutas API, DTO BIGINT como string, propietario desde middleware existente, campos/versiones/IANA y estados.
- Cuota bajo lock estable de workspace: activos/inactivos cuentan, archivados no; última plaza concurrente admite una sola alta. Restauración requiere cupo; downgrade conserva registros.
- internal/billing/entitlements: firma, validación estricta, caché por propietario acotada y lease. Callback persiste invalidación antes de recargar reglas legacy; réplicas y fetchs en vuelo quedan fenced.
- db: migración 3 agrega únicamente clinic_entitlement_epoch, fingerprints verificados 17/18 y transición 1→2→3 o 2→3 atómica; conserva SQL baseline, migración2 y algoritmo de catálogo. Tests/arranque esperan 26 tablas, 21 FK.
Billing:
- GET interno autenticado por owner calcula derechos desde cobertura real y cuota explícita, firma resultado.
- Catálogo permite max_clinics opcional, GET module limits sigue con dos claves legacy; PUT legacy preserva cuota nueva y PUT de cuota nueva preserva cuotas clínicas omitidas.
- Concesiones/pagos confirmados agregan notificación durable dentro de transacciones existentes; no ACK si falla el outbox. Tests de cola distinguen callbacks de pagos y entregan ambos.
- Tres expectativas heredadas 17→19 tablas corregidas solo en tests; HEAD ya tenía 19 CREATE TABLE. No DDL Billing.

No toca pacientes, consultas, recetas, estudios o propiedad clínica; no horarios/reservas/staff/M3. Archivo no cancela turnos; aún no existe ese motor en esta fase.

## Ejecuciones reales
Go1.27.3, Docker propio label codex.task=doctorchart-m2:
- doctorchart-m2-pg17: PostgreSQL17.11, loopback55442; doctorchart-m2-pg18: PostgreSQL18.6, loopback55440; rol migration_fixture, tmpfs.
- doctorchart-m2-race: Linux/GCC, fuentes readonly, red PG17 y proxy loopback temporal 55442→5432. Sin cambiar guards de tests.
- Eight DSN DoctorChart: admin baseline, schema, integrity, growth, followups, startup, clinics, entitlements; instrucciones en [db/README](../../../db/README.md).
- Fixtures se retiran al cierre. No se usaron servicios o datos reales.

| Verificación | Resultado |
|---|---|
| DoctorChart go test ./... -count=1, Windows/PG18.6 | PASS |
| DoctorChart go test -race ./... -count=1 -json, Linux/PG17.11 | PASS, 404 casos/subcasos |
| Billing go test -race ./... -count=1 -json, Linux/PG17.11 con BILLING_STABILIZATION_TEST=1 | PASS, 495 casos/subcasos en esa corrida |
| Billing focal race TestDoctorChartEffective / CoverageChanges / GoHTTPContracts / LimitChangesQueue, tras ajuste final de PUT | PASS |
| Billing go test -race ./db -count=1, BASELINE_TEST_ADMIN_DSN habilitada | PASS tras ajustar dos conteos heredados |
| Billing TestStartupBaselineBeforeReadinessAndRejectsDrift, Windows/PG18.6 | PASS sin SKIP tras ajustar tercer conteo heredado |
| Interoperabilidad TestM2CrossRepositoryEffectiveRights → TestActualBillingEffectiveRights | PASS sin SKIP; handlers reales Billing y DoctorChart, catálogo/PG reales de fixture y Supabase sintético |
| build / vet / gofmt / git diff --check, ambos repos | PASS |
| govulncheck ./..., ambos repos | exit0, cero vulnerabilidades alcanzables/importadas; avisos de módulos no alcanzados, dependencias intactas |

Proof focal incluye: ownership ajeno 404; último cupo y versión concurrentes; snapshot offline apto, cobertura/lease vencidos y firma inválida; callback duplicado y reload fallido; fetch atravesado por invalidación; fence de mutación antes de ACK; restore/downgrade, IANA inválida y -1 solo explícito.
Migración2→3 desde estado publicado y fallo SQL/rollback/reintento PASS; baseline1→última con seeds reales y conservación PASS. Startup DoctorChart ejecutado sin SKIP en race, con health503/ping200 y recuperación200.

Reproducción interoperabilidad (solo fixtures propios, no DSN reales), desde repo Billing:
```powershell
$env:BILLING_STABILIZATION_TEST = '1'
$env:BILLING_TEST_DSN = 'postgres://migration_fixture:<fixture-password>@127.0.0.1:55442/postgres?sslmode=disable'
$env:DOCTORCHART_M2_SOURCE = 'C:/Users/gcast/Documents/GitHub/doctorchart-backend-go'
$env:DOCTORCHART_CLINICS_TEST_DATABASE_URL = 'postgres://migration_fixture:<fixture-password>@127.0.0.1:55440/doctorchart_clinics_test?sslmode=disable'
go test ./internal/billing -run '^TestM2CrossRepositoryEffectiveRights$' -count=1 -v
```
El fixture exige ruta local del repo y PostgreSQL propio. No ejecutar en paralelo con otra suite que use la misma base fija clinics.

## Errores encontrados y corregidos
- Comparador de snapshots de datos incluía la nueva tabla epoch: se excluyó solo metadata nueva, sin relajar comparación de tablas clínicas originales.
- Mensaje de key desconocida Billing divergía del contrato legacy; texto preservado sin retirar soporte max_clinics.
- Tests de lease de pagos tomaban nuevas notificaciones: ahora entregan callbacks antes de probar la lease de pago.
- Fixture de pago con proveedor estricto requería usuario remoto sintético; corregido setup.
- Baseline/shared/startup Billing esperaban 17 tablas frente a las 19 publicadas en HEAD; corregidos únicamente tres conteos y ejecutados con DSN explícita.
Todos los fallos relacionados se revalidaron; no se ocultaron ni se aceptaron SKIP como PASS.

## Omisiones y alcance de prueba
DoctorChart omitió en suite genérica los cinco tests externos históricos:
TestLiveConverterLocalStorage, TestLiveConverterOrientationAndSpecialPreservation, TestLiveConverterS3AndApplicationIsolation, TestLiveStudyHTTPConverterAndPersistence, TestLimitsSyncDoctorChartFixture.
TestActualBillingEffectiveRights se omite sin provider fixture; **ejecutado aparte PASS** mediante Billing.
Billing omitió previews manuales, grant/R1 Gateway, límites cross-service legacy y soporte/converter live por infraestructura externa. Baseline/shared/startup inicialmente omitidos se habilitaron y ejecutaron aparte; interoperabilidad nueva también ejecutada.
No se validó contra Supabase/Gateway reales, checkout real o producción. No se declara frontend/E2E navegador ni activación comercial. Paquetes sin tests no equivalen a tests omitidos.

## Riesgos materiales / integración futura
- Configurar cantidades comerciales reales por propietario/tier antes de habilitar esas operaciones; falta no bloqueó desarrollar partes seguras.
- Lease/callback dan revocación acotada, no instantánea antes de entrega de evento; modificaciones manuales externas deben emitir callback.
- Nueva versión de esquema es 3; binario M1 rechaza marcador3. Recuperación real requiere imagen compatible o respaldo/restauración verificada con autorización, no bajar hashes.
- Integración futura: conservar respuesta legacy de Billing; habilitar proveedor nuevo y clave comercial solo tras pruebas/revisión. No hubo cambios de Gateway.
- Argon debe revisar ambos diffs y esta API congelada; no inferir aprobación de su trabajo Angular ni de M3.

## Manifiesto de archivos de código/tests (bytes actuales)
Estas huellas identifican los bytes de código/tests probados y verificados antes de los commits de consolidación, incluidos los archivos que eran nuevos. No cambiar checkout o finales de línea durante la auditoría.
Auditar DoctorChart con `git diff 6d7b0d6 1dd09bc --` y Billing con `git diff bf65c04 80c171c --`; `git show --stat <commit>` muestra el scope cerrado. La documentación posterior no cambia aplicación. Get-FileHash -Algorithm SHA256 verifica los bytes actuales; Git puede normalizar finales de línea en blobs según sus atributos.

| Repo | Archivo | SHA256 |
|---|---|---|
| doctorchart-backend-go | `cmd/api/main_test.go` | `2ed252f0530c208c156ad7cd3be78015f57c6f6298bf76c5db067150bd39def5` |
| doctorchart-backend-go | `cmd/api/main.go` | `fce8b708dda6f526b78a60d512809fe97d2f2cba70bf57e721fbf95270782bff` |
| doctorchart-backend-go | `db/init_test.go` | `a1e08d46aadccf0c89bd311af3a9ab02f5e167c50a20e08b111d828d3d7982a2` |
| doctorchart-backend-go | `db/init.go` | `ee57c025f2d5d0b84acb7c265882327c86ce429d0a606a5d1ed88940b60ff7cf` |
| doctorchart-backend-go | `db/migrations_test.go` | `81ff2782737e34f60ce002af90c4d42b4a10171bda18bed9466d7a580f63edaa` |
| doctorchart-backend-go | `db/migrations.go` | `78aacb6eb2079dd21d4be070c8f37a2b8d4094ae932ed025fa6e30d1c70e4a84` |
| doctorchart-backend-go | `db/migrations/0003_entitlement_epoch.sql` | `06a8d83da55416455a23b7808aa493499cacf1b04b5ea0e69e4d415766561998` |
| doctorchart-backend-go | `internal/api/server_test.go` | `2d146f64a16ac2d155a37f41840542b970153798ffd081fee1a5f8ffa08d739f` |
| doctorchart-backend-go | `internal/api/server.go` | `574cebd042f000c399fcc04ea5c5954c692ed012a75fc130422d9c946b3b4e53` |
| doctorchart-backend-go | `internal/billing/entitlements_test.go` | `b7c33a63795b584a7f7a0f5cbeba8d1021dd7a76b52e9a5ff97020588a6cb47b` |
| doctorchart-backend-go | `internal/billing/entitlements.go` | `74bb662f58baaf151e20b5bc1c0b968ac99ab613d062e5296df8b1cab431a378` |
| doctorchart-backend-go | `internal/billing/rules.go` | `c874f6afe41ca28861cddd874aa484e17d94ad46fa029e5ac9b9a8ee5c62dc91` |
| doctorchart-backend-go | `internal/bootstrap/migration_test.go` | `466b7cb7eea2f12420735de408f6b856c27aef10ebd0bb5a18b4f28d28f6c2c2` |
| doctorchart-backend-go | `internal/bootstrap/schema_test.go` | `ff34e51e5cd8552676b68d3917194dc9e17ae42282c63b9ff53ff1e60cebde4c` |
| doctorchart-backend-go | `internal/clinics/clinics_test.go` | `97a3495413f2997f5ba6f5c8dd3725f6e8284e1fa05811ceea32293912536b2b` |
| doctorchart-backend-go | `internal/clinics/clinics.go` | `c670d37c824899b8772a5fe76a4ec8056fa5740a92527989a129f31a9d4727a5` |
| bycode-backend-go | `cmd/billing/startup_integration_test.go` | `4b2c034e7c4417541aced934713f6afa9c64b67e20295ecca898177ce078eb1a` |
| bycode-backend-go | `db/init_test.go` | `924b27e6812955c048b0408a25af7445c9d4c821be90597fc17b74f1dd1deb27` |
| bycode-backend-go | `db/shared_test.go` | `8342d8fc938cc59027f00e4a2d9200832c0e50c838b5121e1e5588106adb29eb` |
| bycode-backend-go | `internal/billing/admin_grants.go` | `540386b345e535723f79ee69a4c11181d782ef37a4762c70725dc6bca959b159` |
| bycode-backend-go | `internal/billing/catalog.go` | `0d1aa7a99cac5ee04a39404a826b3e30f4fabca1923e7cbd73dfdd2c53ce26b1` |
| bycode-backend-go | `internal/billing/doctorchart_entitlements_test.go` | `0bc9fa6e97419dab5ef558dda9a76e96d92aae6fabcb91b597f215cd5aa54c70` |
| bycode-backend-go | `internal/billing/doctorchart_entitlements.go` | `c2224deb0a3902a8cf6f18d44bcd3a50105065e53d9886dd369e1ceda7c61b45` |
| bycode-backend-go | `internal/billing/http.go` | `ff1eeb93d27aff35972a359b6b99743e06cf7cd77b2371a40078a10d1cb1d05f` |
| bycode-backend-go | `internal/billing/payment_credit.go` | `b71b3092d46cfc9f5918bd5ca3fb541be29fc040900f885111ea1a7a6e9bbfbf` |
| bycode-backend-go | `internal/billing/payment_stabilization_test.go` | `9da885e027fe96f43090051537155a96444a3815f9f583f25e81eb1c17002241` |

Documentación adicional: este informe, contratos/handoff/tablero/tickets/prompts/índice y db/README de DoctorChart; guía Billing docs/doctorchart-entitlements.md. No forman parte del manifiesto de aplicación.
