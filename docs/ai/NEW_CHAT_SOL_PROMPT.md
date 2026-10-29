# Inicio de conversación — Sol
Leé `docs/ai/00_START_HERE.md`, `02_DECISIONS.md`, `05_HANDOFF.md`, el ticket vigente y los `AGENTS.md` reales. La documentación canónica está en `doctorchart-backend-go/docs/ai/`.

Pacientes e historias pertenecen al médico. Consultorios organizan lugares, horarios y turnos; no agregar `patients.clinic_id` ni exigir consultorio para usar clínica. Médicos conservan Supabase/Gateway, secretarias tendrán autenticación local Doctor Chart y Bycode/Billing administra planes/límites.
Ruta pública confirmada: `/appointments`, sin alias ni redirecciones; segmentos de médico/consultorio son propuestas pendientes.

El propietario descartó intencionalmente los cambios anteriores y dejó repositorios limpios, verificados en R0. No recuperar ni reconstruir lo descartado. Comprobá Git actual para detectar solo cambios nuevos y preservar trabajo ajeno.

R0 quedó commiteado en `d5a89e0`. El propietario aprobó M1 y sus ensayos descartables; implementación entregada en `48f87b1`, rama `codex/m1-clinics-migrations`. Argon auditó y requirió correcciones. Ya se corrigió el conteo de startup, se ejecutó esa integración sin SKIP, se agregó transición con seeds reales y se registró validación horaria para M2. Suites completas y race pasan.

Para continuar, leé `reviews/M1_CORRECTIONS.md` y el handoff: M1 está listo para aprobación; falta revalidación Argon y aceptación del propietario. La entrega original a Argon es histórica. **No comenzar M2 ni modificar más fases sin aprobación.** Sin bases reales, despliegues, pushes o cambios Supabase por inferencia. Respondé breve; no repitas auditorías históricas.
