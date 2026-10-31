# Handoff M3 — implementación validada
30/10/2026. M3 completo y validado; pendiente aceptación del propietario. Un único agente. M4 NO autorizado.
Leer [entrega, pruebas, límites y evidencia](M3_DELIVERY.md) y [contrato congelado](contracts/M3_AVAILABILITY_API.md). No repetir auditorías históricas.

Rama codex/m3-availability en clones normales C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-backend-go y doctorchart-frontend; sin worktrees. Checkouts habituales en main intactos. turnos aprobado permanece c3bf46c/471daaf; Billing 80c171c y Gateway 55197fa sin cambios.
Esquema 5 mediante 0005 exclusivamente; baseline y migraciones históricas intactos. fresh/upgrade/rollback/preservación PASS en PostgreSQL17/18. Docker disponible y pruebas ejecutadas dentro de Linux para evitar ejecutables temporales Windows/firewall. Helper de puertos de prueba corregido; no se cambió firewall.
Go suite 488 PASS + crecimiento PG separado PASS, M3 -race 17 PASS, upgrade PG18 5 PASS. Angular 732 PASS, focal final 12 PASS; builds y recorrido autenticado real PASS. Seis pruebas externas omitidas explícitamente en entrega.

SCHEDULES_ENABLED=false en frontend y por defecto backend; CLINICS_ENABLED=false. Sin reservas, pacientes automáticos, nuevas cuotas, pushes, merge a main, Cloudflare, bases reales ni despliegues.
La activación/publicación requiere autorización independiente y revisar el deep link de hosting. No iniciar M4. Los hashes finales se encuentran en HEAD y en las ramas M3 de los repositorios habituales, sincronizadas por fetch local.
