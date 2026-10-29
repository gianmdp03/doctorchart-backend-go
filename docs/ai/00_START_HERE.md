# Doctor Chart — inicio breve
**Versión 0.3 · 29/10/2026 · M1 cerrado; M2-SOL implementado para revisión. M3 no autorizado.**

## Producto confirmado
- Pacientes e historias clínicas pertenecen al médico. Un mismo paciente conserva su historia al atenderse en distintos consultorios.
- Consultorios = lugares de atención para ubicaciones, horarios y turnos. No agregar `patients.clinic_id`, `clinic_patients`, copias ni filtros clínicos por sede.
- Se puede usar la clínica y completar el wizard sin crear un consultorio. Un lugar es necesario solo para configurar agenda/reservas.
- Médicos conservan Supabase/Gateway. Secretarias tendrán autenticación local en Doctor Chart y acceso administrativo, sin historia clínica.
- Bycode/Billing administra planes y límites; Doctor Chart los aplica sin inventar cantidades por tier.
- Ruta pública confirmada: `/appointments`, sin alias ni redirecciones de compatibilidad para esta funcionalidad nueva. Segmentos de médico y consultorio: propuestas hasta aprobar el contrato de rutas.
- Agendas por lugar, sin reservas simultáneas incompatibles para el mismo médico. Reservar no crea paciente ni historia.

## Estado real
El propietario descartó intencionalmente el trabajo anterior y preparó repositorios limpios. Bases iniciales verificadas: frontend `1419697`, backend `c2c30c1`, Gateway `55197fa`, Billing `bf65c04`. **No recuperar ni reconstruir lo descartado.**
M1 final `6152867`, aprobado por Argon y cierre documental autorizado por el propietario. Startup sin SKIP y upgrade con seeds reales PASS; cinco pruebas externas omitidas justificadamente, no bloqueantes. Rama `codex/m1-clinics-migrations` pendiente de integración controlada; sin cambios en producción.

## Próximo paso
Revisar [entrega M2-SOL](reviews/M2_SOL_DELIVERY.md): DoctorChart 1dd09bc y Billing 80c171c congelados para Argon. Frontend ea4f4d7 auditado por Sol: [tres P2 y una observación P3](reviews/M2_ARGON_CROSS_REVIEW.md), sin modificarlo. API M2-CLINICS-v1 congelada; correcciones, aceptación e integración pendientes. [Prompts](07_SESSION_STARTERS.md) y [handoff](05_HANDOFF.md) permiten reiniciar sin auditorías históricas.
Desde M2: Sol backend/BD/Billing; Gemini Argon frontend Angular; revisión cruzada. **Un escritor por directorio Git, sin worktrees bajo ninguna circunstancia.** Ver [protocolo](06_PROTOCOL.md).
M2-SOL autorizado y entregado; Angular reservado a Argon. Sin merge, push, despliegue o bases reales. Snapshot efectivo apto permite operar con Billing caído; sin él no hay defaults. Configuración real de max_clinics pendiente. Los `AGENTS.md` reales siguen vigentes; backend `production: true`.
