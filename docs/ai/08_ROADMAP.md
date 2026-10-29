# Roadmap — lugares administrativos, clínica por médico
**M1 CERRADO / ARGON APROBADO en 6152867. M2 solo preparado para autorización.**

| Fase | Resultado / estado | Dependencia para avanzar |
|---|---|---|
| R0 | Documentación base en d5a89e0 | Completada |
| M1 | Esquema 2 + clinics, commit final 6152867, APROBADO | Integración controlada pendiente; no merge/despliegue |
| M2-SOL | Backend, IANA Go, propiedad, API y capacidades/cuotas | Aprobar contrato/ticket; fuente comercial efectiva para habilitar escrituras |
| M2-ARGON | Frontend de gestión y mocks | Aprobar mismo contrato/ticket; puede avanzar simultáneamente con Sol |
| Integración M2 | Validar API real/UI/derechos y revisión cruzada | Ambos entregan evidencia; propietario autoriza integración/activación |
| T0 | Auth local staff | Contrato propio y permiso; fuera de M2 |
| T1 | Horarios/excepciones por lugar | Sin doble ocupación del médico; fuera de M2 |
| T2 | Reservas/formularios versionados | Motor común; reserva no crea clínica |
| T3–T4 | Panel staff y portal /appointments | Contratos y fases posteriores aprobados |

Ramas backend M2 pueden partir del M1 aprobado sin integrarlo a main; frontend usa su base independiente. Un escritor por Git; revisión cruzada y sin worktrees.
El [contrato único](contracts/M2_CLINICS_API.md) concentra propuestas de cuotas/estados/fallo comercial. Ningún contrato redactado equivale a implementación autorizada.
Sin cantidades por tier inventadas, escrituras por derechos desconocidos ni exposición operativa de mocks. Sin cambios clínicos por sede, pacientes duplicados o consultorio obligatorio.
