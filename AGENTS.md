---
production: true
---

# Política obligatoria de PostgreSQL

Leer este AGENTS.md antes de cualquier tarea relacionada con PostgreSQL. El campo production anterior es único y determina la política estructural. Si falta, está duplicado, es ambiguo o tiene un valor distinto de false/true, detener los cambios de esquema y solicitar aclaración. No cambiarlo automáticamente: sólo el propietario puede autorizar false→true; nunca cambiar true→false para evitar una migración. Una skill de Go/PostgreSQL debe respetar esta política.

## Preproducción (production=false)

Ninguna versión de este backend está en producción. Cuando cambien tablas, columnas, índices, constraints, relaciones, funciones, triggers u otros objetos PostgreSQL:

1. Editar directamente db/schema.sql, manteniéndolo como definición completa y definitiva del esquema inicial. Código y baseline evolucionan juntos.
2. No crear migraciones incrementales, UP/DOWN, cadenas históricas ni backfills para datos que serán descartados. No conservar compatibilidad innecesaria con esquemas anteriores de desarrollo ni reintroducir las migraciones preproducción retiradas en etapa3.
3. Actualizar consultas Go, modelos, fixtures y regresiones relacionadas. Conservar y actualizar la validación uniforme de catálogo/fingerprint, sin omitir controles ni aceptar drift recalculando hashes.
4. Verificar creación en una base PostgreSQL17 completamente vacía, reinicios, inicialización concurrente, seeds administrados, restricciones, integridad, transacciones/rollback y operaciones reales.
5. No modificar automáticamente bases existentes para adaptarlas. Ante incompatibilidad informar que se requiere una reinicialización autorizada, sin ejecutarla.

Ejemplo de decisión: agregar una columna durante preproducción significa modificar db/schema.sql y sus lectores/escritores/pruebas; no crear un archivo incremental.

## Producción (production=true)

Desde la primera versión productiva, el baseline inicial publicado db/schema.sql es inmutable. Todo cambio estructural posterior requiere una migración incremental versionada que preserve datos; no reescribir retrospectivamente el baseline para evitarla.

Gestionar orden, versión, checksums y estado previo esperado; utilizar el mismo criterio de transacciones y advisory locks en los tres backends, validar el catálogo resultante y probar sobre PostgreSQL descartable con datos representativos. Comprobar errores, rollback, reintentos y compatibilidad entre versiones/despliegues cuando corresponda. Preparar instrucciones de despliegue, respaldo y recuperación.

El flag productivo permite PREPARAR migraciones, no ejecutarlas contra una base real sin permiso explícito. El mecanismo de baseline/fingerprint deberá ampliarse para reconocer versiones posteriores de forma segura cuando llegue la primera puesta en producción. No implementar anticipadamente ese runner mientras no haga falta ni crear arquitecturas diferentes por backend.

Ejemplo de decisión: agregar una columna después de publicar el baseline requiere preparar una migración versionada, verificar conservación/rollback y documentar su ejecución; no ejecutarla automáticamente.

## Reglas incondicionales

- No crear worktrees; trabajar en los repositorios principales y preservar cambios ajenos, staged, unstaged y untracked.
- Nunca borrar datos ni ejecutar DROP sobre una base existente sin autorización explícita. Los recursos de ensayo descartables propios sólo se crean/destruyen cuando están autorizados y su identidad/puerto/label se verifican; no inferir ownership de una DSN de desarrollo.
- Preservar el mecanismo uniforme de etapa3: db/schema.sql embebido, db/init.go, db/catalog.go y pruebas equivalentes; drivers actuales, READ COMMITTED, contexto30s, lock_timeout10s, statement_timeout20s y advisory lock transaccional namespace17209/key del backend.
- Esquema, seeds necesarios y marcador se confirman atómicamente antes de HTTP/jobs. Los reinicios validan identidad/checksum/catalogo, conservan decisiones administradas y no adoptan, reparan ni vacían esquemas incompatibles.
- Conservar tablas funcionales de auditoría, historial, pagos, idempotencia, movimientos y seguridad. No eliminar funciones por nombres como history/events/ledger/snapshots.
- Esta política regula estructuras PostgreSQL; no confundir cambios en datos de negocio con cambios estructurales. No cambiar reglas comerciales por conveniencia.
- No hacer commits, pushes, despliegues ni resets de desarrollo salvo autorización expresa para esas acciones.
