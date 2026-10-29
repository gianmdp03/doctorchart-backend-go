# Tablero — M1 cerrado, M2 preparado
**29/10/2026.** M1 APROBADO por Gemini Argon en `6152867`; cierre documental autorizado. Preparación no equivale a autorización de implementar M2.

| ID | Estado | Implementador / repo | Revisor | Entregable |
|---|---|---|---|---|
| R0 | DOCUMENTATION_COMMITTED | Coordinación / backend | Propietario | 26 documentos en d5a89e0 |
| M1 | CLOSED / ARGON_APPROVED | Sol / backend | Gemini Argon | 6152867; startup sin SKIP, seeds reales PASS; rama pendiente de integración |
| M2-SOL | READY_FOR_APPROVAL / NOT_STARTED | Sol / backend | Gemini Argon | [Ticket](tasks/M2_SOL.md): endpoints, propiedad, IANA, capacidades/cuotas Billing |
| M2-ARGON | READY_FOR_APPROVAL / NOT_STARTED | Gemini Argon / frontend | Sol | [Ticket](tasks/M2_ARGON.md): UI de gestión y mocks del contrato |
| B0 | REPORT_RECEIVED / antecedente | Sol / consumidor Billing | Revisión según alcance | Fuente comercial efectiva todavía por acordar |
| T0 | BACKLOG / NO AUTORIZADA | Backend + frontend según tickets futuros | Cruzada | Autenticación staff |
| T1–T4 | BACKLOG / NO AUTORIZADAS | División futura por repositorio | Cruzada | Horarios, reservas, panel y portal; fuera de M2 |

Contrato único [M2-CLINICS-v0.1](contracts/M2_CLINICS_API.md), todavía propuesto. Cada ticket requiere aprobación expresa; ambos podrán empezar simultáneamente una vez aprobado el contrato y autorizado su alcance.

## Propiedad para la fase futura
| Directorio Git | Escritor asignado | Rama propuesta | CLAIM de implementación |
|---|---|---|---|
| doctorchart-backend-go | Sol | codex/m2-clinics-backend, desde M1 aprobado | NO INICIADO |
| doctorchart-frontend | Gemini Argon | codex/m2-clinics-frontend | NO INICIADO |

No hay agentes de implementación iniciados. Un escritor por Git, incluidos docs y herramientas que escriban; jamás worktrees. Revisiones cruzadas de solo lectura sobre entrega congelada; pruebas del otro repo requieren RELEASE/CLAIM.
M2 integra derechos efectivos y validación IANA Go; sin esos derechos no hay mutaciones comerciales. Sin merge a main, push, despliegue o bases reales.
