# M5 — calendario médico validado en ensayo
30/10/2026. M4 aceptado por el propietario en 7fe9bd6; M5 autorizado y completado para su aceptación. M6 no autorizado. Sin integración a main ni producción.

## Ramas y versiones
Se conservaron los clones normales existentes en `C:/Users/gcast/Documents/GitHub/doctorchart-m3/`, sin worktrees ni reorganización. Rama independiente `codex/m5-calendar` en ambos repositorios:
- Backend: base exacta M4 `7fe9bd6870cb13921f6031498e912aa8ef2a3134`; contrato previo `9bd148f`; aplicación `c63f5f3`; corrección de aserción de prueba `25a8c6f`. Cierre documental posterior en HEAD.
- Angular: base M3 `d0b7a458a7a0c1301cd7e77f35be325aa8d5e6c1` (M4 no había modificado Angular); aplicación `3f09d8d`, ajuste móvil/hosting `0de8490`. Cierre documental posterior en HEAD.
- Backend M3 `ff5053b`, M4 `7fe9bd6`, ramas turnos y ramas anteriores preservadas. M4 contiene M1/M2 completos. Referencias M5 adicionales conservadas mediante fetch del clon LOCAL en los repositorios habituales, sin contactar GitHub ni mover sus main.

## Resultado y contrato
Calendario privado `/calendar`, día/semana, navegación con app-date-picker, una sede o todas, consultas paginadas completas, disponibilidad diferenciada de reservas y todos sus estados. Horas/fechas civiles por zona de cada sede, offset visible para desambiguar DST. Fechas españolas dd/MM/aaaa, sin controles nativos date. Acceso desde navegación de DoctorChart, responsive, claro/oscuro, carga/vacío/error y editor con cierre visible al desplazarse.

Crear, detalle/eventos, cancelación, reprogramación y transiciones M4 mediante transporte HTTP real. Se mantienen autoridad Go, control de versión y recibos idempotentes. Doble clic bloqueado; reintento de la misma operación conserva clave ante fallo de red. 409 informa y conserva campos, exige versión/slot actualizado; respuestas tardías no cambian otro editor/filtro/propietario. Derechos con vencimiento y refresco periódico, sin habilitación por defecto; downgrade conserva estados permitidos y bloquea altas/reprogramación.

[M5-CALENDAR-v1](contracts/M5_CALENDAR_API.md) extiende M4 sin endpoints nuevos: `attendeeName` opcional en creación y aditivo en Booking, separa persona atendida de `contactName/contactPhone` del solicitante. M4 omitido implica mismo nombre. Vinculación clínica opcional usando búsqueda del médico existente, nunca alta o modificación de pacientes. Eventos/recibos históricos permanecen intactos y Angular admite el fallback histórico.

Migración 0007 agrega columna nullable y protección de inmutabilidad; sin backfill ni modificación de pacientes, baseline o 0002..0006. Catálogo 7 independiente: PG17 `c2956e5b19d3d605b89bc94916cd5e9e6c061faa4ee4b776aa94e71e3279f06c`; PG18 `39069383dc0633dc8dedb3980de63057f98ad576cab1525e396f1b6ac7a7ce5b`. Conserva 30 tablas/28 FK. Rollback probado es transaccional ante fallo: un binario M4 rechaza esquema 7; una recuperación real requiere respaldo, binario compatible y autorización, nunca rebajar marcador/borrar datos.

## Pruebas ejecutadas
| Comprobación | Resultado |
|---|---|
| Angular `ng test --watch=false` | 747 PASS, 87 archivos, 0 FAIL/SKIP |
| Angular build development | PASS |
| Build production/deployment + SEO/hosting | PASS con clave pública sintética de compilación; no credenciales ni publicación reales |
| Hosting estático/configuración `node --test` | 8 PASS, incluidos deep links calendar/schedule y noindex |
| Go `go test -p 1 ./... -count=1 -json` | 516 casos/subcasos PASS, 0 FAIL, 6 externos omitidos |
| Go reservas `-race -run '^TestBooking'` | 18 casos/subcasos PASS, sin carreras ni SKIP |
| Migración M4→7 PostgreSQL 18 | 3 casos/subcasos PASS; éxito, fallo tardío/rollback, reintento y fresh=upgrade |
| Go vet/build | PASS |
| Navegador real con aplicación Angular completa y servicios aislados | 11 recorridos PASS |

Las regresiones Angular cubren seis endpoints/interceptores, IDs/versiones decimales sin conversión numérica, paginación, permisos vencidos/revocados, reintentos sin perder campos, doble clic, 409, respuestas tardías, limpieza por propietario y fechas/horas DST. Go agrega solicitante distinto de persona atendida, compatibilidad M4, inmutabilidad SQL, sin creación clínica; conserva todas las pruebas de concurrencia/integridad M4 y pruebas de migración desde versiones históricas.

Integración: aplicación real con router, guards de plan/onboarding/calendario, shell, AuthService e interceptores; no harness que los reemplace. Signup/password reales de Supabase Auth aislado, Gateway `55197fa`, Billing `80c171c`, PostgreSQL17 tmpfs y usuarios/cuotas sintéticas BASIC/PRO/UNLIMITED. No JWT fabricados ni headers de identidad para acreditar login. Solo snapshot descartable con flags activos, configuración loopback y sin hidratación SSR para servir el ensayo CSR. Se verificaron 240 archivos de aplicación contra Angular final; backend solo difiere de su snapshot por la aserción de test corregida.

Recorridos: login/guards; sedes UTC y America/New_York con dos 01:30 durante DST; alta solicitante/persona distintos; reprogramación entre sedes; cancelación y liberación M3; dos pestañas compitiendo por último slot (201/409 y borrador preservado); estados confirmado/en consultorio/atendido; otro médico recibe 404 y snapshots clínicos poblados idénticos; escritorio/móvil/oscuro sin overflow; vínculo clínico opcional desde móvil con búsqueda restringida al propietario; versión concurrente 409 seguida de recarga vigente.

Seis omisiones no declaradas PASS: `TestLimitsSyncDoctorChartFixture` (fixture legacy opt-in); `TestActualBillingEffectiveRights` (opt-in adicional; integración real ejecutada por separado); `TestLiveStudyHTTPConverterAndPersistence`, `TestLiveConverterLocalStorage`, `TestLiveConverterOrientationAndSpecialPreservation`, `TestLiveConverterS3AndApplicationIsolation` (converter/S3 externos no iniciados, ajenos a M5). Hubo correcciones de fixtures históricos por versión 7 y del código esperado en el sobre de error; la ejecución final pasó completa.

Evidencia fuera de Git: `C:/Users/gcast/Documents/GitHub/doctorchart-m3/.fixtures/m5/`: final.jsonl, race.jsonl, pg18.jsonl, angular-all.log, angular-production-build.log, static-tests.log, vet.log, build.log; `integration/`: m5-results.json, m5-http.json, versions-verified.json y capturas calendar-desktop.png, calendar-dark.png, calendar-mobile-390.png, calendar-mobile-320.png, calendar-mobile-form.png. Sesiones/contraseñas sintéticas privadas no versionadas. Contenedores y servidor local propios retirados tras verificar ownership; evidencia conservada.

## Archivos y continuidad
Angular: nuevo módulo features/calendar (modelos, servicio, componente y pruebas), guard/flag, router/sidebar, exclusión exacta bookings del contexto clínico timeZone, configuración/headers/hosting privado y prueba de deep links. Backend: nombre administrativo, migración 7/runner/pins, pruebas de upgrade/rollback y actualización de expectativas históricas. No se modifica lógica clínica, planes, precios, cuotas reales, Gateway o Billing.

Las cinco main habituales permanecen limpias y en sus SHA originales: DoctorChart Go d5a89e0; Angular 1419697; Billing bf65c04; Bycode frontend 322bf23; Gateway 55197fa. CALENDAR_ENABLED/CLINICS_ENABLED/SCHEDULES_ENABLED Angular continúan false; BOOKINGS_ENABLED/SCHEDULES_ENABLED Go false por defecto. Sin migraciones reales, Cloudflare, GitHub remoto, merge, push o despliegue.

Límites deliberados: sin drag-and-drop ni actualización en vivo de reservas (botón Actualizar; permisos se refrescan cada 30s y vencen localmente). El buscador clínico legacy usa IDs numéricos: se rechazan vínculos no representables de forma segura, sin redondearlos; reservas/consultorios/versiones permanecen strings. Portal /appointments, staff, correo, pagos y marketing excluidos. Próximo paso: aceptación del propietario; M6/activación/integración requieren autorización independiente.
