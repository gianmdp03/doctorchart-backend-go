# M8-NOTIFICATIONS-v1

31/10/2026. Rama codex/m8-notifications desde M7 aprobado. NOTIFICATIONS_ENABLED=false por defecto en backend y Angular. Sin cambios comerciales/Billing, main, producción ni M9. M4 sigue siendo único motor.

## Reserva y consulta pública

Con M8 habilitado, la confirmación M6 incorpora `accessCode` (256 bits, base64url43) y `accessExpiresAt`. Campos M6 restantes intactos. Un replay autorizado devuelve el mismo código mientras esté vigente. No depende de SMTP; la reserva y la constancia inmediata sobreviven a una caída del proveedor.

POST `/api/doctorchart/public/appointments/confirmation` `{accessCode:string}` →200 `{reference,status,start,end,doctorName,clinicName,address,timezone,accessExpiresAt}`. Read-only, sin cuenta, Billing, DNI, email, teléfono, IDs internos ni respuestas del formulario. Código solo en body, nunca query/path/localStorage/analytics. Ruta Angular `/appointments/confirmation` anterior a `:publicId`, noindex/no-store/no-referrer. Email enlaza esa ruta sin secretos; el usuario pega el código. Código atado a una sola reserva; hash SHA-256 para búsqueda, copia AES-GCM con AAD por reserva para entrega/replay. Caduca siete días después del final del turno; reprogramación ajusta vencimiento. Cancelar permite consultar estado cancelado. Ninguna cancelación pública en M8.

DELETE médico `/api/doctorchart/bookings/{id}/confirmation-access` →204 revoca el código de esa reserva propia, sin borrar reserva/historia ni generar nuevo código. Datos fuera de propiedad404. Rate limit persistente recuperación30/min por red HMAC Gateway, body16KiB, código inválido/vencido/revocado/ajeno devuelve404 `CONFIRMATION_NOT_FOUND`, sin diferencias que faciliten enumeración;429 Retry-After60. Sin buscador por PII ni endpoint de reenvío público.

## Correos y outbox

Solo destinatario administrativo explícito de la reserva pública M6; no inferir email desde paciente clínico ni inventarlo para turnos manuales sin email. Plantillas de texto en español: confirmación, cancelación, reprogramación y recordatorio. Solo profesional, sede/dirección pública, fecha/hora IANA y referencia/código de consulta; sin diagnósticos, nombres del paciente/solicitante, DNI/teléfono ni respuestas. Invitación staff incluye código privado de un uso M7; nunca contraseña/código maestro personal.

Evento y outbox se insertan atómicamente con M4; nunca SMTP dentro de esa transacción. UNIQUE por reserva/versión/tipo e invitación/hash. Workers con SKIP LOCKED, lease2min, timeout SMTP10s, reintento exponencial1s..5min, máximo10 intentos, estados PENDING/PROCESSING/SENT/OBSOLETE/DEAD, errores constantes sin cuerpos/secretos. Jobs PROCESSING recuperables tras lease vencida/reinicio. Payloads cifrados AES-GCM con AAD de dedup-key; clave estable separada NOTIFICATIONS_KEY de32bytes base64, necesaria al habilitar M8. Message-ID estable por job. No plataforma genérica ni nuevas cuotas.

Recordatorio: NOTIFICATIONS_REMINDER_MINUTES=1440 por defecto, minutos reales antes del instante UTC, presentación en zona IANA; si ese momento ya pasó no se programa aviso inmediato duplicado. Cada versión obtiene su job; justo antes de enviar se verifica versión, estado CONFIRMED y turno futuro. Cancelado/reprogramado/atendido/ausente o invitación consumida/renovada/revocada se marca OBSOLETE. No se retienen locks de reservas durante SMTP. Una modificación posterior a la comprobación final o a la aceptación SMTP no puede retirar correo ya entregado; tampoco se garantiza exactly-once físico cuando se pierde un ACK. Se garantiza unicidad lógica y se usa mismo Message-ID en reintentos.

SMTP configurable por host/puerto/from/usuario/password; STARTTLS obligatorio por defecto o TLS implícito, certificados verificados, sin fallback plaintext. Ensayo con Mailpit descartable y TLS. Cero envíos a proveedores/usuarios reales. Fallo SMTP no revierte reservas; fallo PostgreSQL al persistir outbox sí impide commit incompleto, como otros eventos M4. Clave de cifrado debe conservarse para reinicios/restauraciones; rotación automática fuera de alcance.

## Secretarias

M7 se conserva: entrega manual y recuperación por código personal siguen funcionando. Con M8, invitación se encola en la misma transacción y su renovación invalida mensajes pendientes anteriores.

POST `/api/doctorchart/staff/recovery-email` `{email}` →202 genérico, exista o no cuenta. Origin exacto M7, límites M7 + máximo3 solicitudes/día por email normalizado (mismo comportamiento para desconocidos);429 genérico. Token de un uso256bits, vence15min, almacenado hash; email/outbox cifrado. No invalida solicitudes anteriores al pedir otra para evitar bloquear recuperación legítima; consumir una invalida todas.
POST `/api/doctorchart/staff/recover-email` `{email,emailCode,password}` →200 `{recoveryCode}` nuevo código personal mostrado una vez, revoca todas las sesiones y códigos de email anteriores. Origin/límites/bcrypt/locks/auditoría M7;401 genérico para datos incorrectos. No Supabase, no sesión automática, no reset global por un médico. Endpoints exactos en Gateway, sin bearer médico/identidad del navegador. Cookies/sesiones/CSRF existentes intactos.

## Prueba y recuperación

Migración incremental0010, baseline/0002..0009 inmutables. Fresh/upgrade9→10, fallos SQL/drift/seed y rollback/reintento/preservación sobre PG descartable. Binarios anteriores rechazan10, sin DOWN destructivo ni bajar marcador. Probar Mailpit TLS real, caída/reintento/reinicio/dos workers, obsolescencia, idempotencia, consulta/códigos inválidos/expirados/revocados/aislamiento, cuenta médica real y staff local. No contar omitidos PASS. Editor comercial onlineBooking/secretaryManagement va a M9, no implementado aquí.
