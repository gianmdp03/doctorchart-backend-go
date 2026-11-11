# DoctorChart — M3: bloques por consultorio y especialidad

Fecha: 10/11/2026. Continúa [M1](specialty-contexts-m1.md) y
[M2](specialty-contexts-m2.md). Fuente de alcance: pedido M3 del propietario;
las etapas antiguas de consultorios de `docs/ai/` no son este rediseño.
Este documento permite comenzar M4 en otro chat sin reconstruir la conversación.

## Resultado y límite

Un workspace administrativo por médico, consultorios compartidos y una agenda
global. Cada bloque tiene identidad estable, consultorio, contexto clínico,
día semanal o excepción, inicio, fin y duración propia. Continúa la restricción
`doctor_specialty_contexts_single_context_key`: un contexto por workspace.
Sólo Pediatría, Medicina General y Dermatología. No se amplió el catálogo, ni
se agregó un selector global, un segundo wizard o endpoints multiespecialidad.

Angular ofrece «Mis horarios»: consultorio y especialidad se autoseleccionan
cuando son únicos; permite crear varios días juntos, editar/quitar bloques y
configurar excepciones/cierres. El calendario global conserva sus pantallas y
las reservas públicas/administrativas conservan su contrato actual.

## Esquema 13

`db/migrations/0013_schedule_blocks.sql` evoluciona 12 → 13. Baseline y SQL
0002–0012 intactos. Resultado: **47 tablas y 56 FK**.

| Objeto | Responsabilidad e integridad |
|---|---|
| `clinic_schedules` | Una raíz por consultorio; conserva versión, anticipación y horizonte. Agrega workspace validado por FK compuesta al consultorio. |
| `schedule_blocks` | BIGINT identity; consultorio/workspace/contexto obligatorios, minutos civiles de inicio/fin y duración 5–480. |
| `schedule_exceptions` | BIGINT identity; consultorio/workspace, fechas inclusivas y etiqueta. Una excepción sin bloques es cierre. |

El bloque referencia la raíz por `(clinic_id,workspace_id)` y el contexto por
`(specialty_context_id,workspace_id)`. Un bloque de excepción referencia además
`(exception_id,clinic_id,workspace_id)`. Las FK son restrictivas, sin cascadas
nuevas. Un CHECK exige exactamente uno entre weekday 1–7 y exception_id.
Los minutos cumplen `0 <= inicio < fin <= 1440`; 24:00 sólo puede ser final.
No se permiten franjas nocturnas que crucen medianoche.

Tres exclusiones GiST diferidas impiden solapar fechas de excepciones de un
consultorio, bloques semanales del mismo consultorio/día y bloques de una misma
excepción. Se difieren para poder actualizar un conjunto de bloques sin que el
orden de sus UPDATE cause conflictos transitorios. El estado final se valida
antes del commit. Los índices soportan lectura por consultorio/día/inicio,
contexto e identidad de excepción. Las etiquetas usan text para conservar
exactamente valores antiguos: el límite HTTP existente cuenta 160 runas después
de TrimSpace, por lo que no equivale a varchar(160).

No existe una asociación permanente consultorio–especialidad fuera del bloque.
No se creó una agenda por pareja ni se duplicó la fuente de verdad en JSON.
Después del traslado se retiran `clinic_schedules.weekly`, `exceptions` y
`slot_minutes`. El DTO sigue agrupando filas en weekly/exceptions para reutilizar
los clientes y el motor, pero esos JSON no se persisten.

## Migración, conservación y operación

Cada franja anterior se convierte una sola vez usando el único contexto del
workspace del consultorio y su duración anterior. Se conservan minutos, días,
fechas, etiquetas, cierres, reglas administrativas, versiones y reservas. Los
bloques/excepciones obtienen su identidad inicial en esta migración; los
reinicios y las ediciones posteriores la conservan. Días sin franjas se leen
como ausencia de bloques, con la misma semántica de cierre semanal.

Se rechazan propietarios sin contexto, weekdays repetidos/inválidos, horas
malformadas, excepciones incompletas y solapamientos. No se inventa contexto ni
se repara una agenda inválida. Las exclusiones se verifican antes de retirar
las columnas antiguas. Cualquier fallo revierte DDL, datos, historial y marcador.

El runner existente conserva pgx, READ COMMITTED, contexto 30s,
lock_timeout 10s, statement_timeout 20s y advisory lock transaccional (17209,3).
Identidad, checksum SQL, historial y catálogo previo se verifican antes de cada
transición; esquema, seeds y marcador confirman juntos antes de HTTP/jobs.
El arranque 13 valida sin adoptar/reparar bases incompatibles. Se ampliaron las
pruebas de versiones anteriores hasta 13; no se omitieron controles de drift.

Pins finales, derivados de bases vacías independientes a partir del SQL y
comprobando también todos los pins publicados anteriores:

| PostgreSQL | SHA-256 del catálogo 13 |
|---|---|
| 17.11 | `aeff97d76a5ba07e3e6a9ccdd7eb40013e8c2bca57ed505bf7d40f83ce6935fe` |
| 18.6 | `67d332b5acd8e86798b5134729999f55ad4f2271d020352b3b7cb60c436951e4` |

**No iniciar este binario contra una base real sin autorización de migración:**
el arranque aplicaría 13 automáticamente. No se ejecutó contra ninguna base
real. El laboratorio fue PostgreSQL 17.11/18.6 en Docker Linux, con datos
sintéticos, label `codex.task=doctorchart-specialty-m3-20261110`, puertos loopback
55468/55470 y rol migration_fixture; las DSN reales no se usaron.
Al terminar se eliminaron exclusivamente esos contenedores y volúmenes de caché
propios, verificando labels/puertos. No quedan recursos Docker de este laboratorio.

Para una futura puesta en servicio autorizada: revisar primero
[operación del VPS](vps-operations.md), identificar servicio/base/versión reales,
obtener y verificar un respaldo recuperable de datos y esquema, ensayar 12→13
sobre una copia aislada y coordinar la actualización del backend y frontend.
Detener escritores antiguos durante el cambio: un backend 12 no soporta las
nuevas tablas y rechaza el marcador 13. No hay operación mixta 12/13 soportada.
Tras el arranque, verificar versión/historial/catálogo, salud, horarios,
excepciones, disponibilidad y una reserva de ensayo autorizada.

Si falla antes del commit, la transacción conserva 12 y permite corregir la
causa/reintentar. Después del commit no bajar el marcador ni arrancar el binario
anterior: usar un binario compatible o una restauración autorizada del respaldo,
con escritores detenidos y considerando cualquier escritura posterior. No se
preparó un DOWN destructivo ni se autoriza reconstrucción, wipe o despliegue.

## Contrato HTTP de horarios

Rutas existentes `GET/PUT /api/doctorchart/clinics/{id}/schedule` y
`GET /api/doctorchart/clinics/{id}/availability`.
Raíz de PUT: expectedVersion string, minNoticeMinutes, maxAdvanceDays, weekly
y exceptions explícitos. El guard comercial, propiedad del consultorio, estado
archivado, límites y control de versiones siguen vigentes.

Ejemplo de weekly para el cliente M3:

```json
{
  "expectedVersion": "7",
  "minNoticeMinutes": 0,
  "maxAdvanceDays": 30,
  "weekly": [{"weekday": 1, "ranges": [
    {"id": "51", "specialtyContextId": "101", "start": "08:00", "end": "10:00", "slotMinutes": 20},
    {"specialtyContextId": "101", "start": "10:00", "end": "12:00", "slotMinutes": 30}
  ]}],
  "exceptions": []
}
```

Los IDs de bloques/excepciones/contextos en este DTO son strings decimales,
como los BIGINT de consultorios/reservas; no convertirlos arbitrariamente a
números JS. Omitir id crea un bloque. Conservar id lo edita. Omitir un bloque
del conjunto lo elimina, sujeto a reservas futuras. IDs ajenos/repetidos se
rechazan; no se permite mover una identidad de otro consultorio. No se borran y
recrean todos los bloques para guardar una agenda.

Una excepción incluye id opcional, from/through ISO, label y ranges. Cada range
tiene el mismo formato de bloque. Fechas inclusivas; ranges vacío cierra la
agenda en esas fechas. Toda excepción reemplaza la semana habitual.

Compatibilidad transitoria: PUT admite el antiguo slotMinutes raíz válido
5–480 para completar duración y, **sólo con exactamente un contexto**, contexto
omitido. Un cliente antiguo sin IDs sólo conserva la identidad de franjas con
iguales inicio/fin dentro del mismo día o excepción; un cambio incompatible con
reservas futuras se rechaza. El GET raíz muestra una duración derivada cuando
todos los bloques coinciden y 0 cuando difieren/no hay bloques (null si no hay
configuración). Ese campo no es fuente de verdad ni debe reenviarse como 0:
Angular M3 lo omite y envía duración/contexto por bloque. Esta compatibilidad
debe revisarse antes de M5; nunca resolver ambiguamente contextos múltiples.

Errores relevantes: 400 INVALID_REQUEST por horas/duración/solapamiento local;
404 SPECIALTY_CONTEXT_NOT_FOUND o SCHEDULE_BLOCK_NOT_FOUND por ámbito ajeno;
409 VERSION_CONFLICT, SCHEDULE_CONFLICT o BOOKING_SCHEDULE_CONFLICT.
Los conflictos entre consultorios incluyen nombres, fecha, horario y zona.
El GET de configuración usa una transacción read-only repeatable-read para
leer una misma versión de raíz e hijos, sin cambiar el aislamiento del runner
o de las escrituras.

## Motor temporal y reservas

Se reutiliza civilMinutes y la zona IANA del consultorio. La duración efectiva
se toma de cada range. Se conservan horas inexistentes/repetidas, fechas omitidas,
rechazo de slots que atraviesan cambios de offset y slots completos anclados al
inicio de cada bloque. No hay cálculo paralelo en Angular, slots persistidos,
feriados automáticos ni recurrencias nuevas. Las pausas son huecos entre bloques.

La validación global existente compara intervalos reales UTC [inicio,fin),
incluyendo distintas zonas/consultorios y excepciones. Se conserva su horizonte
de escritura de 369 días y la comprobación adicional en disponibilidad. No se
promete una validación perpetua de todas las recurrencias futuras. Los bloques
adyacentes son válidos. La exclusión PostgreSQL de reservas continúa por workspace
e intervalo: no se separa por contexto o bloque.

Las escrituras mantienen locks de workspace/consultorio, fences comerciales,
versión y revalidación transaccional. Disponibilidad toma el lock compartido
existente y filtra ocupaciones de todo el workspace. Crear/reprogramar conserva
el cálculo de fin en el servidor; el cliente no puede elegir una duración ajena
al bloque. Continúan cancelaciones, estados, idempotencia, reservas públicas,
secretaría y notificaciones.

AvailableSlot incorpora sólo metadatos **internos** blockID/contextID; el JSON
público sigue siendo start/end. Antes de guardar horarios se compara para cada
reserva futura confirmada/en atención el slot previo y el propuesto: deben
conservar bloque, contexto y fin. También sigue la validación general de reservas
frente a la agenda. Retirar/recrear un bloque o agregar una excepción que lo
reemplace no puede reasignar silenciosamente una reserva. No se reescriben
reservas ni historias. La procedencia no está persistida todavía.

## Verificación

- Go 1.27.3 ejecutado en Linux Docker, sin levantar binarios Go en Windows.
- Build/vet y suites completas con detector de carreras: PASS en PostgreSQL
  17 y 18. Tras el último refuerzo de rechazo de reservas previas sin bloque de
  origen verificable, se repitió clinics; el fixture nuevo omitía incrementar
  la versión exigida por el trigger. Se corrigió el fixture y las tres pruebas
  M3 pasaron nuevamente con race en ambos motores; el resto de clinics había
  pasado en esa repetición.
- Migración: vacío/12→13, conservación exacta de horas/días/duración/etiquetas y
  cierres, reservas/versiones, restart estable, catálogo independiente,
  SQL fallido, drift, seed tardío, rollback/reintento y FK/exclusiones.
- Motor/API: tres consultorios/contexto único, bloques adyacentes y superpuestos,
  duraciones mixtas, slots incompletos descartados, contextos/IDs ajenos,
  excepciones, cambios con reservas futuras y reprogramación. Concurrencia de
  edición contra creación y contra reprogramación; siguen suites de zonas,
  disponibilidad pública/administrativa, exclusión global y secretaría.
- Angular: 17 pruebas de horarios PASS; suite completa 822 PASS y 3 fallos
  preexistentes de flags en calendar.guard, schedules.guard y
  public-appointments.guard. No se cambiaron esos tests, flags ni guards.
- TypeScript y build de desarrollo PASS. Build de producción BLOCKED por
  supabase.publishableKey ausente; no se inventaron credenciales.
- El smoke estático adicional alcanza las rutas privadas, pero falla al buscar
  un asset con hash en el artefacto development, que genera main.js sin hash.
  No valida el paquete productivo, cuya generación está bloqueada por Supabase.
  La nueva ruta /schedules sí se recorrió en Chrome con autoselección.
- Recorrido real del cliente en Chrome con sesión/API sintéticas: carga,
  autoselección, creación multidiaria y guardado del DTO. Capturas revisadas
  a 1440×1000 y 390×844, sin desborde horizontal ni errores JS. Esto no es E2E
  contra Supabase/Gateway reales. Pruebas del editor cubren además edición,
  eliminación, conflictos, excepciones y preview servido por backend.
- Integraciones que requieren Billing/conversor/credenciales externos mantienen
  sus SKIP condicionales. No se ejecutó govulncheck ni se presenta como aprobado.

Comandos equivalentes del gate: `go build ./...`, `go vet ./...`,
`go test -p 1 ./... -count=1`, `go test -race -p 1 ./... -count=1`.
Para PostgreSQL usar exclusivamente laboratorios propios con las ocho DSN de
fixtures documentadas en db/README.md; nunca una DSN real. El puerto de fixture
se adapta en el laboratorio sin relajar los guards de nombres/loopback/usuario.
Los tests crean/eliminan sólo sus recursos descartables autorizados.

## Handoff exacto para M4 y M5

M4 debe persistir en reservas el contexto y la procedencia del bloque/slot,
definir conservación de snapshots al editar o retirar bloques y migrar reservas
previas usando evidencia inequívoca. La comparación de M3 resuelve el slot al
editar desde el horario anterior; no constituye una auditoría histórica durable
ni debe usarse para inferir especialidades de pacientes. Revisar backfill,
reservas fuera de agenda y estados históricos antes de decidir ese contrato.

Extender conjuntamente creación/reprogramación administrativa, pública y staff,
idempotencia, disponibilidad, notificaciones y pruebas de carreras. Mantener
workspace como ámbito de exclusión global. No exigir contexto nuevo a clientes
públicos sin una evolución explícita del contrato. No crear identidades clínicas
automáticamente por una reserva.

M5 deberá retirar por migración la restricción de contexto único, definir selección
de contexto y eliminar todo fallback ambiguo. Quedan pendientes para esa etapa
pruebas reales de varios contextos del mismo médico, bloques con distintos
contextos en el mismo consultorio, filtros y selector global. No alterar hoy
la base para simular un escenario prohibido por M3.

## Estado de Git

Backend main, HEAD 6869461; frontend main, HEAD ff14f0c. M1, M2 y cambios anteriores
ya estaban sin commit y se preservaron, incluidos archivos compartidos por estas
etapas. M3 agrega migración/modelo/pruebas/documentación y adapta motor/editor;
el diff contra HEAD contiene también trabajo anterior y no debe atribuirse entero
a M3. No se hicieron commits, pushes, worktrees, despliegues o resets de desarrollo.
M3 termina aquí. M4 requiere un pedido posterior.
