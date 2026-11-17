# Revisiones y anulaciones clínicas

Las fichas y consultas persistidas se pueden corregir con control optimista de versión. Los antecedentes que se editan desde el resumen también generan una revisión de la ficha. Cada revisión contiene el estado anterior, con sus valores y el snapshot aplicable, el autor autenticado, la fecha del servidor, las versiones anterior y nueva y el motivo si se informó. Los cambios de fecha y las anulaciones de aplicaciones de vacunas generan revisiones de la dosis.

Las anulaciones exigen motivo y conservan el registro con su identificador. Las rutas `DELETE` de ficha, consulta, receta u orden y estudio reciben `{"reason":"…"}`, con `expectedVersion` opcional. Angular envía la versión que el médico está viendo al anular una ficha. La anulación de la ficha también anula los registros dependientes vigentes, conserva los vínculos de turnos y retiene los archivos. Los servicios de resumen, impresión y descarga habitual excluyen registros anulados.

## Acceso y almacenamiento

- `GET /patients/{id}/revisions` consulta el historial de la ficha. Acepta `before` para paginación y `encounterId` para restringirlo a una consulta.
- `GET /patients/archived` permite localizar fichas anuladas del contexto y admite la misma paginación.
- `GET /patients/{patientId}/revisions/studies/{studyId}/file` abre el archivo de un estudio anulado para el dueño autorizado. La referencia sigue en el catálogo de estudios, por lo que el archivo conserva su asociación y queda protegido de la cola de limpieza.
- `GET /patients/{id}/revisions/calculations` permite leer los cálculos guardados de una ficha o consulta anulada. Admite `before` y `encounterId`. Requiere propietario, contexto y habilitación clínica de calculadoras. La vista de historial de la ficha ofrece ese acceso de solo lectura.

Estas rutas exigen el contexto clínico y el propietario efectivo. Para documentos, estudios y vacunación se aplican las habilitaciones del contexto y el rol requerido por el módulo. Angular cancela lecturas del historial al cambiar de cuenta o contexto.

PostgreSQL registra cada revisión en la misma transacción que modifica su entidad. El rol ordinario tiene solo lectura sobre `clinical_revisions` bajo RLS. El trigger y las restricciones protegen las escrituras de la aplicación. Los administradores de PostgreSQL o de la infraestructura conservan sus facultades privilegiadas.

## Datos previos y operación

La primera corrección de un registro previo guarda los valores y la versión que existían en ese momento. Las altas conservan los metadatos de cada entidad. El historial comienza con las mutaciones registradas por SP-04B. Los cálculos guardados son registros de solo adición y quedan físicamente asociados a la ficha o consulta. Sus registros dejan de aparecer en el historial habitual del calculador al anular la ficha o la consulta.

El baseline consolidado sube de 16 a 17 bajo la política de preproducción. El arranque 17 rechaza una base inicializada en 16 sin cambios. La [guía de transición](baseline-16-to-17.md) describe cómo llevar esos datos a un destino vacío. El historial identifica el estado anterior a la primera revisión registrada y conserva la versión que tenía el registro. Autores, fechas y versiones ausentes quedan sin registrar.

SP-04B trata como protegidos los registros persistidos. Queda por decidir qué acto profesional los considera asentados, cómo se documentan rectificaciones posteriores y quién es responsable de la custodia. La versión del registro es un control técnico de concurrencia.
