# PostgreSQL baseline 1 — DoctorChart

El único DDL funcional está en `db/schema.sql`, embebido por `db/init.go`. Crea 21 tablas incluyendo `app_schema_state`. Se conserva pgxpool; cada aplicación usa su propia base. No hay cadenas de migraciones preproducción ni adopción de esquemas anteriores.

## Arranque y transacción

`Ensure` abre una transacción READ COMMITTED con contexto de 30 segundos, lock_timeout de 10 segundos, statement_timeout de 20 segundos y advisory lock transaccional `(17209, 3)`. Fija search_path a public,pg_catalog. El arranque normal ejecuta DDL, seeds idempotentes y marcador dentro de esa transacción, antes de atender HTTP/workers. Un error revierte todo. Los reinicios validan antes del seed; los seeds completan sólo registros necesarios ausentes y preservan configuraciones administradas.

Una base sin marcador sólo se inicializa si está vacía: se rechazan objetos de usuario, extensiones ajenas y esquemas adicionales. No se reparan ni borran bases parciales. Un esquema creado manualmente sin marcador tampoco se adopta: arrancar el backend sobre una base vacía es el procedimiento admitido.

## Contrato y fingerprint

El marcador registra backend, baseline_version=1, SHA-256 del SQL embebido (LF/CRLF equivalentes), SHA-256 del catálogo canónico e initialized_at. En cada reinicio se comprueban identidad, versión, SQL y catálogo real. Incluye columnas/posición/tipo/nullability/default/identity/generated/collation, PK/FK/CHECK/UNIQUE, definiciones/estados de índices, secuencias y propiedad, rutinas, triggers, reglas, vistas, políticas, particiones, tipos, collations, operadores y extensiones. PostgreSQL proporciona definiciones normalizadas; el orden incluye la definición para desempatar nombres de restricciones repetidos. Se excluyen OIDs, filas, valores corrientes de secuencias, propietarios y permisos de despliegue. No es un control de seguridad frente a un administrador capaz de falsificar el marcador.

El fingerprint está destinado a PostgreSQL 17. Una actualización mayor o cambio legítimo de DDL exige una transición explícita y sus pruebas; nunca editar hashes para ocultar drift. Objetos nuevos ajenos al baseline también provocan rechazo. Los agregados no forman parte del baseline y su presencia queda registrada para detectar su incorporación.

## Pruebas descartables

`db/init_test.go` crea y elimina exclusivamente bases stage3_baseline_doctorchart_* usando un administrador de ensayo loopback de puerto alto y usuario migration_fixture. `BASELINE_TEST_ADMIN_DSN` debe apuntar al postgres administrativo de ese contenedor propio. Cubre vacío/reinicios, secuencias, 16 pools independientes, exclusión/cancelación, rollback SQL y seed, identidad/hash/version y drift sin reparación. Las pruebas de negocio añaden seeds, estados administrados y operaciones reales; no ejecutar fixtures destructivas contra bases de desarrollo.

## FUTURO inicio limpio de desarrollo — no ejecutado

1. Detener los consumidores del backend y confirmar explícitamente la identidad de la base local de desarrollo que se quiere descartar. Conservar cualquier exportación que se necesite.
2. Crear una base nueva vacía de PostgreSQL 17 con su usuario dedicado. Cambiar únicamente la configuración local de conexión del backend para apuntar a esa base; no reutilizar el esquema anterior ni aplicar SQL históricos.
3. Arrancar el backend normalmente con las demás configuraciones válidas. Su bootstrap confirma esquema, seeds y marcador atómicamente. Comprobar readiness, tablas y recorridos esenciales; reiniciar para validar idempotencia.
4. Cambiar consumidores y comprobar integraciones antes de retirar la base anterior. La eliminación de datos existentes requiere una autorización posterior explícita; esta etapa no la ejecuta.

Billing usa DB_URL con esquema postgres/postgresql y DB_USERNAME/DB_PASSWORD cuando corresponda; Pointly y DoctorChart usan DATABASE_URL. JDBC y los aliases Spring de conexión DB ya no son un mecanismo de arranque.

## Migraciones futuras

Después de la primera versión productiva, un cambio de esquema necesitará una migración versionada explícita, checksum, estado anterior esperado, transacción/exclusión y actualización del catálogo/marcador junto al cambio. Probar actualización, conservación y rollback sobre copia descartable. No se ha añadido un runner ni una cadena vacía preventiva; el bootstrap actual sólo acepta baseline 1 y falla ante cualquier versión distinta.
