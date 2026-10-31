# M3-AVAILABILITY-v1 — contrato de implementación
30/10/2026. Contrato congelado M3-AVAILABILITY-v1. Alcance autorizado: configuración y simulación de disponibilidad; sin reservas, pacientes nuevos ni M4. Complementa M2-CLINICS-v1, que permanece intacto.

## Recursos y rutas
Prefijo existente /api/doctorchart. Identidad exclusivamente del Gateway; propietario del workspace. IDs y versiones strings decimales BIGINT canónicos.
GET /clinics/{id}/schedule → 200 Schedule. Agenda ausente: version "0", configured=false, weekly=[], exceptions=[], duración/anticipación/horizonte null (sin valores comerciales inventados).
PUT /clinics/{id}/schedule → reemplazo atómico completo, 200 Schedule; exige expectedVersion y todos los campos de configuración. Sin DELETE: weekly vacío cierra la agenda conservando configuración/excepciones.
GET /clinics/{id}/availability?from=YYYY-MM-DD&days=1..7 → 200 {clinicId,timezone,scheduleVersion,generatedAt,days:[{date,slots:[{start,end}]}]}. Instantes start/end RFC3339 UTC; fechas y franjas son locales de la zona IANA del consultorio. Sin timeZone adicional. Fechas en UI dd/MM/aaaa mediante app-date-picker.

Schedule: {clinicId,version,configured,slotMinutes,minNoticeMinutes,maxAdvanceDays,weekly,exceptions}.
weekly: [{weekday:1..7, ranges:[{start:"09:00",end:"12:00"}]}], lunes=1. Máximo siete días distintos; varias franjas; intervalos [inicio,fin), sin cruces de medianoche y sin superposiciones. 24:00 permitido solo como fin.
exceptions: [{from:"2026-12-24",through:"2027-01-03",label:"Vacaciones",ranges:[]}]. Fechas inclusivas; rangos vacíos cierran. Franjas especiales reemplazan completamente la semana. Excepciones no superpuestas entre sí; fechas ISO reales, etiqueta ≤160 caracteres.
PUT: {expectedVersion,slotMinutes,minNoticeMinutes,maxAdvanceDays,weekly,exceptions}; sin clinicId/zone/version/configured asignables por cliente.
Límites técnicos (no cuotas comerciales): slotMinutes 5..480; minNoticeMinutes 0..525600; maxAdvanceDays 1..366; hasta 16 franjas por día y 100 excepciones por agenda; body ≤64KiB. Son defensas de entrada/cálculo, no cantidades por plan.

## Disponibilidad, DST y conflictos
Solo consultorios ACTIVE producen disponibilidad; INACTIVE/ARCHIVED devuelven días vacíos con motivo visible. Se conserva la configuración al cambiar estado.
Anticipación y horizonte se computan en tiempo absoluto desde el reloj servidor; solo slots completos dentro de una franja y de esa ventana. No se almacenan slots.
Saltos DST: horas locales inexistentes no generan slots. Horas repetidas pueden generar dos instantes distintos, identificados por UTC/offset al mostrarlos; nunca deduplicar únicamente por texto de hora. Un turno no atraviesa un salto de offset.
Las agendas del mismo propietario se validan juntas, bajo el lock del workspace de M2. La comprobación de guardado cubre desde ayer UTC hasta 367 días después de hoy UTC, incluido el horizonte técnico máximo y los bordes entre zonas. La validación incluye agendas inactivas conservadas para evitar reactivaciones incompatibles; archivadas no participan hasta restauración. Todo cambio de zona/estado de consultorio que altere esa compatibilidad se valida en la misma transacción.
La consulta vuelve a comprobar intersecciones UTC en las fechas solicitadas: nunca publica disponibilidad incompatible por cambios futuros de tzdata. Los conflictos informan el período comprobado; las consultas añaden dos fechas locales de margen a cada lado para contemplar el cruce de la línea internacional de fecha. No se ofrece garantía sobre reglas futuras modificadas por otra versión de tzdata: cada consulta las revalida.

## Permisos, concurrencia y errores
Lecturas respetan aislamiento y acceso médico existentes; recurso ajeno/inexistente → 404 CLINIC_NOT_FOUND antes de revelar agenda/derechos. Escrituras reutilizan derechos efectivos M2 (sin nuevas cuotas), fence de invalidación, estado archivado y bloqueo por downgrade; ninguna concesión por defecto al caer Billing.
PUT obtiene derechos fuera de transacción, luego fence → workspace lock → recurso → versión → validación cruzada → escritura/commit; revisar lease nuevamente tras esperar locks. Conflicto revierte toda la operación. Modificar agenda de otro consultorio no puede saltar validación mediante concurrencia.
Errores mantienen api.Error {code,message,errors:[{path,code}]}: 400 INVALID_REQUEST; 409 VERSION_CONFLICT, SCHEDULE_CONFLICT o CLINIC_STATE_CONFLICT; 403/503 comerciales de M2. Conflictos preservan borrador Angular y exigen recargar/reintentar explícitamente. No reintentos automáticos de escrituras.

## Evolución e interfaz
Nueva migración 0005 incremental con catálogo independiente PG17/18; baseline y migraciones 0002..0004 inmutables. Sin cambios clínicos. Instalación limpia, upgrade 4→5, rollback transaccional y rechazo de drift comprobados antes de dar por terminada la migración.
Angular: ruta en inglés /clinics/{id}/schedule, selección explícita de consultorio, editor semanal, excepciones y disponibilidad día/semana; HTTP real. Flag Angular SCHEDULES_ENABLED=false, CLINICS_ENABLED permanece false. Backend SCHEDULES_ENABLED=false por defecto: las rutas M3 ni siquiera se registran sin opt-in explícito de ensayo. App normal no expone el módulo ni cambia Cloudflare.

