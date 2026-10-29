# Contratos y decisiones pendientes
**29/10/2026.** No reabrir propiedad clínica ni ruta base: pacientes/historias por médico; portal público `/appointments`, sin alias ni redirecciones para esta función.

## Antes de M1
- Resuelto: cuatro repositorios inspeccionados y limpios antes de docs. El propietario descartó intencionalmente el trabajo anterior; no recuperar nada.
- Verificado: `production: true`; baseline 1 y fingerprint en `db/init.go`/`db/catalog.go`. Para M1 se propone migración incremental, manteniendo baseline, transacción, checksums y rechazo de drift. No extender la excepción histórica de estudios.
- Aprobar [M1](tasks/M1_FOUNDATION.md): mínimos de ubicación, nombre técnico `clinics`, ID bigint, FK al workspace propietario, estados y restricciones. Son propuestas de ingeniería, no decisiones ya confirmadas.
- Propuesta: M1 solo infraestructura + tabla, sin API/altas para usuarios hasta cuotas Billing. Ninguna relación nueva con pacientes.
- Al autorizar implementación, definir entorno PostgreSQL 17 descartable propio autorizado. No usar DSN existente ni inferir permiso para crear/destruir bases a partir de esta fase documental.

## Billing — antes de M2
- Auditoría histórica reportó `module_tier_limits`, `billing_webhook_jobs` y recarga de catálogo por callback. Verificar código y revisiones reales antes de implementar; R0 no probó esa integración.
- Contrato efectivo/versionado por médico, cuotas de lugares y secretarias, invalidación durable y disponibilidad ante fallos.
- Aprobar qué consume cupo (activo/inactivo/archivado, invitaciones pendientes), downgrade, vencimiento, cantidades y features en Bycode. No inventar cuotas.
- Verificar compatibilidad de claves nuevas, PUT del editor de límites, readiness y claims/cachés antiguas.

## Staff — antes de acceso local
Autenticación local Doctor Chart confirmada; diseño de invitación, verificación de email, contraseña, sesiones, cookie, CORS/CSRF, recuperación, revocación y contrato Gateway pendientes.
Definir permisos multi-médico, asignación por lugar y comportamiento ante suscripción vencida. Nunca exponer historias, recetas o estudios.

## Agenda y público — antes de T1/T2/T4
- Franjas, duración/intervalo, zonas IANA, excepciones con reservas existentes y bloqueo/exclusión por médico entre lugares.
- Formularios versionados, solicitante separado del atendido y vínculo opcional al paciente del mismo médico. Reservar no crea ficha ni empareja por DNI.
- Ruta base confirmada: `/appointments`. Segmentos de médico y consultorio, identificadores públicos y separación del panel administrativo siguen como propuestas hasta aprobar el contrato completo de rutas.
- Publicación/directorio, verificación profesional, prevención de abuso, notificaciones, recuperación y requisitos de privacidad aplicables por definir.

## Clínica existente
Se usa sin consultorios. No inventar lugares para datos históricos, trasladar pacientes ni agregar `patients.clinic_id`.
Registrar lugar como metadato opcional de consulta sería otra propuesta futura, con aprobación independiente; no cambia propiedad ni autorización clínica.
