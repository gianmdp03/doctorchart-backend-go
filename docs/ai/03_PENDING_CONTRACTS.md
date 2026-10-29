# Contratos y decisiones pendientes
**29/10/2026.** No reabrir propiedad clínica ni ruta base: pacientes/historias por médico; portal público `/appointments`, sin alias ni redirecciones para esta función.

## M1 — corregido, listo para aprobación
El propietario aprobó M1 y bases descartables. Implementación `48f87b1`: baseline intacto, transición versionada 1 → 2, catálogo fijado para PostgreSQL 17/18, tabla `clinics` con bigint/FK al workspace y estados ACTIVE/INACTIVE/ARCHIVED.
Pruebas y recuperación: [handoff](05_HANDOFF.md), [resumen de auditoría](reviews/M1_ARGON_BRIEF.md) y [db/README](../../db/README.md).
Auditoría Argon recibida: 0 críticos, 1 obligatorio, 2 observaciones menores; correcciones completadas. Pendientes: revalidación Argon y aceptación del propietario. No hay endpoints de lugares ni altas para usuarios. No se aprobaron M2, migraciones reales o despliegue.

## M2 — validación horaria pendiente (observación de Argon)
Registrar validación de zonas IANA en Go, adicional a la restricción SQL existente, con criterios coherentes y pruebas de entradas válidas/inválidas y disponibilidad de tzdata. No implementado en M1; M2 no autorizado.

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
