# Mis turnos y correo de secretaría

## Reserva y consulta pública

La reserva pública requiere documento y correo. Con `NOTIFICATIONS_ENABLED=true` se encola un código de seis dígitos y la constancia devuelve su `verificationRequestId` mientras esté vigente. La reserva se confirma en pantalla al guardar. Un fallo posterior del SMTP no revierte la reserva. Cada correo contiene el código y su vigencia sin enlaces, datos del turno ni datos clínicos.

- `POST /api/doctorchart/public/appointments/my-bookings` con `document`, `challenge`, `solution` y `offset` opcional devuelve hasta 50 coincidencias de DNI argentino. Cada entrada incluye una referencia interna, la fecha civil en la zona del turno, disponibilidad de acciones y si la cookie ya autoriza esa reserva. La hora, el profesional, el consultorio y el correo requieren verificación.
- `POST /api/doctorchart/public/appointments/access/request` recibe `document`, la `reference` devuelta por la búsqueda, `challenge` y `solution`. Busca el correo registrado para ese DNI y esa reserva. Si se encoló el correo, devuelve 202 con `requestId` y `maskedEmail`.
- `POST /api/doctorchart/public/appointments/access/verify` recibe `document`, `reference`, `requestId` y `code`. Un código se puede usar una vez, vence en 10 minutos y admite hasta cinco intentos. Al validarlo responde 204 y establece una cookie cifrada, HttpOnly, Secure y SameSite=Strict con una vigencia de 48 horas para los turnos del mismo DNI y correo.
- `my-bookings` con la cookie y sin `document` devuelve hasta 50 reservas completas del par verificado. Admite `offset` y `focusReference` para priorizar el turno seleccionado.
- `POST /api/doctorchart/public/appointments/my-bookings/change` recibe la cookie, `reference`, `expectedVersion`, `action` y, para reprogramar, `start`. Requiere `Idempotency-Key`. Usa el motor de agenda para cancelar una reserva futura confirmada o moverla a un horario disponible del mismo consultorio publicado y especialidad.

El servidor valida el Origin configurado. La sesión se recupera al volver al mismo navegador y `POST /api/doctorchart/public/appointments/my-bookings/logout` la revoca y borra la cookie. El código se verifica con HMAC, la sesión se guarda como hash y el correo en cola se cifra. La solicitud usa la protección pública existente y un límite por correo de cinco por hora con un intervalo mínimo de 45 segundos. La página `/appointments/confirmation` mantiene `noindex` y `no-referrer`.

## Constancia y correo de secretaría

El worker marca los avisos de turnos pendientes de versiones anteriores como `OBSOLETE` con el código `BOOKING_EMAIL_DISABLED`. Los códigos de verificación siguen la cola SMTP y dejan de enviarse si vencieron o ya se usaron. Las reservas online también envían confirmación, reprogramación, cancelación y un recordatorio 24 horas antes. El worker verifica la versión del turno antes de enviar y cada correo explica cómo entrar a Mis turnos.

La cola SMTP conserva las invitaciones y la recuperación de secretaría. La invitación incluye un enlace privado de un solo uso. Los workers usan SKIP LOCKED, lease de 2 minutos, timeout SMTP de 10 segundos y hasta 10 intentos con espera exponencial. Una invitación consumida, renovada o revocada se marca como `OBSOLETE`. Los payloads se cifran con AES-GCM usando la clave estable NOTIFICATIONS_KEY de 32 bytes en base64. Cada reintento conserva el Message-ID.

SMTP se configura con host, puerto, remitente, usuario y contraseña. Usa STARTTLS por defecto o TLS implícito con certificados verificados. La clave de cifrado se conserva para reinicios y restauraciones.

## Secretarias

M7 se conserva: entrega manual y recuperación por código personal siguen funcionando. Con M8, invitación se encola en la misma transacción y su renovación invalida mensajes pendientes anteriores.

POST `/api/doctorchart/staff/recovery-email` `{email}` →202 genérico, exista o no cuenta. Origin exacto M7, límites M7 + máximo3 solicitudes/día por email normalizado (mismo comportamiento para desconocidos);429 genérico. Token de un uso256bits, vence15min, almacenado hash; email/outbox cifrado. No invalida solicitudes anteriores al pedir otra para evitar bloquear recuperación legítima; consumir una invalida todas.
POST `/api/doctorchart/staff/recover-email` `{email,emailCode,password}` →200 `{recoveryCode}` nuevo código personal mostrado una vez, revoca todas las sesiones y códigos de email anteriores. Origin/límites/bcrypt/locks/auditoría M7;401 genérico para datos incorrectos. No Supabase, no sesión automática, no reset global por un médico. Endpoints exactos en Gateway, sin bearer médico/identidad del navegador. Cookies/sesiones/CSRF existentes intactos.

## Prueba y recuperación

El baseline de preproducción se define en `db/schema.sql`. Para probar localmente se puede usar Mailpit. Para enviar por Brevo se configura el SMTP existente con un remitente autenticado y sus credenciales de relay.
