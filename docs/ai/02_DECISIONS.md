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
| DC-041 | CONFIRMADA | Sol implementa y Argon audita independientemente en solo lectura, con documentación persistente, handoff y aprobación humana antes de cada fase. |
| DC-051 | CONFIRMADA · CORRECCIÓN DEFINITIVA | Ruta pública de turnos `/appointments`, en inglés. Sin alias ni redirecciones de compatibilidad porque la función no fue lanzada. Sustituye cualquier alternativa anterior. |
| DC-052 | CONFIRMADA | El propietario descartó intencionalmente el trabajo anterior y preparó repositorios limpios. No recuperar ni reconstruir cambios de la arquitectura descartada. |
| DC-053 | CONFIRMADA · AUTORIZACIÓN ACOTADA | Instalar y corregir únicamente documentación de coordinación en `docs/ai/`, actualizar handoff y preparar M1, conservando AGENTS.md. No autoriza implementación, migraciones ni aplicación. |

## Decisiones de ingeniería recomendadas, aún por aprobar
| ID | Estado | Propuesta |
|---|---|---|
| DC-042 | PROPUESTA | `clinics` es entidad de ubicaciones del médico para agendas; paciente **sin clinic_id**; `appointments` referencia ubicación y opcionalmente paciente del mismo médico. |
| DC-043 | PROPUESTA | `BIGINT` para ID de lugar; validar convenciones del esquema real. |
| DC-044 | PROPUESTA | Exclusión por médico + rango `[inicio,fin)` PostgreSQL GiST y bloqueo estable para reservas/excepciones. |
| DC-045 | PROPUESTA | Formularios de reserva versionados; predeterminado por médico con override por lugar. |
| DC-046 | PROPUESTA | Auth staff: email verificado, invitación, hash Argon2id, sesiones opacas, HttpOnly/CORS/CSRF y revocación; contrato con Gateway por aprobar. |
| DC-047 | PROPUESTA | Contrato comercial efectivo/versionado por médico con invalidación durable de webhooks y consulta interna; Billing fuente de verdad. |
| DC-048 | PROPUESTA | Cupos de lugares cuentan activos e inactivos, no archivados; downgrade preserva todo y elige recursos operativos. |
| DC-049 | PROPUESTA | Cuotas de secretarias por relación con médico, con invitaciones pendientes consumiendo cupo. |
| DC-050 | PROPUESTA | Nuevas capacidades `clinics`, `staffRelations`, `appointmentManagement`, `onlineBooking` y mantener cuotas clínicas existentes. |

La ruta base pública está confirmada. Segmentos de médico y consultorio, identificadores públicos y rutas administrativas continúan como propuestas hasta aprobar su contrato; ninguna URL anidada de auditorías queda aprobada por referencia.
M1 está preparado, pero DC-042 a DC-050 siguen siendo propuestas. Las restricciones reales de AGENTS.md, incluido production=true, no se sustituyen con esta documentación.

## Anuladas expresamente (¡NO IMPLEMENTAR!)
| ID anterior | Estado | Qué se descarta |
|---|---|---|
| DC-001 a DC-005 | **ANULADAS** | Consultorio como entorno clínico separado; pacientes e historias propias de sede; match DNI entre sedes; cambio clínico A→B; primer consultorio obligatorio al terminar wizard. |
| DC-012 | **ANULADA** | `patients.clinic_id` y backfill de pacientes a consultorios. |
| DC-018 (en parte) | **ANULADA PARA SEDE** | Borradores clínicos segregados por consultorio; mantener protección estándar por médico/paciente. |
| Plan antiguo M2–M5 | **ANULADO** | Reescritura transversal de clínica, permisos, dashboard, adjuntos, PDFs y wizard por pertenencia a sede. |

## Procedimiento de cambios
Última corrección de rutas del propietario prevalece sobre la aclaración transitoria anterior que permitía un alias.

Nueva decisión del propietario → registrar ADR/impacto → actualizar `01_REQUIREMENTS`, `08_ROADMAP`, ticket y handoff → implementar solo tras aprobación. Los `sources/audit-*` no tienen autoridad para reactivar decisiones anuladas.
