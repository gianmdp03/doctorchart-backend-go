# DoctorChart — M4: reservas contextualizadas

Fecha: 10/11/2026. Continúa [M1](specialty-contexts-m1.md),
[M2](specialty-contexts-m2.md) y [M3](specialty-contexts-m3.md).
Pedido del propietario y estos handoffs son la autoridad para especialidades.
Las auditorías de docs/ai/sources son antecedentes de consultorios, con decisiones
clínicas superadas; no se recuperó su arquitectura descartada.

## Resultado y alcance

Toda reserva nueva obtiene contexto y duración del bloque autorizado por el
motor M3, y conserva snapshots duraderos. Se extendió la transacción común de
reservas internas, públicas y de secretaría. Continúan agenda global, cupos,
versiones, estados, eventos, idempotencia, constancias y outbox.

Un médico sigue teniendo un workspace y un único contexto. Se conserva
doctor_specialty_contexts_single_context_key y el catálogo de Pediatría,
Medicina General y Dermatología. No hay nuevos selectores, especialidades,
wizard, recurrencias, estados de reserva ni motor de disponibilidad. M5 no se
implementó. No se crearon fichas o consultas automáticamente al reservar/vincular.

## PostgreSQL: esquema 14

0014_booking_contexts.sql evoluciona 13→14 mediante el runner existente.
Baseline, migraciones 0002–0013, seeds y catalog.go se conservaron byte por byte
respecto del inicio de M4. Resultado: **47 tablas, 58 FK**.

| Campo/objeto | Garantía |
|---|---|
| bookings.specialty_context_id NOT NULL | FK compuesta al contexto y workspace propietarios; inmutable. |
| bookings.specialty_snapshot NOT NULL | JSON con id decimal string, code y name del catálogo al capturar; inmutable incluso al reprogramar. |
| bookings.clinic_snapshot NOT NULL | name, address privado, publicAddress publicado y timezone; se actualiza explícitamente sólo al reprogramar. |
| bookings.provenance NOT NULL | Snapshot del bloque autorizado o marca histórica LEGACY_CONTEXT_ONLY. |
| bookings_patient_context_fkey | FK (patient_id,workspace_id,specialty_context_id) al paciente; NULL permite reservas administrativas sin ficha. |
| patients_id_workspace_context_key | Clave compuesta de soporte; no modifica pacientes ni documentos clínicos. |
| bookings_context_start_idx | Lectura por contexto/inicio/id; no divide la exclusión global. |

La procedencia SCHEDULE_BLOCK contiene blockId, scheduleVersion, weekday,
exceptionId, from/through, startMinute/endMinute, slotMinutes y timezone. Inicio
y fin reales permanecen en bookings.start_at/end_at. El snapshot identifica una
autorización concreta, sin materializar slots vacíos.

El ID histórico del bloque se conserva dentro del snapshot, deliberadamente
sin una FK viva a schedule_blocks: retirar un bloque pasado/cancelado no debe
borrar, bloquear ni modificar la historia de reservas. Al insertar o reprogramar,
un trigger verifica el bloque existente y su pertenencia a consultorio/contexto/
workspace, snapshot exacto, versión de agenda, duración, alineación civil, día y
excepción aplicables. Los helpers SQL booking_block_snapshot y
booking_clinic_snapshot construyen los snapshots desde filas autorizadas.
El motor Go M3 continúa siendo autoridad de las horas reales y transiciones DST.

El trigger conserva identidad/versión, contactos, atendido y transiciones; impide
cambiar contexto/especialidad, reemplazar/desvincular un paciente ya asociado o
retocar snapshots fuera de una reprogramación válida. El vínculo NULL→paciente
es la única ampliación de la inmutabilidad anterior. Las FK siguen restrictivas.
guard_context_binding además impide reutilizar una especialidad del contexto
cuando ya tiene reservas, incluidas las históricas. Los inserts toman lock
compartido del contexto para excluir una modificación simultánea de su identidad.

El runner conserva pgx, READ COMMITTED, contexto 30s, lock_timeout 10s,
statement_timeout 20s y advisory lock transaccional (17209,3). Verifica identidad,
SQL/checksums, historial y catálogo previo; DDL, backfill, seeds y marcador
confirman juntos antes de HTTP/jobs. El reinicio valida sin adoptar ni reparar.

Pins 14 derivados de bases vacías independientes, validando todos los pins
anteriores durante su construcción:

| Motor | SHA-256 de catálogo |
|---|---|
| PostgreSQL 17.11 | 7fb6b29f2f36ea496c354074f33209168d6ddb67a9395231421631e6a253b704 |
| PostgreSQL 18.6 | 416f9e105d957d54d92641fb0f26659ff40af634a0f2e9fac217b6ebffa628ef |

## Reservas anteriores: política explícita

La migración conserva todas las reservas, IDs, versiones, fechas, estados,
contactos, vínculos existentes, eventos, recibos y mensajes en outbox. No crea
eventos, envía avisos ni consulta agendas para atribuir un bloque histórico.

CONFIRMED futuras, CHECKED_IN, COMPLETED, NO_SHOW y CANCELLED se clasifican por
el **único contexto verificable del workspace al migrar**. Esa clasificación
no demuestra qué especialidad o bloque existía cuando se creó una reserva muy
antigua. Se persiste provenance={kind:LEGACY_CONTEXT_ONLY}, sin blockId. Los
snapshots de especialidad y consultorio de estas filas corresponden al momento
de migración, sin presentarlos como datos originales de creación.

La política incluye reservas sin bloque actual, fuera de la agenda vigente,
creadas antes de cambios y con origen no reconstruible. No se emparejan por
horas parecidas. Continúan ocupando sus intervalos conforme a sus estados.
Una reserva activa incompatible impide guardar horarios hasta una reprogramación
o cancelación explícita; no se corrige silenciosamente. CHECKED_IN conserva sus
transiciones anteriores y no obtiene una reprogramación nueva.

Un propietario sin contexto, paciente con propietario/contexto incompatible o
catálogo estructural alterado provoca rechazo y rollback íntegro. No se inventa
contexto ni se borra una reserva. La siguiente reprogramación válida de una
reserva legacy conserva su contexto/clasificación y captura procedencia exacta
del destino, con evento before=legacy/after=SCHEDULE_BLOCK. No reconstruye el pasado.

## Contratos y transacción de reservas

Se conservan rutas y campos anteriores. Booking incorpora:

```json
{
  "specialtyContextId": "101",
  "specialty": {"id":"2","code":"general_medicine","name":"Medicina General"},
  "clinicSnapshot": {"name":"Centro","address":"Privada","publicAddress":"Publicada","timezone":"America/Argentina/Buenos_Aires"},
  "provenance": {"kind":"SCHEDULE_BLOCK","blockId":"51","scheduleVersion":"7","weekday":1,"exceptionId":"","from":"","through":"","startMinute":540,"endMinute":780,"slotMinutes":20,"timezone":"America/Argentina/Buenos_Aires"}
}
```

Los IDs/versiones del contrato administrativo siguen siendo strings decimales.
GET/listado/detalle devuelven los valores persistidos, nunca el contexto activo
del frontend ni nombres actuales del perfil. Los recibos anteriores a 14 no se
reescriben y pueden carecer de estos campos; Angular contempla ese caso y permite
recargar el detalle vigente. Los documentos de eventos anteriores tampoco cambian.

POST /bookings y POST /bookings/{id}/reschedule aceptan specialtyContextId
opcional. Si se proporciona debe coincidir con el slot; el backend siempre
deriva el contexto real del bloque. La creación no depende de un fallback de
contexto único: el slot ya lo identifica. No se aceptan fin/duración/procedencia
arbitrarios en el body.

Dentro del mismo flujo transaccional se mantienen autorización del canal,
fences comerciales, lock de workspace, locks de consultorio, versión, estado,
revalidación del slot M3 y ocupación global. Se guardan reserva/snapshots, evento,
recibo y notificaciones atómicamente. La exclusión PostgreSQL sigue por workspace
e intervalo [inicio,fin), sin contexto en su clave. Los bloques adyacentes son válidos.

La reprogramación debe mantener el contexto y el snapshot de especialidad.
Puede cambiar de consultorio/bloque/duración dentro de ese contexto: captura el
destino explícitamente y el evento conserva la procedencia anterior y nueva.
BOOKING_CONTEXT_MISMATCH (409) indica que se requiere cancelar/crear para cambiar
de especialidad. La reserva original no se pierde si falla la operación.
Estados, cancelación, cierre y liberación de disponibilidad conservan sus reglas.

La edición de agenda compara también el bloque/contexto/fin persistidos de las
reservas futuras exactas, además de las protecciones M3. Un cambio de nombre,
wizard, publicación o bloque no modifica snapshots existentes.

## Vinculación posterior con paciente

Operación nueva exclusiva del médico autenticado:

```http
PUT /api/doctorchart/bookings/{id}/patient
Idempotency-Key: una-clave-estable-por-intento

{"expectedVersion":"7","patientId":"44"}
```

Comprueba propietario de reserva/paciente, workspace y contexto concordantes,
versión y permisos; utiliza el lock/transacción/recibo comunes. Incrementa la
versión y registra PATIENT_LINKED con actor médico y before/after. Repetir la
misma clave/body devuelve el recibo; una versión vieja devuelve VERSION_CONFLICT.
Una ficha ajena/incompatible devuelve PATIENT_NOT_FOUND (404). Un vínculo existente
devuelve BOOKING_PATIENT_ALREADY_LINKED (409); no se sustituye ni se elimina.

Puede vincularse una reserva conservada en cualquiera de sus estados. No se
agrega una transición de estado ni se exige recrear un turno histórico. La
operación no consume un cupo nuevo de consultorio; conserva fences/permisos de
gestión y puede operar durante un exceso de cupo igual que cerrar un turno.
No crea/edita paciente o consulta, no busca por homonimia/DNI y no infiere identidad.
No hay operación de corrección/desvinculación; requeriría un pedido diferenciado.

## Canales público y secretaría

El visitante sigue eligiendo consultorio/horario con el mismo formulario,
verificación, enlaces y claves de reintento. No se le exige contexto nuevo ni se
agrega selector. Si envía un contexto se verifica contra el bloque. La confirmación
inmediata incorpora specialtyName persistido; no incluye IDs clínicos/procedencia.

Secretaría sigue usando credenciales, sesión, CSRF, permisos y sedes asignadas.
Sus DTO incluyen especialidad y nombre/zona históricos del consultorio; excluyen
patientId, procedencia técnica y direcciones del snapshot. No hay ruta delegada
para vincular fichas, ni búsqueda/lectura clínica. Revalidación y replay continúan
dentro de los locks de autorización. Ocupaciones de sedes no visibles igualmente
bloquean el tiempo global. Los errores de disponibilidad pública/staff no exponen
nombres de consultorios no autorizados ante un conflicto global de agenda.

## Notificaciones y constancias

Confirmation/reprogramación/cancelación/reminder obtienen especialidad y datos de
consultorio de los snapshots de la reserva. La dirección pública es un campo
separado: nunca se proyecta el domicilio privado a constancias o correos. La
constancia privada refleja la reserva vigente y su destino después de reprogramar;
los eventos y recibos preservan las versiones anteriores.

PATIENT_LINKED no genera un correo ni duplica recordatorios. El worker mantiene
vigentes avisos pendientes si las únicas versiones posteriores son eventos
PATIENT_LINKED, comprobando la cadena completa; otros cambios conservan su
obsolescencia anterior. Payloads cifrados anteriores a M4 sin especialidad se
completan al enviar desde la clasificación congelada de la reserva. Los demás
datos de esos payloads históricos se conservan; no se reescriben ni se reenvían
avisos por la migración. Deduplicación, leasing, expiración, recuperación y
validación del código privado continúan. No se incluyen nombres de fichas,
antecedentes o diagnósticos en avisos administrativos.

## Angular

Se conserva el calendario global y sus acciones. Cada turno/detalle muestra
especialidad y consultorio persistidos, persona atendida, fecha/hora y estado.
La zona del snapshot conserva la interpretación histórica frente a cambios
administrativos. No hay códigos de las tres especialidades hardcodeados en la vista.

El médico puede buscar una ficha en el contexto concordante, elegirla y confirmar
explícitamente el vínculo desde el detalle. Se reutilizan PatientService y la
pantalla existente de alta mediante un enlace; crear la ficha no la vincula
automáticamente. Cuenta/editor/contexto invalidan resultados tardíos de búsqueda.
Cambiar el contexto activo no reclasifica los turnos administrativos. Secretarias
y constancias muestran especialidad en texto sin controles clínicos.
El editor «Mis horarios» no se rediseñó; se conservan sus 17 pruebas.
Ver [handoff frontend](../../doctorchart-frontend/docs/specialty-contexts-m4.md).

## Pruebas y límites de verificación

Go 1.27.3 Linux/amd64 en Docker, fuentes/dependencia local de medios en read-only.
PostgreSQL 17.11 y 18.6 Debian propios: label
codex.task=doctorchart-specialty-m4-20261110; contenedores
doctorchart-specialty-m4-pg17-20261110 y doctorchart-specialty-m4-pg18-20261110,
loopback 55468/55470, rol migration_fixture y siete DB de ensayo separadas.
Se verificaron identidad/puertos antes de operar; no se usaron DSN reales ni
binarios Go servidores en Windows. No se modificó el firewall.

- Build y vet: PASS. Suites completas normales: PASS en PG17 y PG18.
- Detector de carreras completo: PASS en PG17 y PG18. gofmt y diff --check: PASS.
- Migración: vacío/13→14, todos los estados, futuro fuera de agenda, bloque
  desconocido, conservación de eventos/recibos/outbox/versiones, restart,
  catálogo independiente, checksums, drift, fallos SQL/seed, rollback/reintento,
  propietario sin contexto y rechazo de mutaciones SQL incompatibles.
- Reservas: tres canales, duración por bloque, movimiento entre consultorios,
  snapshots de antes/después, retiro de bloque cancelado, contexto ajeno, vínculo
  explícito/replay/rollback, privacidad staff y notificaciones que sobreviven al
  vínculo. Continúan carreras de creación/agenda/reprogramación/canales, exclusión
  global, DST, excepciones, estados y pruebas de permisos existentes.
- TypeScript app/spec: PASS. Angular focalizado: 61 PASS en 11 archivos.
  Suite completa: 827 PASS y los 3 fallos previos de flags (calendar.guard,
  schedules.guard, public-appointments.guard). No se cambiaron esos tests/flags.
- Build development: PASS, 2 rutas prerenderizadas. Producción: BLOCKED por
  supabase.publishableKey ausente. No se inventaron claves.
- Chrome con sesión/HTTP sintéticos: etiquetas históricas, vínculo clínico
  explícito con scope, especialidad staff sin acceso clínico y constancia pública;
  capturas 1440×1000 y 390×844 revisadas, sin overflow/errores JS. No es E2E
  contra Supabase/Gateway reales. Static smoke productivo sigue bloqueado por
  falta del artefacto productivo; no se lo reporta como aprobado.
- Integraciones externas de Billing/conversor conservan SKIP condicionales.
  No se ejecutó govulncheck ni se presenta como auditoría de seguridad.

Reproducción: go build ./..., go vet ./..., go test -p 1 ./... -count=1 y
go test -race -p 1 ./... -count=1, con las ocho DSN de fixtures documentadas en
db/README.md apuntando exclusivamente a laboratorios propios. Node usa
NODE_OPTIONS=--no-experimental-webstorage sólo en tests para jsdom.

## Operación futura, riesgos y M5

**No iniciar este binario contra una base real sin autorización:** Ensure
aplicaría 14 automáticamente. No hubo migraciones reales, wipes o despliegues.
Para una futura operación autorizada, leer vps-operations.md, identificar la
instancia/major/base reales, verificar respaldo y restauración y ensayar 13→14
sobre un clon representativo dentro de los timeouts uniformes. Detener escritores
13 y coordinar binario/frontend 14; no hay operación mixta de backends 13/14.
Comprobar marcador/historial/catálogo, snapshots, conteos, permisos, disponibilidad,
constancia y outbox antes de reabrir. No enviar correos reales durante el ensayo.

Un fallo previo al commit deja 13 y permite corregir/reintentar. Tras confirmar
14 se requiere binario compatible o restauración autorizada que contemple las
escrituras posteriores. No bajar marcadores/checksums ni usar un DOWN destructivo.
La procedencia exacta antigua continúa desconocida: es una limitación del dato,
no una tarea de matching pendiente que un agente deba resolver automáticamente.

M5 debe habilitar múltiples contextos por migración, definir selección clínica,
agenda/filtros y publicación pública por contexto, y reemplazar los JOIN/fallbacks
de contexto único que aún existen en workspace/wizard/perfil y PUT legacy de
horarios. La creación de reservas ya deriva del bloque; reprogramar conserva su
contexto y la FK paciente/contexto ya está preparada. Verificar varios contextos
reales sólo después de retirar autorizadamente la restricción; no hacerlo en M4.
Revisar callbacks de búsqueda clínica, creación de fichas y reprogramaciones
con selector activo distinto, privacidad staff entre sedes, directorio, cuotas y
notificaciones. No ampliar el catálogo antes de finalizar M6.

## Estado de Git y entrega

Backend main HEAD 6869461; frontend main HEAD ff14f0c. M1–M3 y cambios previos
ya estaban sin commit y se conservaron. M4 agrega migración 14, snapshots,
vinculación y pruebas; actualiza runner/lectores/fixture assertions para la
versión 14. El diff contra HEAD mezcla varias etapas: no atribuirlo entero a M4.
No se hicieron commits, pushes, worktrees, resets ni cambios en bases reales.
Los contenedores y volúmenes propios del laboratorio M4 se eliminaron tras
verificar labels/puertos; no quedan recursos Docker de este ensayo.
M4 termina aquí; M5 requiere un pedido posterior.
