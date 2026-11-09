# M6 — reservas públicas online validadas en ensayo
31/10/2026. M5 aceptado por el propietario; M6 completado para aceptación técnica. Sin publicación ni integración a main. M7 no autorizado.

## Versiones
Ramas normales `codex/m6-public-booking`, en los clones aislados existentes `C:/Users/gcast/Documents/GitHub/doctorchart-m3/`:

| Repositorio | Base aprobada utilizada por M5 | Aplicación final |
|---|---|---|
| doctorchart-backend-go | 89a2564 (contiene M1–M4 completos) | ebfb2af + 5276875 + dca90a0 + 9e86373 + **8ccf7c2** |
| doctorchart-frontend | a3dfb0d (Angular M3/M5; M4 no lo modificó) | **5c45d4e** |
| bycode-backend-go | 80c171c (dependencia sin cambios en M5) | 74f953b + **6bed61d** |
| api-ai-gateway-go | 55197fa (dependencia sin cambios en M5) | **d23924f** |

El contrato previo quedó en 0fcf329. Cierres documentales posteriores visibles en HEAD. Los cuatro snapshots de aplicación se verificaron contra Git; 250 archivos Angular comparados. Únicas diferencias de fixture: flags activados, configuración loopback, serve CSR sin hidratación y angular.json. Se usó la aplicación real, no un harness sustituyendo router/auth/guards. Bycode frontend 9428426 se conserva sin cambios M6.

Main/turnos/M3/M4/M5 anteriores no se mueven; nuevas refs M6 se conservan también mediante fetch del clon LOCAL en los checkouts habituales, sin GitHub. Billing y Gateway usan clones normales adicionales desde sus versiones exactas utilizadas por M5, no desarrollo en main ni worktrees.

## Contrato, pantallas y datos
[M6-PUBLIC-APPOINTMENTS-v1](contracts/M6_PUBLIC_APPOINTMENTS_API.md) es el contrato canónico. Pantallas `/appointments`, `/appointments/:publicId` y privada `/booking-publication`. Búsqueda por país/provincia/ciudad/especialidad/nombre, paginación, perfil y sedes explícitamente autorizados, slots M3, formulario administrativo y confirmación. Acceso público sin login y también con sesión médica activa, sin dashboard ni shell privado.

API pública exacta bajo `/api/doctorchart/public/appointments`:
- GET `/doctors` y `/doctors/{publicId}`.
- GET `/doctors/{publicId}/clinics/{publicClinicId}/availability`.
- POST `/challenge` y `/doctors/{publicId}/bookings`.

API privada: GET/PUT `/api/doctorchart/booking-publication`; GET `/api/doctorchart/bookings/{id}/public-details`. Reservas y administración M4/M5 permanecen protegidas. Identificadores públicos UUID independientes, sin workspaceId, userId, bookingId, patientId o IDs administrativos de sedes en respuestas públicas. No endpoint público para leer reservas por referencia.

Solicitante: nombre, documento/país/tipo, teléfono y email; persona atendida distinta opcional. Hasta 8 preguntas administrativas cortas, con claves estables y snapshot/version inmutable de etiquetas/respuestas por reserva. No campo público patientId, búsqueda de coincidencias clínicas o creación automática de pacientes/historias. El calendario privado muestra los datos administrativos únicamente al médico propietario. Documento: validación de formato de DNI argentino de 5–8 dígitos o pasaporte internacional; país validado como región/país ISO. No verificación oficial de identidad ni nuevos formatos nacionales inventados; [referencia SISA](https://sisa.msal.gov.ar/sisadoc/docs/050202/cmdb_if_ficha.jsp).

## Motor, derechos y seguridad
Una extracción acotada de `applyBooking` comparte el motor M4 entre creación privada y pública. Misma disponibilidad M3, lock del workspace, control de derechos, exclusión GiST entre sedes, evento y recibo idempotente. Publicación/formulario se revalidan bajo el mismo lock. Actor de evento `PUBLIC_BOOKING`, nunca una identidad médica simulada. Un cambio de disponibilidad o publicación compite de manera serializable con la reserva.

Decisión expresa del propietario: capacidad comercial **independiente** `online_booking_enabled` boolean, ausente/false=no acceso. Sin valores por tier ni nuevas cantidades/precios/planes. Solo BASIC/PRO/UNLIMITED pueden configurarla. El alias legacy ENTERPRISE conserva gestión de sedes M2, pero no hereda esta nueva capacidad. Billing agrega endpoint interno firmado `/internal/doctorchart/owners/{owner}/entitlements-v2` con `onlineBooking`; v1 conserva exactamente sus ocho campos y los consumidores legacy reciben sus claves originales. PUT administrativo conserva las demás cuotas y la capacidad al editar mapas legacy. Configuración real y editor visual comercial de esta capacidad quedan pendientes; ninguna configuración real fue cambiada.

Se corrigió una regresión reproducida con una lease v1 aún válida y una v2 nueva con cupo 0: antes podía mezclarse capacidad y cupo de snapshots distintos. Ahora **toda mutación pública utiliza una sola snapshot v2** para capacidad, cobertura y cuota, con el fence M2, sin doble autoridad. Repro fallida histórica `lease-repro.jsonl`; regresión y suite final aprobadas. Caché vigente/verificable/no invalidada permite continuidad ante Billing caído; sin lease apta devuelve 503. Nunca se extiende cobertura comercial.

Gateway allowlist por método y segmentos UUID, sin publicar namespaces completos; descarta bearer y headers de identidad del navegador en operaciones públicas. Backend exige secreto Gateway e identificador de red HMAC resuelto con la política TrustedProxy existente. Sin cuenta de servicio pública. Body 16KiB, no transfer-cache, no-store/private/noindex/no-referrer, sin PII en URLs ni logs. Límites SQL compartidos por réplica: 120 lecturas/20 desafíos/10 altas por minuto y red anonimizada; Retry-After 60. Proof-of-work SHA256 18 bits, firma/expiración/ligadura a red, uso único persistido con la reserva; reclamación concurrente de proof resuelta atómicamente con UNIQUE/ON CONFLICT. Reintento de misma clave/payload devuelve el resultado original antes de revalidar proof, sin duplicar eventos.

## PostgreSQL
Nueva migración **0008**, cuatro tablas de publicación/sedes públicas/detalles versionados/contadores. Esquema 8: **34 tablas, 32 FK**. Baseline y 0002..0007 intactos. Fingerprints construidos desde SQL inmutable en bases vacías: PG17 `1c8a843fd50a7fbeb64890397d6a22a4853ba30cc8f85e10f285c69eb3bbc973`; PG18 `85ab5ab6bc2fea17c5b06ee8328c4bb838bbb8e82e9f875b14b844a94770aeaf`. Fresh/upgrade M5→8 convergen, con SQL fallido, drift y seed tardío/rollback/reintento probados, datos clínicos y reservas M5 conservados. Billing no cambia esquema. Se ajustó exclusivamente un test de CRLF de Billing para generar CRLF canónico desde LF/CRLF, sin tocar su baseline ni runtime.

## Pruebas finales
| Comprobación ejecutada | Resultado |
|---|---|
| Angular completo | **763 PASS**, 92 archivos, 0 FAIL/SKIP |
| Angular development y production/deployment, SEO/hosting | PASS; clave pública sintética solo para compilación, sin despliegue |
| Hosting/configuración estática | **8 PASS** |
| DoctorChart `go test -p 1 ./...` | **536 casos/subcasos PASS**, 0 FAIL; 6 externos omitidos |
| Reservas privadas/públicas `-race` | **32 PASS**, sin carreras ni SKIP |
| Migración pública PostgreSQL18 | **5 PASS**, sin SKIP |
| Billing completo con opt-in/DSN descartable válidos | **529 PASS**, 0 FAIL; 6 externos omitidos |
| Gateway completo | **356 PASS**, 0 FAIL; 15 externos omitidos |
| Go vet/build en los tres repositorios Go | PASS |
| Aplicación real en Chrome + servicios reales descartables | **13 recorridos PASS** |

Recorridos: publicación desde UI médica; enlace directo/refresco con sesión médica y sin redirección; búsqueda con cinco filtros y proyección autorizada; reserva anónima real y replay; aparición en calendario con respuestas e aislamiento entre médicos; versión de formulario concurrente y borrador preservado; dos visitantes (201/409); escritorio/móvil/oscuro/SEO; creación real desde móvil y dos sedes seleccionables con zonas diferentes; Billing detenido con lease válida y luego 503 tras invalidación legítima; revocación comercial/despublicación conservando reservas; 429 persistente; ausencia de documento/email de solicitante en logs/respuestas públicas/URLs.

Supabase Auth oficial aislado emitió sesiones signup/password para los médicos/administración/servicio de ensayo. Los visitantes no tuvieron cuenta ni JWT; siguen existiendo exactamente los 7 usuarios iniciales de Auth después de las reservas. No se fabricaron JWT o headers de identidad para aprobar autenticación. Tres reservas públicas finales, snapshots poblados de pacientes/encuentros idénticos antes/después. Capturas revisadas a 1500px, móviles emulados 390/320px y oscuro; no se afirma prueba en teléfono físico.

Omisiones no contadas PASS: 6 DoctorChart (sincronización legacy/fixture efectivo externo/converter/S3); 6 Billing (preview manual, fixtures cruzados históricos, soporte live); 15 Gateway (fixtures adicionales Pointly/Billing/AI/SMTP/previews). Ningún caso nuevo de M6 se omitió. El listado exacto por nombre está en `verification-summary.json`; la integración M6 real se ejecutó por separado, sin convertir esos SKIP en PASS.

Evidencia local fuera de Git: `C:/Users/gcast/Documents/GitHub/doctorchart-m3/.fixtures/m6/`: final-accepted.jsonl, race-accepted.jsonl, pg18.jsonl, billing-closing.jsonl, go-gateway.jsonl, angular-final.log, static-tests.log, logs de builds/vet y verification-summary.json. `integration/`: m6-results.json, m6-http-statuses.json (sin cuerpos/credenciales), versions-verified.json y capturas appointments-*.png. Credenciales/sesiones sintéticas privadas no versionadas. Contenedores/servidor propios retirados al cierre verificando task/labels; evidencia preservada.

## Límites y continuidad
Confirmación visible solo en memoria; guardar la constancia antes de refrescar. No hay enlace de consulta/cancelación pública, verificación oficial de documento/email, recordatorios, pagos, staff, apps nativas, videollamadas o espera automática. Antiabuso con proof/rate-limits no equivale a verificar identidad humana; configuración productiva de red/TrustedProxy y activación comercial requieren autorización independiente. Nuevos formatos documentales locales y UI comercial del flag quedan en backlog, sin cuotas propuestas. Ninguno bloquea el recorrido M6 autorizado.

Las cinco main habituales permanecen limpias en sus SHA previos: DoctorChart Go d5a89e0; Angular 1419697; Billing bf65c04; Bycode frontend 322bf23; Gateway 55197fa. PUBLIC_BOOKINGS_ENABLED backend y PUBLIC_APPOINTMENTS_ENABLED Angular siguen false por defecto, al igual que módulos previos. Solo el fixture los activó. Sin Cloudflare remoto, GitHub remoto, push, merge, despliegue o migraciones/datos reales. Próximo paso: aceptación del propietario; **M7 no iniciado**.
