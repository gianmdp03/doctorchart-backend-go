# Doctor Chart — continuidad breve

31/10/2026. M1–M6 aprobados; M7 secretarias implementado y validado en ensayo, pendiente de aceptación. M8 no autorizado. Ramas codex/m7-secretaries derivadas de los M6 aprobados, en clones normales doctorchart-m3. Nunca trabajar en main ni usar worktrees.

Leer 05_HANDOFF.md y M7_DELIVERY.md para versiones, pruebas y límites. Contrato: contracts/M7_SECRETARIES_API.md. No hace falta leer auditorías históricas para continuar.

Pacientes e historias pertenecen al médico; no clinic_id ni acceso clínico para secretarias. Médico usa Supabase/Gateway; secretaria usa cuenta local Go y permisos por relación/sede. M4 sigue siendo el único motor de reservas. Capacidades comerciales independientes de propietario, BASIC/PRO/UNLIMITED, sin valores por defecto. /appointments sigue como ruta pública.

Flags desactivados en aplicación normal. Sin producción, bases reales, GitHub remoto, push, merge o despliegues. Conservar turnos y ramas anteriores. No alterar AGENTS.md.
