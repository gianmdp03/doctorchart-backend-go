# Agenda unificada y teleconsultas

## Modelo

Cada espacio médico tiene una agenda global. El registro que identifica el lugar o la modalidad de atención conserva los identificadores y relaciones de horarios, reservas, publicación y secretaría. PostgreSQL permite una agenda virtual por médico y exige que sus campos físicos estén vacíos. Solo los consultorios físicos no retirados cuentan para el cupo.

Las escrituras de agenda y turnos se serializan con el bloqueo de la fila del médico en PostgreSQL. Go compara intervalos efectivos en UTC, incluidas las excepciones y los cambios de hora, y usa intervalos semiabiertos para permitir franjas contiguas. PostgreSQL excluye reservas superpuestas por médico y valida los bloques dentro de cada agenda. Los conflictos de configuración incluyen la agenda que se edita, el horario existente y la intersección. El recorrido público usa errores de disponibilidad generales.

Desactivar cierra la disponibilidad nueva y conserva la configuración y los turnos vigentes. Retirar un consultorio físico libera su cupo y quita sus franjas recurrentes. Sus turnos vigentes siguen ocupando tiempo y su historial permanece accesible. La agenda virtual admite activación y desactivación.

Cada turno guarda la modalidad en su snapshot. Google Meet podrá vincular un enlace privado al identificador del turno vigente con acceso explícito para el médico y el paciente autorizado. La respuesta administrativa de secretaría utiliza una lista de campos permitidos, incluso al repetir una solicitud idempotente. Esta etapa entrega la reserva virtual y deja la videollamada para una integración posterior.

## Verificación

Pasaron la compilación, vet y las pruebas Go. Las suites de catálogo y agenda se ejecutaron en PostgreSQL 17 y 18 descartables, con una corrida adicional de las regresiones de concurrencia bajo el detector de carreras. Pasaron las 862 pruebas de Angular. En Node 26, la suite se ejecutó con `NODE_OPTIONS=--no-experimental-webstorage` para que el entorno DOM de pruebas gestione `localStorage`.

Playwright completó 18 recorridos con Angular, el gateway, Go y PostgreSQL. Usó los adaptadores locales de autenticación y facturación. La prueba confirmó una reserva pública exclusivamente virtual y verificó que aparece junto a los turnos presenciales en el calendario global. La suite Go comprobó el alcance de secretaría sobre la agenda virtual.

## Prueba local

Usá una base vacía. El arranque comprueba el checksum y el catálogo. Una instalación persistente inicializada con un baseline previo debe conservarse o reinicializarse con autorización. La [guía de la instancia local](../ops/local/README.md) describe el arranque habitual y los respaldos.

Para ensayar con un entorno separado, usá el laboratorio descartable que ya ofrece el repositorio. Prepará y arrancá el laboratorio desde el repositorio del backend.

```powershell
node ops/testing/specialty-m6-lab.mjs prepare
node ops/testing/specialty-m6-lab.mjs build
node ops/testing/specialty-m6-lab.mjs up
```

Compilá la aplicación y ejecutá la prueba desde el repositorio del frontend.

```powershell
npx ng build --configuration m6-lab
node scripts/specialty-contexts-m6-real.cjs
node scripts/specialty-contexts-m6-real.cjs --serve
```

La prueba de navegador crea un médico de ensayo, tres especialidades y varios consultorios, luego prueba el alta de la teleconsulta, el conflicto de horarios, la publicación y el turno virtual. Con `--serve` podés iniciar sesión en `https://localhost:4353` usando `email` y `password` del archivo `settings.json` que indica el comando. El navegador del laboratorio verifica el certificado local de prueba. Para una sesión manual, confiá en ese certificado de laboratorio según tu configuración de Windows.

1. En **Consultorios**, abrí la agenda virtual y luego **Ver horarios y agenda**. Probá guardar el lunes una franja que empiece a las 11:30 y que se cruce con el consultorio hasta las 12:00.
2. En **Agenda de turnos**, elegí el lunes con turnos de prueba y alterná los filtros de modalidad, sede y especialidad. Abrí el turno virtual para ver sus acciones.
3. En una ventana privada, abrí `/appointments` y elegí **Teleconsulta**. Abrí el perfil de prueba y verificá la modalidad y la hora antes de confirmar.
4. En **Secretarias**, asigná la agenda virtual a una cuenta de prueba. Abrí su invitación desde Mailpit y verificá sus acciones autorizadas.

Para repetir la prueba de navegador, usá `reset-e2e` solo sobre el laboratorio descartable. `down` detiene y retira los recursos propios del laboratorio.
