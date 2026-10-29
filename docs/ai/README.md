# Índice de coordinación — Doctor Chart v0.3
Instalación documental autorizada en `d5a89e0` (26 archivos). M1 cerrado con revalidación Argon APROBADO en `6152867`; rama pendiente de integración controlada. M2-SOL implementado para revisión, Angular reservado a Argon y M3 no autorizado. No modifica ni sustituye `AGENTS.md`.

Para una conversación nueva basta comenzar con [inicio breve](00_START_HERE.md), [decisiones](02_DECISIONS.md) y [handoff](05_HANDOFF.md); después leer el ticket activo. No releer auditorías históricas automáticamente.

## Inventario de archivos instalados
### Coordinación
- [00_START_HERE.md](00_START_HERE.md)
- [01_REQUIREMENTS.md](01_REQUIREMENTS.md)
- [02_DECISIONS.md](02_DECISIONS.md)
- [03_PENDING_CONTRACTS.md](03_PENDING_CONTRACTS.md)
- [04_TASK_BOARD.md](04_TASK_BOARD.md)
- [05_HANDOFF.md](05_HANDOFF.md)
- [06_PROTOCOL.md](06_PROTOCOL.md)
- [07_SESSION_STARTERS.md](07_SESSION_STARTERS.md)
- [08_ROADMAP.md](08_ROADMAP.md)
- [09_TEST_MATRIX.md](09_TEST_MATRIX.md)
- [10_REPOSITORY_MAP.md](10_REPOSITORY_MAP.md)
- [NEW_CHAT_SOL_PROMPT.md](NEW_CHAT_SOL_PROMPT.md)
- Este `README.md`, índice agregado al paquete.

### Tickets
- [tasks/R0_CONTEXT_RESET.md](tasks/R0_CONTEXT_RESET.md) — entrega documental commiteada por instrucción expresa.
- [tasks/M1_FOUNDATION.md](tasks/M1_FOUNDATION.md) — cerrado, APROBADO en 6152867.
- [tasks/B0_BILLING_DISCOVERY.md](tasks/B0_BILLING_DISCOVERY.md) — antecedente reportado, sin integración verificada.
- [tasks/T0_STAFF_BOUNDARY.md](tasks/T0_STAFF_BOUNDARY.md) — backlog.
- [tasks/A0_ARCHITECTURE_CONSOLIDATION.md](tasks/A0_ARCHITECTURE_CONSOLIDATION.md) — cancelado, solo trazabilidad.

### M2-SOL entregado
- [contracts/M2_CLINICS_API.md](contracts/M2_CLINICS_API.md) — API v1 congelada (hash en handoff).
- [contracts/M2_EFFECTIVE_ENTITLEMENTS.md](contracts/M2_EFFECTIVE_ENTITLEMENTS.md) — fuente comercial, firma, snapshot e invalidación.
- [reviews/M2_SOL_DELIVERY.md](reviews/M2_SOL_DELIVERY.md) — pruebas y hashes para revisar diffs.
- [reviews/M2_ARGON_CROSS_REVIEW.md](reviews/M2_ARGON_CROSS_REVIEW.md) — revisión de ea4f4d7 en solo lectura; correcciones pendientes.
- [tasks/M2_SOL.md](tasks/M2_SOL.md) — backend y capacidades.
- [tasks/M2_ARGON.md](tasks/M2_ARGON.md) — frontend y mocks.
- [07_SESSION_STARTERS.md](07_SESSION_STARTERS.md) — dos prompts, sin duplicar contrato.

### Revisión M1
- [reviews/M1_ARGON_BRIEF.md](reviews/M1_ARGON_BRIEF.md) — entrega original histórica previa a la auditoría.
- [reviews/M1_CORRECTIONS.md](reviews/M1_CORRECTIONS.md) — hallazgos atendidos, pruebas ejecutadas/omitidas y cierre APROBADO.

### Plantillas
- [templates/TASK_TEMPLATE.md](templates/TASK_TEMPLATE.md)
- [templates/REVIEW_TEMPLATE.md](templates/REVIEW_TEMPLATE.md)

### Antecedentes
- [sources/README.md](sources/README.md) — restricciones de uso de históricos.
- [sources/audit-01-original.md](sources/audit-01-original.md)
- [sources/audit-02-clinics-corrected.md](sources/audit-02-clinics-corrected.md)
- [sources/audit-03-staff-auth.md](sources/audit-03-staff-auth.md)
- [sources/audit-04-billing-entitlements.md](sources/audit-04-billing-entitlements.md)
- [sources/OBSOLETE_NEXT_PROMPT_SOL_V02.md](sources/OBSOLETE_NEXT_PROMPT_SOL_V02.md) — no ejecutar.

## Límite de la instalación
Se incorporaron solo `docs/ai/` del paquete y este índice, con correcciones de coordinación. No se instalaron anexos de `repo-instructions/`, no se copió el README externo con instrucciones obsoletas de recuperación/commit ni el manifiesto de hashes del paquete original, que no describe esta revisión.
