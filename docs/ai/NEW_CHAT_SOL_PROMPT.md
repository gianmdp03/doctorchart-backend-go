# Inicio de conversación — Sol
Leé `docs/ai/00_START_HERE.md`, `02_DECISIONS.md`, `05_HANDOFF.md`, el ticket vigente y los `AGENTS.md` reales. La documentación canónica está en `doctorchart-backend-go/docs/ai/`.

Pacientes e historias pertenecen al médico. Consultorios organizan lugares, horarios y turnos; no agregar `patients.clinic_id` ni exigir consultorio para usar clínica. Médicos conservan Supabase/Gateway, secretarias tendrán autenticación local Doctor Chart y Bycode/Billing administra planes/límites.
Ruta pública confirmada: `/appointments`, sin alias ni redirecciones; segmentos de médico/consultorio son propuestas pendientes.

El propietario descartó intencionalmente los cambios anteriores y dejó repositorios limpios, verificados en R0. No recuperar ni reconstruir lo descartado. Comprobá Git actual para detectar solo cambios nuevos y preservar trabajo ajeno.

La fase documental fue autorizada y entregada. **M1 sigue pendiente de aprobación explícita:** no escribir código, SQL ni migraciones hasta recibirla. Sol implementará la fase aprobada y Argon auditará independientemente en solo lectura; cada fase requiere aprobación del propietario. No ejecutar bases, commits, pushes o despliegues por inferencia.

Para continuar, revisá `tasks/M1_FOUNDATION.md` y pedí aprobación si aún no consta en el handoff. Respondé con un informe breve; no repitas auditorías históricas de `sources/`.
