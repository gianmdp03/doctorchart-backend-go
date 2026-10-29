# Decisiones pendientes — alcance mínimo
**M1 CERRADO:** Argon APROBADO sobre `6152867`; propietario autoriza cierre documental. M2 solo preparado.

## Bloqueos reales para M2
1. **Aprobar contrato público y tickets.** [M2-CLINICS-v0.1](contracts/M2_CLINICS_API.md) es propuesta, incluida semántica de estados/cupo, downgrade y bloqueo de escrituras ante caída Billing. No se inventan cantidades por plan.
2. **Acordar fuente comercial efectiva por médico:** ruta/autenticación de consulta a Bycode/Billing, revisión, vigencia, habilitación de gestión y `max_clinics`; definir coherencia de revisión/revocación antes de escrituras reales. Los campos mínimos están en el único contrato, no se duplican aquí.
3. **Compatibilidad del consumidor:** `internal/billing/rules.go` hoy rechaza cualquier clave distinta de `max_patients` y `max_records_per_patient`. Preparar soporte seguro sin romper cuotas clínicas; no publicar claves a clientes incompatibles ni alterar Bycode por inferencia.

Tras aprobar contrato/tickets, UI puede avanzar con mocks y backend con proveedor sintético/estado cerrado. La falta del proveedor comercial bloquea habilitar operaciones reales, no exige fingir que la integración existe. Rutas/nav operativas seguirán apagadas; API bloquea todas las mutaciones sin derechos efectivos.
Ramas propuestas pueden partir del M1 aprobado en backend y base frontend independiente; no es necesario hacer merge a main para desarrollar. Integración de M1 continúa pendiente de autorización.

## Fases posteriores (fuera de M2)
- Staff: contrato local de invitación/sesión/revocación/Gateway y permisos, conservando Supabase médico.
- Horarios/reservas: reglas de intervalos, zonas, excepciones, exclusión por médico e identidad solicitante/atendido.
- Público: `/appointments` confirmado sin alias; segmentos de médico/consultorio y publicación pendientes.
- Sin traslado, duplicación ni pertenencia clínica por lugar. No exigir consultorio para clínica; metadato de lugar en consulta sería otra decisión futura.
