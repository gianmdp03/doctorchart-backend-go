# M7-SECRETARIES-v1

31/10/2026. Desarrollo aislado desde M6; flags desactivados por defecto. Médico Supabase sin cambios; secretaria local sin cuenta Supabase. No acceso clínico, pacientes, recetas, estudios, planes, publicación ni horarios.

## Identidad y seguridad

Cuenta local por email normalizado, contraseña bcrypt (12, 12–72 bytes), sesiones opacas aleatorias de 256 bits, solo hash SHA-256 persistido. Cookie `__Secure-dc_staff`, HttpOnly, Secure, SameSite=Strict, Path=/api/doctorchart/staff; duración absoluta 8h. Sin sesión médica inyectada ni headers de identidad aceptados. Mutaciones requieren Origin exactamente autorizado y token CSRF ligado a la sesión; login/activación/recuperación también comprueban Origin. Gateway solo permite operaciones staff explícitas, mantiene el secreto servidor y elimina identidad/bearer del navegador. Límite persistente por red y cuenta, respuestas genéricas de autenticación. Auditoría sin contraseñas/tokens.

Invitación privada de un solo uso con vencimiento de 6 horas. El enlace lleva el secreto en el fragmento de `/staff/activate#invite=…`; Angular lo quita del historial y lo envía en el cuerpo de la solicitud. Consultar el enlace no lo consume. La cuenta existente requiere su contraseña actual. El código personal de recuperación se puede guardar de forma opcional desde la agenda. Recuperar exige email+código+contraseña nueva; consume y rota el código y revoca todas las sesiones. Un médico solo revoca su relación, nunca restablece una cuenta compartida. Cambiar la contraseña exige la contraseña actual y revoca las otras sesiones.

## Médico autenticado

`GET /api/doctorchart/secretaries`: relaciones propias, permisos, sedes y versión decimal string.
`POST /secretaries/invitations`: `{email,name,phone?,clinicIds:string[],permissions:string[]}` →201 `{id,invitationCode,expiresAt}`. El teléfono opcional usa formato internacional con `+` y código de país. La respuesta incluye el secreto solo al crearlo. El médico comparte el enlace por el canal que prefiera.
`PUT /secretaries/{id}`: `{expectedVersion,clinicIds,permissions}` → relación actualizada; `DELETE /secretaries/{id}` revoca relación, invitaciones y autoridad inmediatamente. Revocar no depende de Billing.
Permisos explícitos: `view`, `create`, `cancel`, `reschedule`, `status`. `view` obligatorio para los demás. Nunca permiso clínico ni de horarios. Asignación solo sedes propias no archivadas. Revocación y reservas se serializan mediante el mismo lock de workspace M4.

## Secretaria local

Base `/api/doctorchart/staff`:
- POST `/invitation` `{invitationCode}` → `{email,name,expiresAt,existingAccount}` para enlaces vigentes. Requiere el origen autorizado y aplica límites por red y código.
- POST `/activate` `{email,password,invitationCode}` → cuenta/sesión + recoveryCode una vez.
- POST `/login` `{email,password}` → sesión + csrfToken.
- POST `/recover` `{email,recoveryCode,password}` → código nuevo, revoca sesiones.
- GET `/session` → `{email,name,csrfToken,expiresAt}`; POST `/logout`; POST `/password` `{currentPassword,password}`.
- GET `/assignments` → médicos/sedes/permisos efectivos, sin datos clínicos.
- GET `/owners/{ownerId}/clinics/{clinicId}/availability` (M3 from/days).
- GET `/owners/{ownerId}/clinics/{clinicId}/bookings` (M4 from/to/page/size), proyección administrativa sin patientId.
- POST `/owners/{ownerId}/bookings`; PATCH `/owners/{ownerId}/bookings/{id}/status`; POST `/owners/{ownerId}/bookings/{id}/reschedule`.

Mutaciones reutilizan DTO M4 salvo prohibición de patientId, claves idempotentes aisladas por secretaria y relación; devuelven proyección sin patientId. El origen y destino de una reprogramación requieren asignación. Cancelar requiere cancel; otros estados status. Eventos M4 identifican actor local sin fingir médico.

## Derechos y errores

Capacidad independiente `secretary_management_enabled`, ausente/false deniega, solo BASIC/PRO/UNLIMITED; sin cuotas nuevas ni valores por defecto. Snapshot firmado v3 contiene derechos M2+M6 y `secretaryManagement`, misma cobertura, invalidación y expiración; v1/v2 intactos. Derechos del propietario verificados en servidor en cada operación delegada, mismo snapshot/fence que la mutación. Billing inaccesible admite snapshot verificable vigente/no invalidado; sin él 503. Cuenta local puede iniciar/cerrar sesión sin plan propio.

400 INVALID_REQUEST; 401 STAFF_UNAUTHENTICATED/INVALID_CREDENTIALS (genérico); 403 STAFF_FORBIDDEN/CSRF_REJECTED/SECRETARY_MANAGEMENT_DISABLED; 404 recursos fuera de asignación; 409 VERSION_CONFLICT y errores M4; 429 con Retry-After; 503 ENTITLEMENTS_UNAVAILABLE. Sin datos privados en errores ni tokens en URLs/logs.

Angular: `/staff/login`, `/staff/activate`, `/staff/recover`, `/staff/calendar` y médica `/secretaries`, rutas inglesas, guard local separado y cookie con credentials, sin interceptor Supabase. Calendario reutiliza cálculo/presentación M5 donde sea seguro, nunca consultas clínicas. Fechas con app-date-picker.

## PostgreSQL y aceptación

Migración incremental 0009, baseline/0002..0008 inmutables. Tablas locales de cuentas, relaciones, asignaciones, invitaciones, sesiones, límites y auditoría; expansión sin borrar datos. Fresh/upgrade desde8 y rollback transaccional probados en PostgreSQL descartable; downgrade de binario no autorizado sobre9. Preservación M1–M6. Demostrar médico real Supabase + secretaria local a través Gateway/TLS, cookie/CSRF, recuperación/revocación, múltiples médicos, denegación clínica, concurrencia M4, expiración comercial, móvil/oscuro. Ningún omitido equivale a PASS. Sin producción ni M8.

Renovar una invitación pendiente utiliza el mismo POST /secretaries/invitations por email propio: conserva la relación, incrementa versión e invalida todos los códigos previos. Una cuenta ya activada se administra mediante PUT, sin restablecer su identidad.

Asignaciones incluyen validUntil del derecho efectivo y doctorName informativo tomado del nombre profesional M6 configurado (también si el perfil no está publicado); fallback «Profesional asignado». No se consultan campos clínicos ni se muestran fragmentos de UID en la interfaz.
