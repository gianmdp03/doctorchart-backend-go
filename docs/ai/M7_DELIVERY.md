# M7 — secretarias y administración delegada

31/10/2026. M6 aceptado; M7 implementado y validado en ensayo para aprobación del propietario. Sin integración a main, activación comercial, despliegue ni M8.

## Versiones y alcance

Ramas normales `codex/m7-secretaries` en `C:/Users/gcast/Documents/GitHub/doctorchart-m3/`. Sin worktrees. Bases M6 exactas: backend 21b910f (M5 89a2564/M4 7fe9bd6/M3 ff5053b/M2 c3bf46c ancestros verificados), Angular 31f68b4 (M5 a3dfb0d/M3 d0b7a45/M2 471daaf), Billing 3dc9874, Gateway 31ee7fc. Turnos y ramas anteriores conservadas.

| Repo | Aplicación M7 | Cierre |
|---|---|---|
| doctorchart-backend-go | 51ed413 + **a70a82b** | HEAD documental posterior |
| doctorchart-frontend | a0298dd + **06f421f** | HEAD documental posterior |
| bycode-backend-go | **df1c063** | HEAD documental posterior |
| api-ai-gateway-go | **20d03ae** | HEAD documental posterior |

Contrato previo 21d3919: [M7-SECRETARIES-v1](contracts/M7_SECRETARIES_API.md). Nuevas refs M7 se preservan mediante fetch LOCAL en los repositorios habituales, sin GitHub ni cambio de checkout/main.

## Funcionalidad y fronteras

Médico Supabase: `/secretaries` permite invitar, renovar códigos pendientes, asignar sedes/permisos con versión y revocar su relación. Código de un uso de 24h compartido manualmente por canal privado, nunca URL. Cada email identifica una cuenta local compartida entre médicos; activar otra relación exige demostrar su contraseña actual. Un médico no puede restablecer la contraseña global de una secretaria.

Secretaria: `/staff/login`, `/staff/activate`, `/staff/recover`, `/staff/calendar`. Agenda diaria/semanal por médico/consultorio, alta manual con solicitante y atendido distintos, cancelación/liberación, reprogramación en el consultorio seleccionado y estados M4. Fechas por app-date-picker y zona IANA de la sede. Se reutilizan helpers de fecha/estado M5; no se reutiliza su componente médico, porque inyecta PatientService. Ningún servicio clínico en el calendario administrativo. Identificación profesional usa nombre M6 configurado; sin ese dato muestra «Profesional asignado», sin fragmentos de IDs.

API médica GET `/secretaries`, POST `/secretaries/invitations`, PUT/DELETE `/secretaries/{id}`. API local `/staff`: activate/login/recover/session/logout/password/assignments; consultas por owner/clinic y mutaciones de bookings. Lista de operaciones exacta en Gateway y backend, no bypass por namespace. Gateway descarta bearer e identidad médica en requests staff y genera contexto de red autenticado. Otros endpoints continúan exigiendo Supabase. Las cookies locales no autentican pacientes/recetas/estudios/documentos/workspace/planes.

Contraseñas bcrypt costo12 y 12–72 bytes; tokens de 256 bits, solo hashes SHA-256 persistidos. Cookie __Secure-dc_staff Secure/HttpOnly/SameSite=Strict, sin Domain, Path=/api/doctorchart/staff, vencimiento absoluto8h. Origin HTTPS exacto configurado y CSRF de sesión en mutaciones; login/activación/recuperación verifican Origin. Límite persistente por cuenta/red en ventanas15min, respuestas genéricas, Retry-After y auditoría sin secretos. Cambio de contraseña exige la actual y revoca otras sesiones; recuperación personal consume/rota código y revoca todas. Código personal mostrado una vez; ningún reset por mera autorización de un médico.

Permisos `view/create/cancel/reschedule/status`, por relación y sede, sin horarios ni autoconcesión. Revocación y cambios de asignación se serializan con el workspace lock M4. Sesión y permisos se revalidan dentro de transacción, incluido replay; recuperación/logout sincronizados mediante lock de cuenta. Una reprogramación comprueba sede origen y destino. Reutiliza applyBooking M4, disponibilidad M3, exclusión GiST, fence comercial, eventos e idempotencia; claves aisladas por cuenta/relación. Actor `STAFF:<uuid>`. Proyección administrativa elimina patientId; no se acepta patientId ni se crea/modifica información clínica.

Billing: `secretary_management_enabled` independiente, ausente/false deniega, solo BASIC/PRO/UNLIMITED; ningún valor por plan/precio/cantidad nuevo. Derechos del propietario mediante snapshot firmado v3, misma cobertura/epoch/HMAC/expiración, sin plan de secretaria. v1 de ocho campos y v2 de nueve intactos; PUT legacy conserva capacidades/cuotas. Snapshot verificable vigente no invalidado permite continuidad con Billing caído; sin él503. Revocación comercial se hace efectiva tras webhook existente o expiración de lease (máximo1min y nunca más allá de cobertura); no afirmar invalidación síncrona en el PUT, porque Billing usa outbox. Revocación de relación médica sí es inmediata y no depende de Billing.

## PostgreSQL y pruebas

Migración nueva0009, baseline y0002..0008 intactos. Siete tablas locales de cuentas/relaciones/sedes/invitaciones/sesiones/límites/auditoría; UNIQUE compuesto adicional en clinics para FK que impide asignaciones cruzadas. **Esquema9:41 tablas/40 FK**. Pins construidos independientemente desde SQL publicado en bases vacías: PG17 `485cfb1f92969b5c648651c4bee4579799cc05b41dacff101d9a5583b741ee1f`; PG18 `7f4795bd498e2910a746a9e8f02cceedce5529e92f89c9f1aab46ae71b40ca80`. Upgrade8→9/fresh, SQL fallido/drift/seed tardío, rollback transaccional/reintento/reinicio y conservación clínica verificados. Expectativas históricas de versión/contador se actualizaron; snapshots de conservación excluyen únicamente tablas nuevas. No DOWN destructivo: binarios antiguos rechazan9; recuperación real requiere binario compatible o restauración autorizada, nunca bajar marcador.

| Ejecutado | Resultado |
|---|---|
| Go DoctorChart completo sobre51ed413 | **553 casos/subcasos PASS**,0FAIL,6 externos omitidos |
| Regresión staff tras proyección de nombre a70a82b | **7 PASS**,0FAIL/SKIP; único cambio posterior es SELECT/label, no motor/auth |
| Go Billing completo | **533 PASS**,0FAIL,6 externos omitidos |
| Gateway completo, vet/build | **357 PASS**,0FAIL,15 externos omitidos |
| Go DoctorChart vet/build, binario Linux final | PASS |
| M4/M7 `-race` | **24 PASS**,0FAIL/SKIP; sin carreras |
| PostgreSQL18 migración/arranque | **5 PASS**,0FAIL/SKIP |
| Angular final | **769 PASS**,94 archivos,0FAIL/SKIP |
| Builds development y production/deployment + SEO | PASS; clave pública sintética solo compilación |
| Hosting/configuración, incluyendo nuevas rutas directas | **8 PASS**,0FAIL/SKIP |
| Chrome + Angular/Gateway/Go/Billing/PostgreSQL/Supabase Auth oficiales aislados | **14 recorridos PASS** en versiones finales |

Recorridos integrados: login médico e invitación desde UI; activación local/cookie real HTTPS; alta administrativa; aparición en calendario médico M5; reprogramación/cancelación/liberación; rechazo de sede ajena/patientId/APIs clínicas/headers falsos; CSRF/Origin/cookie adulterada; concurrencia201/409/versión obsoleta/estados; escritorio/móviles390/320/oscuro; una cuenta y dos médicos con permisos independientes; revocación inmediata de relación; revocación comercial y webhook real; recuperación/login/logout locales; conservación clínica y ausencia de secretos en stdout/stderr. Exactamente7 cuentas Supabase originales y1 cuenta local: ninguna secretaria fue creada en Supabase. No JWT fabricados ni headers de identidad para declarar autenticación aprobada.

27 omisiones externas no son PASS:6 DoctorChart (converter/S3/fixtures legacy),6 Billing (previews/soporte/fixtures cruzados),15 Gateway (Pointly/AI/SMTP/fixtures periféricos). Ninguna prueba M7 omitida. Evidencia exacta por nombre y hashes en `.fixtures/m7/verification-summary.json`; logs principales backend-final.jsonl, staff-closing.jsonl, billing.jsonl, gateway.jsonl, race.jsonl, pg18.jsonl, angular-final.log, static-final.log. Integración: integration/results.json, verification.json, versions-verified.json, http-statuses.json y capturas. Evidencia fuera de Git; credenciales sintéticas privadas no versionadas. Contenedores/servidor propios se retiran al cierre, registros conservados.

## Límites y continuidad

- Invitaciones manuales; sin envío email (M8). Recuperación exige código personal guardado; perder contraseña y código no habilita un bypass por médico. Proceso de soporte adicional requiere diseño/autorización posterior.
- Reprogramación UI dentro de la sede seleccionada; API permite traslado entre sedes asignadas del mismo médico. Sin edición de horarios, autoservicio clínico, secretarias en Supabase, pagos ni nuevos módulos.
- Editor visual comercial del nuevo boolean queda pendiente; API existente preparada y probada con configuración sintética. No se habilitaron derechos comerciales reales.
- Prueba en Chrome de escritorio con emulación móvil, no dispositivo físico. HTTPS/Gateway reales en loopback18443 y Auth18543, no Cloudflare/dominios productivos; activación remota exige autorización y validación del enrutamiento/orígenes reales.
- STAFF_ENABLED backend y Angular permanecen false; backend opt-in exige M4 y STAFF_ORIGIN HTTPS exacto. Módulos previos también desactivados en configuración normal.

Las cinco main habituales limpias y sin cambios M7: DoctorChart Go d5a89e0; Angular1419697; Billing bf65c04; Bycode frontend**89f032c**; Gateway55197fa. Bycode frontend avanzó por un commit ajeno a esta ejecución el31/10 a11:23-03:00, desde322bf23; se preservó sin modificaciones ni integración M7. Sin pushes, merges, resets, worktrees, despliegues, configuraciones remotas ni escrituras/migraciones reales. Próximo paso: aprobación del propietario; **M8 no iniciado**.
