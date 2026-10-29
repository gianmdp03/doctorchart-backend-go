# Matriz de tests (seleccionar por fase; nunca marcar OK sin evidencia)
**R0 fue documental. M1 ejecutó los casos marcados, con evidencia en el handoff.** Agenda, cuotas, staff y público corresponden a fases futuras; no se declaran implementados.
## Protección de clínica existente
- [ ] El médico conserva el mismo paciente/historia al atender jueves en A y viernes en B, sin crear copias por consultorio.
- [x] El alta clínica no exige ningún lugar de atención y el wizard funciona sin crear registros de lugares.
- [x] Consultas, estudios, recetas, vacunas, documentos, archivos, dashboard y borradores siguen visibles por médico/paciente; regresiones clínicas pasan.
- [x] No se agregó `patients.clinic_id` ni filtros clínicos por ubicación, tampoco como asociación opcional.
- [ ] Nadie ajeno al médico accede a historias, especialmente secretarias y público.

## Esquema y límites
- [x] Migraciones desde baseline con datos conservan IDs, relaciones y archivos; instalación limpia llega al mismo catálogo.
- [x] Checksums, drift, versiones desconocidas e inicializaciones simultáneas se tratan de forma segura.
- [ ] Dos altas de lugares para el último cupo producen una sola admisión (si cuota aprobada =1).
- [ ] Downgrade/vencimiento no elimina historias, reservas ni lugares; errores comerciales no conceden infinito.
- [ ] Duplicado/desorden de webhook y Billing caído tratados según contrato aprobado.

## Seguridad de secretaria
- [ ] Sesión staff no acepta rutas clínicas ni JWT médico como sustituto; header falsificado rechazado.
- [ ] Staff solo opera lugares autorizados, sin acceso a formularios sensibles ni `patient_id` interno.
- [ ] Cookies CORS/CSRF, recuperación, rate limiting, revocación y múltiples médicos según alcance.

## Turnos
- [ ] Horarios de consultorios A y B no permiten solapamiento de reservas para el mismo médico.
- [ ] Disponibilidad con varias franjas, excepciones y zonas IANA es correcta.
- [ ] Reserva pública/manual concurrente sobre mismo slot confirma a uno solo.
- [ ] Solicitud hecha por madre para hijo no crea ficha clínica ni copia su DNI al niño.
- [ ] Si el médico vincula un turno al paciente, ese paciente pertenece al médico; **no se exige mismo consultorio clínico**.
- [ ] Cancelación conserva trazabilidad y libera disponibilidad; exceptions no cancelan silenciosamente.
- [ ] Versiones de formulario conservadas; spam, abuso y datos sensibles protegidos.

## Rutas y UI — fases futuras
- [ ] Portal público usa `/appointments` sin alias ni redirecciones; segmentos de médico y consultorio se verifican solo contra contrato aprobado.
- [ ] Selectores usan `app-date-picker`, formato argentino `dd/MM/aaaa` e ISO únicamente en la frontera de datos.

M1 verifica filas/referencias de medios; no reejecuta la integración del converter externo ni un despliegue. El flujo del mismo paciente entre agendas sigue pendiente de las fases de turnos.

## Correcciones posteriores a Argon
- [x] Startup real habilitado con `DOCTORCHART_STARTUP_TEST_DATABASE_URL` y `doctorchart_startup_test`; 25 tablas, seeds y health/ping ante caída/recuperación PostgreSQL; PASS sin SKIP.
- [x] Baseline 1 con seeds reales → `bootstrap.Run` → 2 y reinicio; configuraciones administradas preservadas, una migración y cero lugares inventados.
- [ ] M2: validación IANA en Go además del CHECK SQL, pendiente de fase autorizada.
Resultados, entornos y cinco pruebas externas omitidas: [correcciones](reviews/M1_CORRECTIONS.md).
