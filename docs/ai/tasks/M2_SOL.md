# M2-SOL — backend y capacidades de consultorios
**IMPLEMENTED / READY_FOR_REVIEW. Autorizado por el propietario el 29/10/2026; M3 no autorizado.**
Repo: `doctorchart-backend-go`. Rama: `codex/m2-clinics-backend`, desde `6152867` o descendiente exclusivamente documental aprobado; no desde main sin M1.
Responsable: Sol. Revisor cruzado: Gemini Argon. Contrato único: [M2-CLINICS-v1](../contracts/M2_CLINICS_API.md), congelado; [derechos técnicos](../contracts/M2_EFFECTIVE_ENTITLEMENTS.md).

## Dependencias y propiedad
M1 aprobado por Argon en `6152867`; M2-SOL autorizado. Entrega inicial 1dd09bc y corrección autorizada 594402e (esquema 4); Billing 80c171c intacto. [Correcciones y coordinación timeZone](../reviews/M2_SOL_CORRECTIONS.md) para revalidación independiente. CLAIM de escritura de aplicación liberado según [protocolo](../06_PROTOCOL.md); no worktrees.
Ámbito: nuevo módulo `internal/clinics/`, integración mínima en `cmd/api/main.go`, consumidor `internal/billing/`, wiring API si hace falta y pruebas. Coordinación canónica `docs/ai/` a cargo de Sol durante su CLAIM.
Propietario autorizó cambios acotados de Billing/Gateway. Se implementó proveedor firmado y compatibilidad/outbox en rama Billing propia; Gateway no necesitó cambios. Sin configuración de cantidades comerciales reales.

## Alcance
- DTO/handlers/store de listado, detalle, alta, edición y estado según contrato; propiedad exclusiva por médico con autenticación existente.
- Validar IANA en Go y coherencia con CHECK M1; tipos BIGINT sin pérdida en JSON; optimistic locking.
- Capabilities, proveedor comercial sin defaults y escrituras cerradas hasta derechos efectivos. Con proveedor no integrado, listado local y capabilities UNAVAILABLE; no altas ilimitadas.
- Cuotas transaccionales por workspace, restauración/downgrade/estados según contrato aprobado; preservar registros.
- Ampliar compatibilidad del consumidor Billing sin alterar comportamiento de las cuotas clínicas. Tests con proveedor sintético del contrato; acreditar integración real por separado antes de habilitar operaciones.
DDL: migración 3 agrega epoch para invalidación compartida; corrección 4 restringe zonas no regionales con preflight/revalidación/rollback. Baseline y SQL 0002/0003 publicados intactos; catálogo clínico sin cambios y production=true preservado.

## Exclusiones
Angular, cambios de autenticación Supabase, nuevos planes/cantidades, programación de horarios, reservas, staff, portal público, aislamiento clínico por sede, migraciones reales y despliegue. Sin merge/push/commits no autorizados.
No modificar el repo frontend, ni siquiera su documentación, mientras Argon lo tenga reclamado.

## Pruebas y aceptación
- [x] Contrato/ticket autorizados y CLAIM backend/Billing registrado.
- [x] Tests HTTP validan DTO/códigos exactos, rol, propiedad cruzada 404, versiones y zona horaria; queries siempre por propietario.
- [x] PostgreSQL propio descartable autorizado: dos altas sobre último cupo admiten una; edición/estados concurrentes, restauración y downgrade; rollback sin efectos ante rechazo.
- [x] Billing caído con snapshot apto permite operar; sin copia apta/vigente/no invalidada cero escrituras/503; denegación 403. Cuotas 0/-1 solo explícitas.
- [x] Regresiones de cuotas clínicas, startup sin SKIP, bootstrap, clínica, build/vet/race pertinentes; registrar omitidos.
- [x] Handlers reales Billing/DoctorChart probados juntos con datos sintéticos; max_clinics real sin configurar bloquea sus mutaciones. No afirmar activación productiva.
- [ ] Gemini Argon revisó diff congelado en solo lectura; propietario acepta entrega.

## Entrega
SHA/base/rama o diff identificable, archivos, pruebas y resultados, proveedor real vs simulado, limitaciones y riesgos. Sol actualiza handoff/tablero backend con evidencia propia y el resumen entregado por Argon. El reviewer no aplica cambios al repo del autor; para ejecutar pruebas que escriban allí necesita RELEASE/CLAIM temporal.

Evidencia, archivos y hashes en [entrega M2-SOL](../reviews/M2_SOL_DELIVERY.md). Sin M3, merge/push/despliegue o migraciones reales.
