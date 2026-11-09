# M3 — entrega funcional
30/10/2026 (Argentina). **M3 IMPLEMENTADO Y VALIDADO para revisión del propietario. M4 NO autorizado.** Sin activación productiva, pushes, merges a main ni cambios Cloudflare.

## Resultado
El médico puede seleccionar un consultorio, configurar días y varias franjas, duración, anticipación/horizonte, cierres o franjas especiales por fechas y consultar disponibilidad diaria/semanal. Persistencia versionada; cálculo dinámico sin slots materializados. Horas inexistentes DST se omiten y horas repetidas conservan instantes UTC distintos; el offset distingue la hora en la interfaz.
Los solapamientos entre sedes del mismo médico se rechazan bajo el lock del workspace; también se revalidan cambios de zona, archivo/restauración y cada consulta. Lecturas de disponibilidad mantienen una vista coherente bloqueando cambios concurrentes del workspace. Excepciones reemplazan la semana habitual.
Pacientes, consultas, adjuntos y demás recursos clínicos permanecen por médico; no hay patients.clinic_id, reservas ni creación automática de pacientes.

Contrato congelado [M3-AVAILABILITY-v1](contracts/M3_AVAILABILITY_API.md). GET/PUT /clinics/{id}/schedule y GET /clinics/{id}/availability. M2-CLINICS-v1 intacto. Permisos y snapshots efectivos de M2, sin nuevas cuotas ni cantidades comerciales.

## Git y aislamiento
Trabajo en clones normales, SIN worktrees:
- C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-backend-go
- C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-frontend
Rama codex/m3-availability desde turnos aprobado c3bf46c/471daaf. Origins de los clones apuntan a carpetas locales, no a GitHub. Los commits también se conservan en esa rama de los repositorios habituales mediante fetch LOCAL, sin cambiar sus checkouts main.
Backend: contrato 51b96b8, núcleo 939d91a, implementación 99258c1, regresiones 3f61186/dcb016d y corrección del fixture de pruebas 426026f. El código de aplicación usado en integración corresponde a 99258c1; los commits posteriores son pruebas/documentación.
Frontend: flag 11780bc, editor/servicio 73e3b62, texto de conflicto fa88af1 y protección de fecha visible inválida 72ff632. Commits finales de cierre: consultar HEAD de la rama, sin reescritura de historia.
main habituales intactos: Doctor Chart Go d5a89e0; Angular 1419697; Billing bf65c04; Bycode frontend 322bf23; Gateway 55197fa. turnos originales c3bf46c/471daaf/80c171c/9428426 sin mover. M1 y M2 preservados.

## Esquema y seguridad
Migración NUEVA 0005_clinic_schedules.sql; versión final 5, 27 tablas y 22 FK. schema.sql y 0002..0004 no se modificaron. Agendas en clinic_schedules, configuración JSONB y versión BIGINT independiente del consultorio.
Catálogos construidos independientemente desde SQL publicado, antes de integrar el runner:
- PostgreSQL17: bd320db87155ffabccdc544e1aa5358d36f522cc5708c37613717cf253dd8e08
- PostgreSQL18: 791155c6db3d29619ae6be407e12fd929249dfc2fd6dddbe130358513be5592a
Se verificó fresh=upgrade M2→M3, reinicio conservador, fallo SQL/drift/seed tardío con rollback y reintento, conservación de datos/identidad e integridad. No hay DOWN automático tras commit: un binario M2 rechaza versión 5; no se ofrece un downgrade destructivo.
SCHEDULES_ENABLED=false por defecto en Go y Angular; sin flag el backend no registra endpoints M3. CLINICS_ENABLED=false. Solo el backend descartable y el entrypoint de ensayo accedieron a M3.

## Pruebas y evidencia
Evidencia local persistente: C:/Users/gcast/Documents/GitHub/doctorchart-m3/.fixtures/.
- backend-linux-tests-verified.jsonl: suite completa Go1.27.3 / Linux / PostgreSQL17, **488 PASS y 0 FAIL**, incluidos casos/subcasos. Startup y migraciones sin SKIP; prueba concurrente 16 callers PASS en 0,77s.
- growth-proof.jsonl: TestGrowthHTTPPostgres **PASS** separado, corrigiendo el nombre del opt-in del fixture. No se alteró código de crecimiento.
- backend-m3-race.jsonl: **17 casos/subcasos M3 PASS** con -race, sin carreras. No se afirma haber ejecutado toda la suite histórica con -race.
- backend-pg18-m3-proof.jsonl: **5 casos/subcasos PASS** de upgrade, rollback y conservación sobre PostgreSQL18 Debian.
- backend-vet.log y backend-build.log: go vet ./... y go build ./... PASS en Linux.
- frontend-full-final.log: **732 PASS / 84 archivos**.
- frontend-schedules-final.log: **12 PASS / 3 archivos**, transporte HTTP, BIGINT string, errores, permisos vencidos, borradores, fecha inválida y respuestas tardías.
- frontend-production-build.log y frontend-build.log: build producción con configuración pública sintética y desarrollo PASS.
- integration/m3-browser-results.json: **8 grupos PASS**, login REAL Supabase Auth oficial descartable, Angular con servicios/interceptores reales → Gateway 55197fa → backend M3 → Billing 80c171c → PostgreSQL. Guardado de franjas, disponibilidad 8 slots, semana, vacaciones, conflicto de versión con borrador, aislamiento y conflicto entre sedes. Sin JWT de usuario fabricados ni autenticación positiva mediante headers de prueba.
- Capturas integration/schedule-desktop.png, schedule-mobile-320.png, schedule-mobile-390.png y schedule-dark.png. Sin overflow 320/390; oscuro comprobado con :root.dark. Emulación, no teléfono físico.

Seis pruebas externas permanecen omitidas y no se declaran PASS: TestLimitsSyncDoctorChartFixture; TestLiveStudyHTTPConverterAndPersistence; TestActualBillingEffectiveRights; TestLiveConverterLocalStorage; TestLiveConverterOrientationAndSpecialPreservation; TestLiveConverterS3AndApplicationIsolation. Son sync legacy/conversor/S3 o el opt-in específico de interoperabilidad M2; el recorrido M3 sí usó Billing y autenticación reales en su fixture.
Fallos de preparación corregidos: el reenviador TCP de Docker retenía sesiones canceladas y bloqueaba el test de arranque. Se demostró con pg_stat_activity (idle in transaction), se corrigió cierre bidireccional del helper y el test pasó con los límites/timeouts originales; no se debilitó el runner. También se corrigió un fixture que comprobaba preservar una agenda sin haberla sembrado. Los logs fallidos previos se conservan como antecedentes, no como estado final.

## Límites y siguiente paso
La validación de guardado cubre el horizonte técnico máximo de un año y bordes de zonas; cada consulta vuelve a detectar conflictos en sus fechas, por posibles cambios futuros de tzdata. No es un motor de reservas ni de feriados automáticos: las excepciones son explícitas.
No se cambiaron reglas Cloudflare/hosting. La activación futura debe incluir soporte del deep link /clinics/:id/schedule en el hosting estable y revisar permisos/cupos reales con autorización del propietario; el harness no acredita una publicación de esa ruta.
Los checkouts habituales main pueden seguir usándose. No ejecutar el binario M3 con una DSN real por cambiar de rama: el bootstrap migraría al esquema 5. Sin autorización no activar flags, respaldar por push, desplegar ni comenzar M4.
Recursos temporales y cierre: .fixtures/cleanup.json. No versionar .fixtures/private-state.json, integration/private-state.json, archivos .env, certificados o sesiones.

