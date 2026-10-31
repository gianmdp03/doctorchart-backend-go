# M4-BOOKINGS-v1 — contrato congelado
30/10/2026. Backend médico autenticado; sin portal público, Angular M5, secretarias, pagos o notificaciones.

## API
Prefijo /api/doctorchart. Identidad exclusivamente Supabase/Gateway, aislamiento por workspace del médico. IDs/versiones strings decimales BIGINT canónicos. Instantes RFC3339 con offset obligatorio, persistidos y devueltos UTC.
- POST /bookings: {clinicId,start,patientId?:string|null,contactName,contactPhone?:string}. start debe corresponder EXACTAMENTE a un slot M3 actual; end lo calcula el servidor. 201 Booking, status CONFIRMED, version "1".
- GET /bookings?from=RFC3339&to=RFC3339&page=0&size=20: intersección [from,to), intervalo ≤93 días, size 1..100. 200 {items,page,size,totalElements}; orden start,id. Sin filtro por propietario desde cliente.
- GET /bookings/{id}: 200 Booking.
- GET /bookings/{id}/events: 200 {items:BookingEvent[]}, orden version ascendente.
- PATCH /bookings/{id}/status: {expectedVersion,status}. 200 Booking.
- POST /bookings/{id}/reschedule: {expectedVersion,clinicId,start}. 200 Booking.
Todos los writes requieren Idempotency-Key (16..128 caracteres ASCII alfanuméricos/._:-). Misma clave por médico y mismo método/ruta/cuerpo JSON canónico devuelve el status y Booking originales sin nuevos eventos; cuerpo/operación distintos → 409 IDEMPOTENCY_CONFLICT. Las claves no vencen automáticamente en M4; errores/rollbacks no se guardan como éxitos.

Booking: {id,clinicId,patientId:string|null,start,end,status,version,contactName,contactPhone,createdAt,updatedAt}. Sin DELETE. Nombre contacto requerido, trim 1..160; teléfono opcional trim ≤80. Body ≤16KiB, campos desconocidos/null no permitidos salvo patientId explícitamente opcional.
BookingEvent: {version,kind,actorId,occurredAt,before:Booking|null,after:Booking}; kind CREATED/STATUS_CHANGED/RESCHEDULED. Evento y respuesta idempotente se confirman atómicamente con la reserva, sin sistemas de mensajería nuevos.

## Estados y disponibilidad
CONFIRMED → CHECKED_IN / NO_SHOW / CANCELLED. CHECKED_IN → COMPLETED / CANCELLED. COMPLETED, NO_SHOW y CANCELLED terminales. Estado idéntico con versión vigente no genera cambio/evento; cancelación repetida con nueva clave y versión vigente también es no-op.
Solo CONFIRMED admite reprogramación. La reprogramación conserva datos de contacto/paciente y reemplaza intervalo/sede atómicamente; un error deja la reserva original intacta.
Todas salvo CANCELLED ocupan el intervalo, incluso terminales históricas. Cancelar libera disponibilidad. Reservar nunca crea pacientes ni historias; patientId debe pertenecer al mismo médico y puede omitirse sin afectar la historia clínica.
M3 sigue siendo el único cálculo de slots: duración, franjas, anticipación, horizonte, IANA/DST y excepciones. Solo consultorios ACTIVE admiten nuevas reservas. M3 availability descuenta cualquier intervalo ocupado del mismo workspace, incluso en otra sede.
Cambiar una agenda o zona horaria no puede invalidar reservas futuras CONFIRMED/CHECKED_IN: se rechaza 409 BOOKING_SCHEDULE_CONFLICT hasta reprogramarlas o cancelarlas. Cambiar estado de sede nunca cancela reservas automáticamente; las existentes siguen consultables, mientras no se ofrecen nuevos slots en sedes inactivas/archivadas.

## Integridad
Orden de locks igual a M2/M3: derecho fuera de tx; fence → workspace FOR UPDATE → recursos/versiones → disponibilidad → escritura/evento/idempotencia. Todas las escrituras de agenda y reservas se serializan por médico; no se amplían locks entre médicos.
PostgreSQL agrega exclusión GiST por workspace e intervalo [start,end), sin btree_gist ni extensión nueva: punto numrange(workspace_id,workspace_id,[]) && y tstzrange(start,end,[)) &&, parcial status <> CANCELLED. Filas de reservas no pueden solaparse aunque un escritor omita la comprobación Go.
SQL verifica propiedad de sede y paciente mediante trigger; patientId tiene FK RESTRICT para no borrar historia de vinculación. Tablas de eventos/idempotencia conservan datos de auditoría. No se alteran patients ni sus relaciones clínicas.
Las mutaciones nuevas usan los derechos efectivos M2, incluyendo snapshot apto ante caída de Billing y bloqueo por downgrade; no nuevas cuotas. Repetir una respuesta ya confirmada es lectura y no renueva derechos ni ejecuta una escritura nueva.

## Errores y flag
api.Error existente. 400 INVALID_REQUEST/errores de campo; 401/403/503 de M2; 404 BOOKING_NOT_FOUND o CLINIC_NOT_FOUND/PATIENT_NOT_FOUND sin revelar propietarios; 409 VERSION_CONFLICT, BOOKING_SLOT_UNAVAILABLE, BOOKING_STATE_CONFLICT, BOOKING_SCHEDULE_CONFLICT, IDEMPOTENCY_CONFLICT.
Nueva migración 0006, catálogo fijo PG17/18 construido desde SQL inmutable; fresh/upgrade/rollback y conservación. Sin modificar baseline ni 0002..0005; ningún DOWN destructivo automático.
BOOKINGS_ENABLED=false por defecto; requiere SCHEDULES_ENABLED=true al optar por el motor de ensayo. No cambia flags Angular ni Cloudflare. M5 no autorizado.

