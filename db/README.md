# Esquema inicial de DoctorChart

`db/schema.sql` define la estructura completa de la versión 20. Contiene 55 tablas y 70 claves foráneas. El código embebe ese archivo y el bootstrap carga los seeds administrados. Las entregas históricas de M1–M6 se conservan en `docs/`.

## Arranque

- Para una base vacía, el administrador de PostgreSQL provisiona dos logins independientes, `doctorchart_app` y `doctorchart_media`, con credenciales distintas y sin membresías ni privilegios administrativos. El rol que inicializa es dueño de la base de DoctorChart y de sus objetos.
- Ejecutá `doctorchart init-db` una sola vez con `DATABASE_URL` del dueño del esquema en el entorno de ese proceso. El comando crea el catálogo y concede los permisos de los dos logins. Los secretos del dueño corresponden al proceso de inicialización.
- El servidor HTTP usa `DATABASE_URL` de `doctorchart_app` y `MEDIA_DATABASE_URL` de `doctorchart_media` para almacenamiento gestionado. Comprueba el rol, el marcador y el fingerprint antes de iniciar HTTP o trabajos de fondo.
- La inicialización usa READ COMMITTED, un contexto de 30 s, `lock_timeout` de 10 s, `statement_timeout` de 20 s y el advisory lock transaccional `(17209, 3)`. Estructura, seeds y marcador se confirman juntos antes de habilitar HTTP o jobs.
- Un reinicio valida el marcador y el catálogo, y conserva los registros y las decisiones administradas. El fingerprint se deriva de la definición aplicada a una base vacía de cada versión mayor de PostgreSQL.
- Un esquema previo o incompatible requiere una reinicialización autorizada. Guardá un respaldo físico verificable y la configuración asociada antes de vaciar una base de desarrollo. El backend rechaza una base incompatible al iniciar.

## Aislamiento clínico

Go valida la cuenta y el contexto de especialidad y fija ambos en la transacción. Las políticas RLS del rol de la app limitan lecturas y escrituras en fichas, consultas, recetas, estudios, vacunación, cálculos clínicos y las líneas de recetas. El rol de medios accede al catálogo operativo de adjuntos, su cola y el marcador de esquema. Para verificar referencias antes de compactar la recuperación, puede consultar únicamente la columna `media_asset_id` de estudios. La API verifica la ficha antes de abrir un archivo.

Las excepciones devuelven un resultado acotado. El cupo de pacientes devuelve el total de la cuenta. El trabajo de archivos consulta si una clave de su cola conserva una referencia. Los turnos usan la especialidad persistida del turno al vincular una ficha.

La identidad de la transacción depende del backend y de la protección de las credenciales de PostgreSQL. RLS cubre filtros SQL omitidos en consultas de la aplicación.

## Pruebas

Las suites `./db`, `./internal/bootstrap` y `./cmd/api` cubren catálogo, RLS, reinicio, concurrencia, rollback y seeds. Usá PostgreSQL descartable con los dos roles provisionados. El comando `go-gates` de `ops/testing/specialty-m6-lab.mjs` configura las conexiones y las contraseñas sintéticas de ambos roles para la suite Go completa.

Validación del 11/11/2026 en Docker Linux con Go 1.27.3: compilación, vet y suite completa en PostgreSQL 18; suite con detector de carreras en PostgreSQL 17 y repetición final de las suites de base y clínica tras ajustar el permiso de bloqueo de la cola. La prueba de navegador completó 17 recorridos y 90 solicitudes con tres especialidades, descargas autorizadas, rechazos cruzados y entrega de todos los eventos de medios. Auth y Billing fueron adaptadores locales. La regresión de adjuntos usa los dos logins restringidos, una conexión por pool y verifica borrado, retención y recuperación. Una instancia con un esquema anterior requiere una reinicialización autorizada para activar la versión 16.

La [guía local de Windows](../ops/local/README.md) indica cómo arrancar y detener la instancia persistente.
