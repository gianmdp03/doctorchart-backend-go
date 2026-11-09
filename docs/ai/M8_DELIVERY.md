# M8 — comunicaciones y recuperación de turnos

31/10/2026. M7 aceptado; M8 implementado y validado en ensayo para aprobación. M9 no autorizado. No activación productiva, merges o pushes.

## Versiones y contrato

Ramas normales `codex/m8-notifications`, clones aislados `C:/Users/gcast/Documents/GitHub/doctorchart-m3/`. Bases M7 exactas verificadas: backend d84def8, Angular cc6c79f, Gateway1209462. M1–M7/turnos conservados. Billing permanece69febba en su rama anterior, sin cambios M8; snapshot comercial Angular9428426 sin cambios y sin incorporar main.

| Repositorio | Aplicación M8 | Cierre |
|---|---|---|
| doctorchart-backend-go | **68a4993**, pruebas adicionales **e716623** | HEAD documental posterior |
| doctorchart-frontend | **deee309** | HEAD documental posterior |
| api-ai-gateway-go | **6586a72** | HEAD documental posterior |

Contrato previo0dc3710: [M8-NOTIFICATIONS-v1](contracts/M8_NOTIFICATIONS_API.md). Los fuentes usados en ensayo se compararon con estos commits (119Go/SQL/JSON backend,381Angular,52Billing,55Gateway); solo fixture Angular modifica flags, entorno loopback y serve CSR sin hidratación. No harness sustituyendo router/auth/guards. Nuevas ramas se preservan también mediante fetch LOCAL en repos habituales sin cambiar main, GitHub ni worktrees.

## Resultado funcional

Reserva pública M6 sigue usando applyBooking M4, exclusión/locks, idempotencia y M3. En la misma transacción se registra outbox cifrado y acceso privado; SMTP ocurre después del commit. Error SMTP nunca revierte reserva. Turnos manuales sin email administrativo explícito no inventan destinatario ni leen email de un paciente clínico. Confirmación inmediata incluye accessCode y accessExpiresAt cuando M8 está habilitado; replay obtiene mismo código vigente sin depender del correo.

Correos de texto en español: confirmación, cancelación, reprogramación, recordatorio, invitación y recuperación de secretaría. Incluyen solo profesional, consultorio, dirección públicamente autorizada, fecha/hora IANA, referencia y código privado cuando corresponde. No DNI, contacto/paciente, diagnósticos, respuestas o historias. Enlaces HTTPS sin códigos en URL; código se pega en el formulario. No servicio pago impuesto. SMTP configurable STARTTLS obligatorio por defecto o TLS implícito, verificación de certificado, sin fallback plaintext; sin credenciales productivas.

Outbox: UNIQUE por reserva/versión/tipo o invitación/token y recuperación/id. Payload AES-GCM con AAD de dedup-key; códigos de consulta con hash para búsqueda y copia cifrada/AAD por reserva para replay/email. Clave estable32bytes base64 separada del secreto Gateway; startup comprueba que puede descifrar datos existentes. Estados PENDING/PROCESSING/SENT/OBSOLETE/DEAD, lease2min, SKIP LOCKED, dos workers, timeout SMTP10s, reintento exponencial1s..5min hasta10intentos, errores constantes. Message-ID y Date estables por trabajo. Reinicios recuperan PROCESSING tras lease y PENDING desde PostgreSQL; ningún lock de reserva durante SMTP.

Recordatorio configurable con minutos reales antes del instante UTC (1440 por defecto), texto en zona IANA/DST. Si el momento del aviso ya pasó no crea uno inmediato. Antes de SMTP exige misma versión/CONFIRMED/turno futuro: cancelación, reprogramación, atención o ausencia convierten trabajos antiguos en OBSOLETE. Invitaciones renovadas/consumidas/revocadas y recuperación email expirada/consumida también se descartan.

## Recuperación y seguridad

POST `/api/doctorchart/public/appointments/confirmation` recibe solo `{accessCode}` en body. Devuelve referencia/estado/instantes/profesional/sede/dirección pública/zona/vencimiento, sin IDs internos ni PII del solicitante, sin Billing ni Supabase. Código256bits, expira siete días después del fin, ajustado al reprogramar; invalidez/vencimiento/revocación404 uniforme,30consultas/min por red Gateway HMAC, body16KiB y429Retry-After60. No consulta por DNI/email, reenvío público o cancelación pública. DELETE médico `/bookings/{id}/confirmation-access` revoca código propio, sede/propiedad cruzada404. Consulta sigue mostrando estado CANCELLED cuando corresponde.

Angular `/appointments/confirmation`, anterior a `:publicId`, guard M8, sin bearer/consulta clínica/transfer-cache, noindex/no-referrer/no-store; código solo memoria/body, nunca query/localStorage/analytics. Manejo de errores/doble clic/respuestas tardías. Constancia inmediata mantiene acceso aunque SMTP caiga. Interfaz revisada en escritorio1500 y móvil390/oscuro; emulación, no teléfono físico.

Secretarias: invitación M7 encola entrega en la misma transacción y conserva entrega manual. Nueva recuperación email en `/staff/recovery-email`202genérico y `/staff/recover-email`; código separado256bits/15min/una vez, máximo3solicitudes/día por email conocido o desconocido, más límites red/cuenta y OriginM7. Reset bloquea cuenta, revalida tras lock, consume todos los códigos email, cambia bcrypt, rota código personal y revoca sesiones; no sesión automática ni Supabase. Cambio por contraseña/código personal M7 invalida códigos email pendientes. Cookies/CSRF/permisos y autenticación médica existentes intactos. Sin código personal maestro por email.

## PostgreSQL y pruebas

Migración incremental0010: booking_confirmation_access, staff_email_recovery, notification_outbox. Baseline/0002..0009 y AGENTS.md intactos. Esquema10: **44tablas/45FK**. Pins construidos independientemente desde SQL inmutable en bases vacías: PG17 `b082e9bf04d8924d1a621a8e3fae8224f8fd02150256e5aa2cc13c1dca584572`; PG18 `9b9a194a5ad6e90a74bd809b6de10f7ba6454b851091864b3af7d38a971269e8`. Fresh/upgrade9→10, fallo SQL/drift/seed tardío, rollback/retry/reinicio y conservación de pacientes/reservas/cuentas/sesiones staff probados. Sin DOWN destructivo: binario previo rechaza10; recuperación requiere binario compatible o restauración autorizada, nunca modificar marcador.

| Prueba ejecutada | Resultado |
|---|---|
| DoctorChart Go completo sobre aplicación68a4993 | **575 casos/subcasos PASS**,0FAIL,6 externos omitidos |
| Nuevas regresiones finales e716623 con -race | **2 PASS**,0FAIL/SKIP: attended/revoked/renewed y recuperación email simultánea |
| Notifications/clinics -race | **17 PASS**,0FAIL/SKIP, sin carreras |
| PostgreSQL18 migración/arranque | **5 PASS**,0FAIL/SKIP |
| Gateway completo, vet/build | **358 PASS**,0FAIL,15 externos omitidos |
| DoctorChart vet/build | PASS |
| Angular | **773 PASS**,95archivos,0FAIL/SKIP |
| Development y production/deployment/SEO | PASS, clave pública sintética solo build |
| Hosting/configuración incluidas nuevas rutas directas | **8 PASS**,0FAIL/SKIP |
| Chrome + Gateway/Go/Billing/PostgreSQL/Auth/Mailpit reales | **9 recorridos PASS** + **3 de dependencias PASS** |

Integración: reserva anónima por Angular y entrega STARTTLS real a Mailpit; confirmación recuperada y móvil/oscuro; reprogramación+email; recordatorio vigente/versión anterior obsoleta; cancelación+email/consultaactual; SMTP detenido con reserva confirmada y código funcional, reintento tras reiniciar backend; invitación médica real y activación local; recuperación email desde bandeja real de ensayo y replay401; códigos inválidos/vencidos/otropropietario, snapshots clínicos y Auth preservados. Tres recorridos adicionales: Billing parado no bloquea consulta existente; Gateway parado conserva código y muestra error sin datos viejos; médico real revoca acceso propio sin tocar reserva.

Evidencia outbox sin secretos: `BOOKING_CONFIRMED:SENT:2` después de caída/reinicio; reminder anterior `OBSOLETE:STATE_CHANGED`; avisos confirmación/reprogramación/cancelación/recordatorio/invitación/recuperación enviados. Dos instancias Service concurrentes verificadas en Go; aplicación ejecutó sus dos workers SMTP. No exactly-once físico afirmado. Exactamente7cuentas Supabase médicas/administrativas iniciales; secretaria solo local. Sin JWT fabricados para declarar autenticación aprobada. Logs stdout/stderr de backend/Gateway/Billing comprobados sin códigos/DNI/respuestas.

21omitidos externos no son PASS:6DoctorChart (converter/S3/fixtureslegacy),15Gateway (Pointly/AI/Billing/SMTP/fixturesperiféricos). Ningún nuevo M8 omitido. Billing no se modifica ni se repite su auditoría; binario69febba participa en integración real con catálogo sintético. Manifest con nombres exactos y hashes: `.fixtures/m8/verification-summary.json`. Logs backend-accepted.jsonl,gateway.jsonl,race.jsonl,edge-final.jsonl,pg18.jsonl,angular-tests.log,static-tests.log; integration/results.json,dependencies-results.json,verification.json,versions-verified.json,outbox-evidence.json y capturas. Fuera de Git, secretos sintéticos privados no publicados. Recursos propios retirados al cierre preservando evidencia.

## Configuración, límites y continuidad

Variables: NOTIFICATIONS_ENABLED=false; al habilitar requiere M4 y NOTIFICATIONS_KEY32bytes base64, NOTIFICATIONS_PUBLIC_ORIGIN HTTPS exacto, NOTIFICATIONS_SMTP_HOST/PORT(587)/FROM/USERNAME/PASSWORD/TLS(starttls|tls), NOTIFICATIONS_REMINDER_MINUTES(1440,1..10080). Conservar clave al reiniciar/restaurar; rotación automática no implementada. Angular NOTIFICATIONS_ENABLED=false; flags anteriores también apagados en código normal. Ninguna configuración comercial real ni destinatario real utilizado; todo email a .invalid en Mailpit descartable con certificado de ensayo verificado.

- SMTP puede aceptar DATA y perder su respuesta: entrega física duplicada posible al reintentar, mismo Message-ID. Cambios posteriores a validación final/aceptaciónSMTP no pueden retirar correo ya entregado. No prometer cancelación de mensajes en vuelo ni exactly-once físico.
- DEAD tras10intentos requiere revisión operativa; no panel nuevo. Para reprocesar, verificar proveedor/causa/propiedad del job y autorización de entorno antes de reponer PENDING, sin borrar historial ni cambiar payload.
- Reservas anteriores a M8 no reciben backfill masivo. Obtienen acceso/avisos al siguiente cambio administrativo elegible; no se envían recordatorios históricos ni se inventan emails para turnos manuales.
- Código de consulta reutilizable hasta vencimiento/revocación, solo lectura; quien lo posee puede ver la constancia administrativa. Sin recuperación por PII ni cancelación pública. No contraseñas/email oficiales verificados de visitantes.
- Ensayo HTTPS loopback18443/Auth18543, STARTTLS1025/Mailpit8025; no validación de proveedor/DNS/dominios/Cloudflare productivos ni envío real. SMTP deliverability/SPF/DKIM/DMARC y activación requieren etapa autorizada aparte.
- Editor comercial onlineBooking/secretaryManagement queda para **M9**, no iniciado. Sin WhatsApp/SMS/pagos/campañas/apps/videollamadas/plantillasmultiempresa.

Cinco main limpias, sin cambios M8 realizados aquí: DoctorChart Go d5a89e0, Angular1419697, Billingbf65c04, Gateway55197fa, Bycode frontend**8ac5b14**. En Bycode frontend se preservan commits ajenos89f032c y su descendiente8ac5b14 (31/10,12:15-03:00); no sobrescritos ni incorporados a ramas de turnos. Sin worktrees, GitHub remoto, push, merge, despliegue, base real o M9. Próximo paso: aceptación del propietario.
