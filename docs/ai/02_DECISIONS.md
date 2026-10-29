# Decisiones arquitectónicas — autoridad vigente
Fecha de producto y actualización documental: 29/10/2026. Estado = **CONFIRMADA** cuando la indicó el propietario; **PROPUESTA** para ingeniería no aprobada; **ANULADA** cuando fue sustituida. La entrada nueva prevalece sobre recomendaciones antiguas de informes históricos.

## Confirmadas (producto)
| ID | Estado | Decisión |
|---|---|---|
| DC-030 | **CONFIRMADA · NUEVA** | **Pacientes, historias, recetas, estudios y consultas pertenecen al médico, NO a consultorios; una sola historia acompaña al paciente en todos los lugares de atención.** |
| DC-031 | **CONFIRMADA · NUEVA** | **Consultorios son lugares físicos/administrativos para agenda y turnos**; no separan cartera clínica, dashboard ni borradores clínicos. |
| DC-032 | **CONFIRMADA · NUEVA** | **No se exige consultorio tras el wizard para usar clínica**; sí uno para configurar y habilitar turnos. |
| DC-033 | CONFIRMADA | Login de médicos por Supabase/Gateway compartido con Bycode y Pointly permanece. |
| DC-034 | CONFIRMADA | Secretarias usan login local en Doctor Chart y panel exclusivo administrativo sin historia clínica. Médico siempre puede gestionar agenda. |
| DC-035 | CONFIRMADA | Cada lugar tiene horarios/excepciones; prohibir reservas incompatibles para mismo médico incluso en dos lugares. |
| DC-036 | CONFIRMADA | Reserva pública confirmada automáticamente, cancelable por médico/secretaria; solicitante diferente del atendido; no crea paciente ni historia. |
| DC-037 | CONFIRMADA | Formulario público con datos mínimos del solicitante (nombre, documento, teléfono, email) y extras configurables; obra social opcional. |
| DC-038 | CONFIRMADA | Directorio público por zona, enlace individual por médico, selección de lugar, pago presencial sin gateway de cobro V1. |
| DC-039 | CONFIRMADA | Bycode/Billing define límites de plan, Doctor Chart los aplica; no duplicar reglas comerciales ni fijar cantidades de tiers en código. |
| DC-040 | CONFIRMADA | Diseñar actualización conservadora e instalación limpia; **ninguna eliminación de datos** sin permiso explícito. |
| DC-041 | CONFIRMADA · ACTUALIZADA | Desde M2, Sol implementa backend/BD/Billing y Gemini Argon frontend Angular simultáneamente en repositorios distintos; revisión cruzada independiente de cada entrega y aprobación humana por fase. En M1 Sol implementó y Argon auditó. |
| DC-051 | CONFIRMADA · CORRECCIÓN DEFINITIVA | Ruta pública de turnos `/appointments`, en inglés. Sin alias ni redirecciones de compatibilidad porque la función no fue lanzada. Sustituye cualquier alternativa anterior. |
| DC-052 | CONFIRMADA | El propietario descartó intencionalmente el trabajo anterior y preparó repositorios limpios. No recuperar ni reconstruir cambios de la arquitectura descartada. |
| DC-053 | CONFIRMADA · AUTORIZACIÓN ACOTADA | Instalar y corregir únicamente documentación de coordinación en `docs/ai/`, actualizar handoff y preparar M1, conservando AGENTS.md. No autoriza implementación, migraciones ni aplicación. |

## Confirmaciones de cierre y coordinación
- **DC-054 · CONFIRMADA:** Gemini Argon revalidó y APROBÓ M1 en `6152867`; propietario autoriza cierre documental. Rama pendiente de integración, sin merge ni producción.
- **DC-055 · ACTUALIZADA:** propietario autorizó implementar M2-SOL y cambios acotados necesarios Billing/Gateway. Angular reservado a Argon; sin worktrees, merge/push/despliegue/bases reales ni M3.
- **DC-056 · CONFIRMADA:** nunca dos agentes escriben simultáneamente el mismo directorio Git, incluyendo Git/docs/tests que generen archivos. No usar Git worktrees bajo ninguna circunstancia. Responsabilidad por repositorio y revisión cruzada: [protocolo](06_PROTOCOL.md).

## Políticas confirmadas M2
ACTIVE/INACTIVE consumen cupo; ARCHIVED no y conserva datos/historial. Archivo no cancela turnos. Restaurar/reactivar requiere validación comercial; pacientes y su cuota siguen globales por médico, sin clinic_id. /appointments permanece previsto.
Billing caído permite snapshot efectivo por propietario vigente, verificable y no invalidado; sin snapshot apto bloquear nuevas operaciones comerciales, sin defaults ni extender cobertura. Sustituye bloqueo absoluto del borrador.
Contrato público [M2-CLINICS-v1](contracts/M2_CLINICS_API.md) congelado; [derechos efectivos v1](contracts/M2_EFFECTIVE_ENTITLEMENTS.md) implementados. DC-047 resuelta técnicamente sin nuevas suscripciones; conteo/conservación de DC-048 aprobados. Cuotas reales no inventadas ni configuradas.

## Ingeniería autorizada para M1
El propietario autorizó comenzar M1 con el alcance del ticket el 29/10/2026, incluidos commit documental independiente, rama específica, migraciones incrementales y pruebas automatizadas en PostgreSQL descartable. Sin despliegue, bases reales, cambios de pacientes, Angular, Gateway, Billing o Supabase; M2 requiere nueva aprobación.
- **DC-042 · APROBADA PARA M1:** tabla `clinics` de ubicaciones con FK al workspace del médico, sin relación con pacientes. La futura relación de reservas queda fuera de M1.
- **DC-043 · APROBADA PARA M1:** ID bigint identity de lugares, consistente con el esquema.
Implementación inicial `48f87b1`, corrección final `615286720f98768dfbc85bcbe35d315e41405e87`, revalidación Argon APROBADO y cierre documental autorizado. Evidencia histórica en [correcciones](reviews/M1_CORRECTIONS.md). No autoriza M2 ni integración/publicación.

## Decisiones de ingeniería recomendadas, aún por aprobar
| ID | Estado | Propuesta |
|---|---|---|
| DC-044 | PROPUESTA | Exclusión por médico + rango `[inicio,fin)` PostgreSQL GiST y bloqueo estable para reservas/excepciones. |
| DC-045 | PROPUESTA | Formularios de reserva versionados; predeterminado por médico con override por lugar. |
| DC-046 | PROPUESTA | Auth staff: email verificado, invitación, hash Argon2id, sesiones opacas, HttpOnly/CORS/CSRF y revocación; contrato con Gateway por aprobar. |
| DC-047 | PROPUESTA | Contrato comercial efectivo/versionado por médico con invalidación durable de webhooks y consulta interna; Billing fuente de verdad. |
| DC-048 | PROPUESTA | Cupos de lugares cuentan activos e inactivos, no archivados; downgrade preserva todo y elige recursos operativos. |
| DC-049 | PROPUESTA | Cuotas de secretarias por relación con médico, con invitaciones pendientes consumiendo cupo. |
| DC-050 | PROPUESTA | Nuevas capacidades `clinics`, `staffRelations`, `appointmentManagement`, `onlineBooking` y mantener cuotas clínicas existentes. |

La ruta base pública está confirmada. Segmentos de médico y consultorio, identificadores públicos y rutas administrativas continúan como propuestas hasta aprobar su contrato; ninguna URL anidada de auditorías queda aprobada por referencia.
M1 está cerrado. El [contrato M2](contracts/M2_CLINICS_API.md) concreta propuestas para DC-047/DC-048 y gestión de lugares; DC-044 a DC-050 no quedan aprobadas por su redacción. Las restricciones reales de AGENTS.md, incluido production=true, no se sustituyen con esta documentación.

## Anuladas expresamente (¡NO IMPLEMENTAR!)
| ID anterior | Estado | Qué se descarta |
|---|---|---|
| DC-001 a DC-005 | **ANULADAS** | Consultorio como entorno clínico separado; pacientes e historias propias de sede; match DNI entre sedes; cambio clínico A→B; primer consultorio obligatorio al terminar wizard. |
| DC-012 | **ANULADA** | `patients.clinic_id` y backfill de pacientes a consultorios. |
| DC-018 (en parte) | **ANULADA PARA SEDE** | Borradores clínicos segregados por consultorio; mantener protección estándar por médico/paciente. |
| Plan antiguo M2–M5 | **ANULADO** | Reescritura transversal de clínica, permisos, dashboard, adjuntos, PDFs y wizard por pertenencia a sede. |

## Procedimiento de cambios
Decisión comercial definitiva M2: solo BASIC, PRO y UNLIMITED están vigentes. FREE_TRIAL no activado; ENTERPRISE fuera de oferta. Conservar reconocimiento legacy y alias de metadata sin comercializarlos, configurarlos ni inicializarlos. No ampliar Billing por el P2 referido sobre esos tiers. Verificación y dependencias en [contrato técnico](contracts/M2_EFFECTIVE_ENTITLEMENTS.md) y [entrega](reviews/M2_SOL_DELIVERY.md). No se definen cuotas ni precios aquí.

Última corrección de rutas del propietario prevalece sobre la aclaración transitoria anterior que permitía un alias.

Nueva decisión del propietario → registrar ADR/impacto → actualizar `01_REQUIREMENTS`, `08_ROADMAP`, ticket y handoff → implementar solo tras aprobación. Los `sources/audit-*` no tienen autoridad para reactivar decisiones anuladas.
