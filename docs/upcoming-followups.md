# Recordatorios de próximos controles

Inicio muestra los controles de hoy, mañana y pasado mañana a partir de
`follow_up_date`, la proyección persistida de `values.followUpDate` en cada
historia clínica. El médico puede abrir la ficha mediante el nombre completo
del paciente. Se trata de la fecha indicada por el médico; no agrega una agenda
de turnos, horarios ni confirmaciones de asistencia.

Se considera únicamente la última consulta de cada paciente, ordenada por
`encounter_date DESC, id DESC`. Una consulta posterior reemplaza el control
anterior, incluso si no indica otra fecha. Editar una consulta antigua no vuelve
a activar una indicación reemplazada. Cada paciente aparece una sola vez.

`GET /api/doctorchart/encounters/dashboard` agrega `upcomingFollowUps`, siempre
un arreglo, con `encounterId`, `patientId`, `patientFullName` y `followUpDate`.
La lectura está limitada al propietario autenticado y usa la misma transacción
de lectura que las métricas. No depende de las ocho consultas recientes ni de
la paginación del historial. El rango se calcula con la zona del usuario y las
fechas DATE viajan como `YYYY-MM-DD`, sin conversiones a instantes UTC.

El frontend agrupa por `asOfDate` del servidor y presenta `dd/MM/aaaa`. Recarga
al volver a Inicio, al cambiar de cuenta o de día/zona y ante eventos de cambios
clínicos. Distingue carga, error, respuesta antigua sin el campo y días sin
controles. Los nombres largos se ajustan y los listados extensos permiten
desplazamiento. Se conservaron los filtros del directorio del cambio anterior.

El formulario conserva el selector Material compartido, permite fechas futuras
y borrar el control opcional. Su etiqueta ahora está vinculada al campo. Se
rechaza el año cero tanto en Go como en el frontend, porque PostgreSQL no admite
esa fecha; se conservan las fechas históricas válidas para edición.

## Esquema y despliegue

No cambia ningún objeto PostgreSQL ni el baseline publicado. No necesita
migraciones ni backfills. Actualizar primero el backend con la dependencia
de medios existente y después el frontend. Los clientes anteriores ignoran el
nuevo campo. Un frontend nuevo ante un backend antiguo muestra recordatorios
no disponibles, en lugar de informar falsamente que no hay controles.
La reversión consiste en recuperar las versiones anteriores; los datos de
próximo control conservan su formato.

## Verificación local del 23/10/2026

- Frontend: suite completa aprobada; build de producción aprobado con una clave
  pública ficticia para el ensayo, sin publicar. Capturas de los componentes
  Angular reales con pacientes sintéticos; ambos temas y anchos de 320 a 1440 px,
  contraste de texto de los recordatorios >= 4,5:1, foco de teclado, enlaces,
  estados vacíos, formulario y lectura de la fecha guardada.
- Go 1.27.3: pruebas con race de `internal/wizard`, `db`, `internal/bootstrap`
  e `internal/api` aprobadas. La nueva consulta de producción se ejecutó con
  race sobre PostgreSQL 17.11 descartable propio usando el baseline real.
  Incluye más de ocho pacientes, aislamiento de propietarios, fechas límite,
  cambio de año/zona, empates de creación, fechas reemplazadas, borrado y
  reprogramación de la fecha, nombres actuales y resultados vacíos.
- El gate integral de build/vet/test/race está **bloqueado**: el `replace` de
  `go.mod` apunta a `../bycode-image-email-service-go/pkg/media`, que no existe
  en este checkout. El propietario confirmó que está únicamente en el VPS.
  No se sustituyó la librería ni se accedió al VPS. El test HTTP de guardado,
  lectura y edición preparado en `follow_up_integration_test.go` sigue pendiente
  de ejecución en un entorno de ensayo que tenga esa dependencia.

La consulta puede verificarse independientemente mediante
`go test -race internal/clinical/follow_up.go internal/clinical/follow_up_query_test.go`.
Requiere `DOCTORCHART_FOLLOWUP_TEST_DATABASE_URL` apuntando exclusivamente a una
base descartable propia, de loopback, llamada `doctorchart_followups_query_test`.
Ese test usa el baseline uniforme y revierte sus datos de ensayo.

Con la dependencia disponible, ejecutar también
`go test ./internal/clinical -run '^TestFollowUp' -count=1 -v` contra otra base
descartable propia llamada `doctorchart_integrity_test`, mediante
`DOCTORCHART_INTEGRITY_TEST_DATABASE_URL`. El fixture de integridad reinicializa
su esquema; no debe apuntarse a una base que contenga datos a conservar.
