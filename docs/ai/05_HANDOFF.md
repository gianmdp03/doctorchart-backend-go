# Handoff — M1 cerrado y M2 listo para autorización
**29/10/2026 (Argentina) · trabajo actual exclusivamente documental.**

## Cierre M1
El propietario comunicó revalidación final de Gemini Argon: **APROBADO sobre `615286720f98768dfbc85bcbe35d315e41405e87`** y autorizó cerrar documentalmente M1.
- Startup `TestStartupCreatesSchemaAndServesHealth`: PASS ejecutado sin SKIP.
- Upgrade `TestBaselineUpgradeWithRealSeedsPreservesAdminConfiguration`: PASS con `bootstrap.Run`, seeds reales y configuración administrada preservada.
- Suite Windows/PostgreSQL 18.6 y race Linux/PostgreSQL 17.11: PASS. En race, 395 casos/subcasos aprobados y cinco pruebas externas omitidas, justificadas y no bloqueantes para M1.
- Omitidos: tres tests live de converter en storage, `TestLiveStudyHTTPConverterAndPersistence` y `TestLimitsSyncDoctorChartFixture`; faltan sus fixtures externos, fuera del alcance M1. Nombres y evidencia en [registro](reviews/M1_CORRECTIONS.md).
- Sin cambios en producción. No se repitieron auditorías, tests ni migraciones para este cierre documental.

## Git real
| Repo | Rama / HEAD observado | Estado al iniciar preparación |
|---|---|---|
| doctorchart-backend-go | codex/m1-clinics-migrations / 615286720f98768dfbc85bcbe35d315e41405e87 | Limpio |
| doctorchart-frontend | main / 14196974125e5769c5a95e309896397acad96e63 | Limpio, solo inspección de convenciones |
Backend main permanece en `d5a89e05eebf70de1dca72ed0b01a74467b6b1a4`. **La rama M1 está pendiente de integración controlada**; no hubo merge, push o despliegue.
Esta preparación deja únicamente cambios documentales sin stage/commit en `backend/docs/ai/`; HEAD de aplicación continúa siendo el M1 aprobado. No se cambian AGENTS ni código. Las ramas M2 todavía son propuestas, no fueron creadas.

## Entrega M2, sin implementación
- Único [contrato API](contracts/M2_CLINICS_API.md), revisión propuesta `M2-CLINICS-v0.1`.
- [M2-SOL](tasks/M2_SOL.md): backend Go, propiedad/IANA, consumidor Billing, autorización y cuotas.
- [M2-ARGON](tasks/M2_ARGON.md): frontend Angular, UI/mocks y pruebas; no depende de que el backend termine.
- [Protocolo](06_PROTOCOL.md): trabajo simultáneo por repos distintos; jamás worktrees; escritor exclusivo por directorio Git, revisión cruzada sobre SHA/diff congelado y ventanas RELEASE/CLAIM para ejecutar pruebas en repo ajeno.
- [Dos prompts](07_SESSION_STARTERS.md): contexto mínimo por agente, con autorización todavía pendiente.
Solo tres archivos nuevos (contrato y dos tickets); se reutilizan índice, prompts, handoff y registros existentes. El contrato no se duplica en frontend.
La preparación se validó con enlaces Markdown, consistencia de estado y diff limitado a documentación; no representa implementación ni aceptación del contrato.

## Aprobaciones pendientes y riesgos concretos
1. Aprobar contrato/tickets y políticas propuestas de cupo, estados, downgrade y caída comercial.
2. Acordar proveedor efectivo Bycode/Billing por médico (consulta autenticada, revisión/vigencia, habilitación y max_clinics). Sin él todas las mutaciones permanecen cerradas.
3. Consumidor actual rechaza max_clinics; ampliar compatibilidad sin romper cuotas clínicas antes de publicar nuevas claves. Cambios externos necesarios se presentan para aprobación, no se asumen.
Esto no impide preparar UI simulada o backend cerrado tras la aprobación de desarrollo. No declarar derechos reales con mocks ni permitir cupos ilimitados por ausencia/error de Billing.

## Reglas que permanecen
Pacientes/historias por médico, sin `patients.clinic_id`, separación por sede o consultorio obligatorio. Consultorios solo lugares/horarios/turnos; M2 no implementa horarios ni reservas.
Autenticación médica Supabase/Gateway intacta; secretarias locales en fase posterior. Billing es autoridad comercial sin cantidades por tier inventadas.
Ruta pública futura `/appointments`, sin alias; segmentos aún por aprobar. Rutas de gestión `/clinics` son propuestas M2.
Backend production=true, baseline inmutable y políticas reales AGENTS vigentes. La recuperación/despliegue de M1 mantiene el contrato documentado en db/README; no se ejecutó.

## Próxima acción autorizable
El propietario puede aprobar el contrato y habilitar ambos tickets simultáneamente, indicando si alguna dependencia comercial queda limitada a simulación. Después se registran CLAIM, ramas y snapshot del contrato aprobado; **no iniciar antes**.
Integración M1, commits adicionales, merge, push, bases reales y despliegues requieren autorización específica. La coordinación documental del backend será de Sol mientras lo tenga reclamado; Argon entrega evidencia desde frontend sin editar el otro Git.
