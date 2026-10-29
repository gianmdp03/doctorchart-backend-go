# Prompts breves de reinicio por agente

## Sol — arranque de conversación nueva
Usá `NEW_CHAT_SOL_PROMPT.md`. R0 ya verificó el estado limpio preparado intencionalmente por el propietario; no recuperar ni reconstruir cambios descartados. Comprobar Git actual para preservar solo cambios nuevos. M1 necesita aprobación explícita.

## Sol — comienzo de una fase de código ya aprobada
"Leé `docs/ai/00_START_HERE.md`, `02_DECISIONS.md`, `05_HANDOFF.md`, el ticket aprobado y `AGENTS.md` real. Confirmá repo/branch/commit y alcance exacto. Trabajá solo la fase aprobada, ejecutá pruebas pertinentes en entornos descartables autorizados, actualizá handoff y devolvé diffs/SHAs/resultados. No recuperes decisiones anuladas de auditorías históricas."

## Sol — cierre de una sesión
"Antes de terminar, actualizá `05_HANDOFF.md` y `04_TASK_BOARD.md`. Registrá los commits reales, archivos cambiados, pruebas ejecutadas y omitidas, fallos, decisiones aprobadas, riesgos y próxima acción explícita. Ninguna operación destructiva sin permiso." 

## Argon — revisión independiente
"Leé `docs/ai/00_START_HERE.md`, `02_DECISIONS.md`, `05_HANDOFF.md`, `06_PROTOCOL.md` y el ticket en revisión. Pacientes e historias pertenecen al médico; los consultorios **solo** organizan lugares y turnos. Auditá el diff y SHA que Sol entregue, no su resumen. No modifiques archivos. Señalá P0/P1/P2 con archivo/línea, evidencia y reproducción; marcá NO VERIFICADO si corresponde."

## ChatGPT — coordinación en chat nuevo
"Estamos desarrollando Doctor Chart con Sol arquitecto/implementador y Argon auditor. Leé los archivos `00_START_HERE.md`, `02_DECISIONS.md`, `05_HANDOFF.md` y tablero que adjunto; son la única referencia actual de producto. **No** propongas `patients.clinic_id`: la historia clínica es única por médico, los lugares de atención son solo para turnos. Ayudame a revisar entregas y redactar el próximo ticket; no presupongas que Sol completó acciones del chat anterior."

Ruta pública confirmada: `/appointments`, sin alias ni redirecciones para esta función nueva. Segmentos de médico/consultorio pendientes del contrato; no elevar ejemplos históricos a decisiones.
