# Tablero de tareas — versión 0.3
**29/10/2026.** El estado limpio inicial fue preparado intencionalmente por el propietario y verificado en cuatro repositorios. No hay trabajo anterior a recuperar.

| ID | Estado | Responsable | Revisor | Entregable / siguiente paso |
|---|---|---|---|---|
| R0 — Reset documental | DOCUMENTATION_COMMITTED | Coordinación | Propietario | 26 documentos en d5a89e0, por orden expresa; M1 autorizado después |
| B0 — Billing discovery | REPORT_RECEIVED / evidencia histórica | Sol | Argon al verificar contrato | No equivale a integración implementada ni aprobación |
| M1 — Foundation | IMPLEMENTED / READY_FOR_ARGON_REVIEW | Sol | Gemini Argon independiente | 48f87b1: migración 1→2 + clinics; pruebas PASS; aceptación pendiente |
| M2 — Cuotas de lugares | BACKLOG / NO AUTORIZADA | Sol | Argon | Billing y administración protegida por cuotas |
| T0 — Auth staff boundary | BACKLOG / NO AUTORIZADA | Sol | Argon | Contrato Gateway + sesión local Doctor Chart |
| T1 — Scheduling | BACKLOG / NO AUTORIZADA | Sol | Argon | Agenda por lugar, exclusión por médico |
| T2 — Booking | BACKLOG / NO AUTORIZADA | Sol | Argon | Motor común de reservas y formularios |
| T3 — Admin panel | BACKLOG / NO AUTORIZADA | Sol | Argon | Médico y secretaria, permisos diferenciados |
| T4 — Public portal | BACKLOG / NO AUTORIZADA | Sol | Argon | Portal público en `/appointments`; segmentos pendientes |

Una sola fase activa y un escritor. M1 fue autorizado e implementado en `codex/m1-clinics-migrations`; revisión Argon y aceptación pendientes. **No iniciar M2 ni publicar sin aprobación expresa.** Ver [handoff](05_HANDOFF.md) y [brief Argon](reviews/M1_ARGON_BRIEF.md).
