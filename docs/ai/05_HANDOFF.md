# Handoff M4 — motor de reservas
30/10/2026. M3 aceptado; M4 implementado en codex/m4-bookings desde ff5053b. Un único agente, backend/PostgreSQL; Angular M3 d0b7a45 sin cambios. M5 no autorizado.

Contrato [M4-BOOKINGS-v1](contracts/M4_BOOKINGS_API.md); resultados, commits y límites en [M4_DELIVERY.md](M4_DELIVERY.md). El informe M3 es antecedente, no instrucción vigente de detener M4.

Commits: 58acbda contrato; 5de8106 motor/migración/pruebas; 9b42adf integración de la comprobación entre agendas M3 y prueba concurrente SQL. Cierre documental posterior visible en HEAD. Pruebas finales ejecutadas sobre este código.

Esquema 6: bookings, booking_events, booking_idempotency; exclusión GiST del intervalo del médico sin extensión nueva. Escrituras serializadas por workspace con fence comercial, validación M3, versión y evento/recibo en una transacción. No se modifican pacientes ni tablas clínicas. Nueva FK opcional RESTRICT protege el vínculo histórico; borrar un paciente vinculado requerirá una política explícita futura.

Go/PG descartables y recorrido Supabase Auth real → Gateway → Doctor Chart → Billing → PostgreSQL: consultar detalle en entrega. Entorno Linux Docker para evitar prompts de firewall de binarios Go temporales; no se alteró el firewall. Las credenciales sintéticas quedan fuera de Git.

Checkouts habituales main intactos; ramas turnos y M3 preservadas, sin worktrees ni merges. Billing 80c171c y Gateway 55197fa se reutilizan sin cambios. Flags normales desactivados; no bases reales, Cloudflare, GitHub remoto, push o despliegue.

Para retomar: usar esta rama y contrato, repetir únicamente comprobaciones afectadas; la aprobación del propietario precede a integración/activación o M5. No iniciar UI pública, secretarias, pagos ni notificaciones.
