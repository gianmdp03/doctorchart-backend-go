# Transición de baseline 16 a 17

La versión de `baseline-copy` de SP-04C crea destinos en el baseline 18. El argumento `--source-baseline-16` permite leer un origen 16 con ese binario. Para el procedimiento vigente desde el 17, consultá [Aprobación de plantillas clínicas SP-04C](template-approvals-sp04c.md).

Los números 16 y 17 identifican versiones del esquema DoctorChart. Usá la misma versión mayor de PostgreSQL en origen y destino. La herramienta admite PostgreSQL 17 o 18 y exige la misma codificación y configuración de collation. Este procedimiento construye una base consolidada en un destino vacío. El arranque habitual conserva sus comprobaciones de versión, checksum y catálogo.

## Qué cambia

| Área | Baseline 17 |
| --- | --- |
| Fichas, consultas, recetas, estudios y vacunas | Marca de anulación inicialmente vacía. Recetas, estudios y vacunas incorporan versión inicial cero. Fichas y consultas conservan su versión previa. |
| Revisiones | Tabla nueva con actor, propietario, contexto, motivo, fechas, versiones y contenido previo. Los triggers registran cambios posteriores en la misma transacción. El historial inicial está vacío. |
| Índices e integridad | Unicidad parcial para primera consulta vigente y dosis vigente. Los cálculos mantienen sus claves hacia ficha y consulta. Los turnos conservan su paciente vinculado y requieren una ficha vigente para vínculos nuevos. |
| Seguridad | RLS por propietario y contexto, permisos de lectura para revisiones y retiro de borrado ordinario en tablas clínicas. El rol de medios conserva sus permisos acotados. |

Se copian los datos de las 47 tablas anteriores al historial, con columnas, identificadores y estados de secuencia originales. Esto abarca datos clínicos, configuraciones, snapshots, agendas, turnos, eventos y colas. El marcador del origen queda en su base. El marcador 17 se crea como parte del commit del destino después de comprobar sus datos y catálogo.

## Preparación

1. Prepará una ventana sin escrituras. Detené los procesos que escriben en el origen, incluidos los workers de notificaciones, archivos y reintentos. Mantené los procesos de destino detenidos hasta terminar las verificaciones. Una copia consistente de tablas no congela las secuencias ni los adjuntos.
2. Conservá la base 16 y un respaldo físico recuperable del clúster. Usá `pg_basebackup --wal-method=stream` y comprobá el manifiesto con `pg_verifybackup`. Conservá por separado la configuración externa y las claves de servicio, en almacenamiento protegido. Copiá también los bytes de adjuntos, el almacenamiento de recuperación y la configuración que interpreta las claves de archivos. Para comprobarlos, registrá conteo y hash de los objetos con un inventario de acceso restringido.
3. Restaurá una copia del respaldo físico en un clúster aislado de la misma versión mayor, siguiendo las rutas y permisos de archivos de PostgreSQL. Comprobá marcador, catálogo y arranque con el binario 16 y una muestra de fichas, turnos y archivos. Si usás esa restauración como origen de la copia, asegurate de que represente el último estado detenido. El ensayo lógico con `pg_dump` y `pg_restore` recuperó filas, pero PostgreSQL reescribió expresiones de restricciones e índices y el fingerprint lo rechazó. Para un retorno con el catálogo intacto usá el respaldo físico verificado.
4. Creá una base de destino vacía con el propietario de esquema y los roles restringidos `doctorchart_app` y `doctorchart_media` ya provisionados en el clúster. Usá las identidades de mantenimiento autorizadas para leer todas las filas y secuencias en el origen y crear objetos en el destino. Conservá los logins de ejecución separados. Comprobá identidad, host y nombre de ambas bases antes de ejecutar.

## Ejecución

Pasá las conexiones de origen y destino por `DOCTORCHART_COPY_SOURCE_DSN` y `DOCTORCHART_COPY_TARGET_DSN`, respectivamente. Usá el mecanismo de secretos del entorno y TLS cuando corresponda. Desde el checkout que contiene el baseline 17 ejecutá el comando siguiente.

```sh
go run ./cmd/baseline-copy --confirm-empty-target
```

El origen se abre en una transacción de solo lectura. La herramienta exige el marcador y fingerprint conocidos de la versión 16. En el destino crea tablas e índices, copia las filas ordenadas por clave primaria y coteja conteos y SHA-256 de los datos antes y después. Luego instala llaves foráneas, triggers, RLS y permisos del baseline 17, conserva las posiciones de secuencia y valida el catálogo. Un error revierte la transacción del destino. Una segunda ejecución sobre un destino ya ocupado se rechaza.

## Verificación antes del cambio de conexión

1. Conservá la salida del comando. Verificá que la cantidad de filas preservadas sea coherente con el inventario de la instalación y que los vínculos clínicos, snapshots y configuraciones coincidan. Confirmá que `clinical_revisions` comienza vacío.
2. Montá la copia de archivos y recuperación con las rutas o claves esperadas. Comprobá la apertura, el hash y los permisos de los adjuntos. Verificá logins de ejecución, fingerprint y arranque 17 contra el destino con los trabajos y salidas externas controlados. Probá con cuentas autorizadas e independientes y con dos contextos del mismo médico.
3. Validá una ficha, una consulta previa, un documento, la navegación desde un turno y sus rechazos cruzados. Hacé las escrituras de prueba sobre una copia de ensayo del destino. Verificá que la primera corrección conserve el estado y versión anteriores. El historial identifica ese estado como anterior a la primera revisión registrada, también cuando se trata de la primera corrección de una ficha recién creada.
4. Acordá el cambio de conexión con el responsable y recién entonces habilitá escrituras. Mantené un solo ambiente escritor. Revisá antes cómo tratar las notificaciones pendientes y las credenciales externas conservadas.

## Fallo y retorno

Si falla la copia, conservá el origen y verificá que el destino no tenga marcador ni tablas creadas por el intento. Si se pierde la respuesta al confirmar, inspeccioná el destino antes de reintentar. Un marcador 17 exige comprobar arranque, catálogo e inventario. Los errores SQL informan el SQLSTATE sin mostrar valores clínicos. Los límites por sentencia pueden requerir un ensayo de capacidad con un volumen representativo antes de usar este procedimiento.

Antes de habilitar escrituras en el destino, podés retomar el binario 16 con su base y adjuntos originales. Si hace falta restaurar, usá una base fresca y el backup 16, sus roles y la copia correspondiente de archivos. Una vez que existan nuevas escrituras en 17, detené ambos entornos y conservá ambos estados para reconciliar cambios antes de cualquier retorno. Volver directamente al origen dejaría esas escrituras fuera de la historia activa.

Para el ensayo de código usá `TestBaseline16CopyPreservesRowsAndRollsBack` y `TestBaseline16CopyApplicationAndArchivedCalculations` con los recursos descartables del laboratorio. El segundo usa los permisos de ejecución y prueba las rutas clínicas de Go con adjuntos sintéticos. La restauración integral de producción, su ventana de recuperación y la validación del proveedor externo quedan para SP-04E.

### Evidencia del laboratorio

En PostgreSQL 17 y 18, el fixture principal conservó 47 tablas de datos, 34 filas y 21 secuencias. Un fallo simulado justo antes de confirmar dejó el destino vacío y permitió reintentar. Un destino ocupado, un segundo proceso con el bloqueo, el mismo origen como destino y un catálogo de origen alterado fueron rechazados. En un ensayo aparte sobre PostgreSQL 17, el respaldo físico pasó `pg_verifybackup`. Su restauración validó con el `init-db` original de la versión 16 y pudo copiarse a 17 con datos sintéticos preservados.
