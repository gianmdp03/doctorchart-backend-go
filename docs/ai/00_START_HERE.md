# Doctor Chart — continuidad breve

31/10/2026. M1–M7 aprobados; M8 notificaciones y recuperación implementado y validado en ensayo, pendiente de aceptación. M9 no autorizado. Ramas codex/m8-notifications desde M7, clones normales doctorchart-m3. Main nunca es rama de turnos, sin worktrees.

Leer 05_HANDOFF.md y M8_DELIVERY.md. Contrato: contracts/M8_NOTIFICATIONS_API.md. No hace falta releer auditorías históricas.

Pacientes/historias pertenecen al médico, sin clinic_id ni acceso clínico de secretarias. Médico Supabase, secretaria localGo. M4 único motor de reservas; outboxPostgreSQL envía correos después del commit. /appointments/confirmation permite consulta con código privado sin cuenta ni PII en URL.

Flags desactivados en aplicación normal. Sin producción, bases reales, remotos, pushes, merges o despliegues. Conservar turnos/M1–M7 y commits ajenos89f032c/8ac5b14 deBycodefrontend. No alterarAGENTS.md. Editor comercial pendiente para M9, no autorizado.
