# M4 — motor de reservas
30/10/2026. Backend implementado y validado para aceptación del propietario. M5 no autorizado. Ninguna integración a main, publicación ni migración real.

## Versiones y preservación
- Clon normal de trabajo: `C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-backend-go`, rama `codex/m4-bookings`, base M3 `ff5053bbc84e77d9aa19f5fbd0e1b7ecc6869a75`.
- `58acbda`: contrato previo; `5de8106`: motor, esquema y regresiones; `9b42adf`: comprobación M3 entre agendas al admitir reserva y concurrencia directa SQL. Código final probado: `9b42adf`.
- M3 Angular `d0b7a458a7a0c1301cd7e77f35be325aa8d5e6c1` intacto. M1 `6152867`/M2 backend `c3bf46c` son ancestros del backend M3; M2 frontend `471daaf` del frontend M3.
- Las ramas anteriores M3 y turnos no se mueven. Se conserva una referencia local adicional M4 en el repositorio habitual mediante fetch exclusivamente del clon local, sin GitHub remoto ni worktrees.
- Repositorios habituales limpios en main: DoctorChart Go `d5a89e05eebf70de1dca72ed0b01a74467b6b1a4`; Angular `14196974125e5769c5a95e309896397acad96e63`; Billing `bf65c042cc7480cbf4603447becfeb14002dbde1`; Bycode Angular `322bf2313b78d5292876b85a0a911f48c5dd1c3e`; Gateway `55197fa5701d15ed67a69e395bee4e3699c5a705`.

## Contrato y funcionamiento
[M4-BOOKINGS-v1](contracts/M4_BOOKINGS_API.md) congelado antes del código: creación confirmada, listado/detalle/eventos, cambio administrativo y reprogramación. Identidad del Gateway existente, aislamiento por médico; IDs/versiones strings, control optimista, Idempotency-Key obligatorio para todas las mutaciones.

Fence comercial M2 y lock por workspace ordenan reservas y cambios de agenda. M3 calcula los slots; una reserva ocupa disponibilidad en todas las sedes del médico. SQL agrega exclusión GiST de intervalos sin extensión, propiedad de sede/paciente y transiciones válidas. Reserva, evento y recibo idempotente se confirman juntos; errores no consumen clave ni dejan datos parciales.

Nueva migración `0006_bookings.sql`, esquema 6, 30 tablas/28 FK. Baseline y 0002..0005 intactos. Catálogos independientes: PG17 `9e6c01047fe66b6f5db8735d0c141f5da98433c31ab3fb631f110709bce013aa`; PG18 `a5df22e0e9a90537ed33a4db1c5da8bf68751e733d209af342f14058ba56023b`.

## Evidencia de pruebas
Todas las bases son tmpfs descartables, creadas para esta ejecución, PostgreSQL 17/18. Las pruebas Go corrieron dentro de Linux Docker; no se modificó el firewall Windows. Comandos y DSN de ensayo: [db/README.md](../../db/README.md).

| Comprobación | Resultado |
|---|---|
| `go test -p 1 ./... -count=1 -json` con DSN schema/integrity/growth/followups/startup/clinics/entitlements | 512 casos/subcasos PASS; 0 FAIL; 6 externos omitidos |
| Startup contra PostgreSQL, proceso HTTP real | PASS, sin SKIP |
| Suite de reservas contra PostgreSQL 17 | 17 casos/subcasos PASS más configuración opt-in, incluidos en la suite completa |
| `go test -race ./internal/clinics -run '^TestBookings' -count=1` | 17 casos/subcasos PASS; 0 FAIL, sin SKIP ni carreras detectadas |
| `go test ./db -run '^TestBookingMigration' -count=1` en PostgreSQL 18 | 5 casos/subcasos PASS; sin SKIP |
| `go vet ./...` y `go build ./...` | PASS, runner final exit 0 |
| Supabase Auth real de ensayo → Gateway → Go → Billing → PostgreSQL | 10 recorridos PASS |

Concurrencia y seguridad probadas: dos solicitudes al mismo slot y dos al mismo recibo; exclusión directa SQL concurrente entre sedes, omitiendo Go; otro médico puede ocupar el mismo intervalo; cancelaciones y cancelación vs reprogramación concurrentes; versiones obsoletas; cierre de agenda vs reserva; fuera de horario/vacaciones; aislamiento; transiciones inválidas; trigger de evento o recibo fallido revierte toda la operación y admite reintento.

Migración M3→6: datos clínicos/agendas/configuraciones conservados, SQL fallido, drift de catálogo, seed tardío, rollback/reintento, fresh=upgrade, reinicio conservando reservas/eventos/recibos y rechazo de checksum manipulado. No hay DOWN destructivo: el rollback demostrado es el de una transacción fallida. Binarios anteriores rechazan esquema 6.

Integración real: sesiones signup/password emitidas por `supabase/gotrue:v2.197.0` aislado, sin JWT fabricados ni headers de identidad para declarar aprobada autenticación. Billing `80c171c`, Gateway `55197fa`, DoctorChart `9b42adf`; archivos fuente archivados verificados byte a byte contra commits. Cuotas sintéticas BASIC/PRO/UNLIMITED, sin configuración real. Creación concurrente con una sola confirmación, listado, aislamiento, traslado entre sedes/replay, cancelación/liberación, historial, bloqueo de cambios M3 incompatibles, estados y reintento con Billing realmente detenido. Las pruebas unitarias/integradas internas usan los dobles existentes; no se confunden con este recorrido autenticado.

Seis omisiones opt-in, externas a M4: `TestLimitsSyncDoctorChartFixture` (fixture legacy de sincronización); `TestActualBillingEffectiveRights` (servicio explícito opt-in, cubierto por separado en el recorrido real); `TestLiveStudyHTTPConverterAndPersistence`, `TestLiveConverterLocalStorage`, `TestLiveConverterOrientationAndSpecialPreservation`, `TestLiveConverterS3AndApplicationIsolation` (converter/almacenamiento externo no iniciado). No se declaran PASS. Preservación clínica demostrada con pacientes/encuentros poblados y snapshots idénticos, además del upgrade con datos representativos.

Artefactos locales fuera de Git: `C:/Users/gcast/Documents/GitHub/doctorchart-m3/.fixtures/m4/`: `final.jsonl`, `race.jsonl`, `pg18.jsonl`, `vet.log`, `build.log`; subdirectorio `integration/`: `m4-results.json`, `m4-http.json`, `versions-verified.json`. Credenciales sintéticas en archivos privados no versionados; no reutilizarlas ni publicarlas. Al cierre se retiraron únicamente contenedores con task/label propios verificados; datos tmpfs descartados, logs y evidencia conservados.

## Límites y siguiente paso
BOOKINGS_ENABLED=false por defecto, requiere SCHEDULES_ENABLED=true para optar. Solo el fixture los activó. No se modifica Angular ni sus flags, pacientes, historias, precios, planes, cuotas reales, autenticación productiva o migraciones históricas. Las cancelaciones/estados no requieren regularizar un exceso de sedes, pero sí derechos efectivos válidos; nuevas reservas y reprogramación respetan el downgrade M2.

Recibos idempotentes sin expiración automática en M4; planificación de retención queda pendiente. El vínculo opcional a paciente usa FK RESTRICT: una política futura de eliminación/desvinculación exige decisión explícita. Sin edición de contacto, portal /appointments, secretarias, mails, pagos ni calendario M5. Ninguno es bloqueo del motor autorizado.

Próximo paso: aceptación del propietario y autorización independiente para integración/activación o M5. No ejecutar binarios nuevos contra bases reales para probarlos.
