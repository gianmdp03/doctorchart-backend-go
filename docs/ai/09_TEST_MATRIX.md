# Matriz de tests (seleccionar por fase; nunca marcar OK sin evidencia)
**No ejecutada durante R0 documental.** Los casos de agenda, cuotas, staff y público corresponden a fases futuras; no son requisitos de implementación de M1.
## Protección de clínica existente
- [ ] El médico conserva el mismo paciente/historia al atender jueves en A y viernes en B, sin crear copias por consultorio.
- [ ] El alta clínica no exige ningún lugar de atención y el wizard funciona sin crear `clinics`.
- [ ] Consultas, estudios, recetas, vacunas, documentos, archivos, dashboard y borradores siguen visibles por médico/paciente; regresiones clínicas pasan.
- [ ] No se agregó `patients.clinic_id` ni filtros clínicos por ubicación, tampoco como asociación opcional.
- [ ] Nadie ajeno al médico accede a historias, especialmente secretarias y público.

## Esquema y límites
- [ ] Migraciones desde baseline con datos conservan IDs, relaciones y archivos; instalación limpia llega al mismo catálogo.
- [ ] Checksums, drift, versiones desconocidas e inicializaciones simultáneas se tratan de forma segura.
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
