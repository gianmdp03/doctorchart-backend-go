# M2-SOL — backend y capacidades de consultorios
**READY_FOR_APPROVAL / NO IMPLEMENTAR TODAVÍA.**
Repo: `doctorchart-backend-go`. Rama propuesta: `codex/m2-clinics-backend`, desde `6152867` o descendiente exclusivamente documental aprobado; no desde main sin M1.
Responsable: Sol. Revisor cruzado: Gemini Argon. Contrato único: [M2-CLINICS-v0.1](../contracts/M2_CLINICS_API.md), aún propuesto.

## Dependencias y propiedad
M1 aprobado por Argon en `6152867`; contrato y ticket requieren aprobación antes de comenzar. CLAIM exclusivo del repo backend según [protocolo](../06_PROTOCOL.md); no worktrees.
Ámbito: nuevo módulo `internal/clinics/`, integración mínima en `cmd/api/main.go`, consumidor `internal/billing/`, wiring API si hace falta y pruebas. Coordinación canónica `docs/ai/` a cargo de Sol durante su CLAIM.
Billing externo es dependencia: acordar ruta/autenticación, revisión/vigencia, feature y max_clinics efectivos por médico. Si exige cambiar Bycode, presentar ese alcance para aprobación y reclamar ese repo separadamente; no asumir autorización ni disponibilidad.

## Alcance
- DTO/handlers/store de listado, detalle, alta, edición y estado según contrato; propiedad exclusiva por médico con autenticación existente.
- Validar IANA en Go y coherencia con CHECK M1; tipos BIGINT sin pérdida en JSON; optimistic locking.
- Capabilities, proveedor comercial sin defaults y escrituras cerradas hasta derechos efectivos. Con proveedor no integrado, listado local y capabilities UNAVAILABLE; no altas ilimitadas.
- Cuotas transaccionales por workspace, restauración/downgrade/estados según contrato aprobado; preservar registros.
- Ampliar compatibilidad del consumidor Billing sin alterar comportamiento de las cuotas clínicas. Tests con proveedor sintético del contrato; acreditar integración real por separado antes de habilitar operaciones.
No se prevé DDL adicional. Si surge necesidad real, elevar alcance y respetar production=true/baseline inmutable.

## Exclusiones
Angular, cambios Supabase/Gateway, nuevos planes/cantidades, programación de horarios, reservas, staff, portal público, aislamiento clínico por sede, migraciones reales y despliegue. Sin merge/push/commits no autorizados.
No modificar el repo frontend, ni siquiera su documentación, mientras Argon lo tenga reclamado.

## Pruebas y aceptación
- [ ] Contrato/ticket aprobados y CLAIM backend registrado.
- [ ] Tests HTTP validan DTO/códigos exactos, rol, propiedad cruzada 404, versiones y zona horaria; queries siempre por propietario.
- [ ] PostgreSQL propio descartable autorizado: dos altas sobre último cupo admiten una; edición/estados concurrentes, restauración y downgrade; rollback sin efectos ante rechazo.
- [ ] Billing ausente/caído/timeout/vencido/clave faltante: cero mutaciones, capabilities false y 503; denegación verificada 403; cuotas 0 y -1 solo desde fixtures explícitos.
- [ ] Regresiones de cuotas clínicas, startup sin SKIP, bootstrap, clínica, build/vet/race pertinentes; registrar omitidos.
- [ ] Contrato externo real no confirmado mantiene proveedor operativo apagado; reportar esa limitación, nunca declarar integración comercial completa con mocks.
- [ ] Gemini Argon revisó diff congelado en solo lectura; propietario acepta entrega.

## Entrega
SHA/base/rama o diff identificable, archivos, pruebas y resultados, proveedor real vs simulado, limitaciones y riesgos. Sol actualiza handoff/tablero backend con evidencia propia y el resumen entregado por Argon. El reviewer no aplica cambios al repo del autor; para ejecutar pruebas que escriban allí necesita RELEASE/CLAIM temporal.
