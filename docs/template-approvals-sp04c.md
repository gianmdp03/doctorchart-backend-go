# Aprobación de plantillas clínicas SP-04C

Una aprobación corresponde a la especialidad, la versión de borrador y la huella SHA-256 que calcula PostgreSQL sobre la representación UTF-8 del objeto `jsonb`. El objeto incluye la definición del wizard y la configuración ordenada de navegación, pestañas y acciones de la ficha. Los nombres y las descripciones del catálogo son metadatos administrativos y se pueden actualizar por separado. El orden de las propiedades y el espacio en blanco del JSON de entrada no cambian la huella.

## Operación del catálogo

1. Guardar la definición y la experiencia como borrador. Cada escritura crea una revisión pendiente. Si hay una publicación aprobada vigente, sigue disponible.
2. Consultar `GET /api/doctorchart/admin/specialties/{id}/wizard/reviews` para obtener la revisión y la huella calculada por el servidor. La definición y la experiencia se consultan en los endpoints administrativos de la misma especialidad. La revisión debe seguir siendo la misma durante la evaluación externa.
3. Registrar la aprobación con el botón del panel o con `POST` en el endpoint de revisiones, enviando sólo `expectedVersion`. El servidor valida la identidad y constancia, y conserva el contenido aprobado y la hora de PostgreSQL.
4. Publicar la revisión aprobada. El servidor revalida la aprobación dentro de la transacción y aplica en conjunto la definición y la experiencia. Al publicar, la especialidad queda activa.
5. Para retirar la aprobación, usar el historial de decisiones. La revocación se registra una sola vez con la identidad autenticada, la hora y el motivo. Una publicación revocada pierde elegibilidad para altas y nuevas reservas públicas. Reactivarla requiere una publicación con aprobación vigente.

La desactivación administrativa conserva la aprobación. Se puede reactivar una publicación desactivada si su aprobación sigue vigente. Los contextos personales que ya completaron el alta conservan su acceso, definición, historial, registros y turnos. Los médicos pueden continuar la atención en esos contextos y gestionar su agenda privada. Los borradores personales de altas incompletas se conservan. Para finalizar el alta inicial se necesita la publicación aprobada y activa que corresponde a su versión de origen. Restablecer la configuración desde el catálogo exige una publicación aprobada vigente. La revocación conserva el estado de las consultas registradas. La anulación de una consulta sigue su propio flujo.

Las tres publicaciones iniciales se conservan como heredadas. El backend bloquea nuevas selecciones y ofertas públicas basadas en ellas hasta que se apruebe y publique una revisión. La instalación nueva y las copias del baseline 17 aplican la misma política. Las 16 plantillas `PROVISIONAL_TEST_ONLY` continúan como artefactos importables en forma de borrador.

## Autoridad y constancia

El Gateway autentica al actor y entrega su identidad y rol verificados. Un `SUPER_ADMIN` puede editar el catálogo, publicar una revisión que ya tiene aprobación válida y registrar una revocación. Para registrar una aprobación clínica necesita además una entrada coincidente en `DOCTORCHART_TEMPLATE_REVIEW_MANIFEST`, la configuración del servidor que representa una constancia externa ya validada. El manifiesto es un arreglo JSON de entradas con `specialty` (código), `revision` (versión), `fingerprint` (huella del servidor), `reviewer` (identidad autenticada exacta del operador autorizado a registrar la decisión) y `reference` (identificador verificable de la constancia). La referencia debe apuntar a un registro externo sin exponer datos de pacientes.

El responsable clínico debe definir quién certifica la revisión profesional, cómo se verifica su habilitación y custodia la constancia, y quién puede incorporar las entradas al manifiesto. Configurar el manifiesto requiere acceso operativo al servidor. Una configuración vacía bloquea aprobaciones clínicas nuevas. Retirar una entrada del manifiesto impide registrarla de nuevo. Para retirar una aprobación ya registrada corresponde revocarla en DoctorChart.

Las aprobaciones sintéticas quedan identificadas en el historial. El binario regular del API configura la sesión para rechazarlas. El laboratorio M6 requiere el binario con `doctorchart_template_lab`, el revisor de prueba configurado y la conexión a la instancia descartable que identifica el entorno M6. El fixture de arranque registra aprobaciones sintéticas sólo para las tres plantillas iniciales de ese laboratorio.

## Transición del esquema

Desde SP-04D, `baseline-copy` crea destinos en el baseline 19. Para leer un origen 17 se agrega `--source-baseline-17`. Consultá [la transición vigente](clinical-access-sp04d.md).

El baseline 18 es el esquema consolidado de una instalación nueva. El arranque valida marcador y catálogo y rechaza el baseline anterior. Para conservar una base 17, `baseline-copy --confirm-empty-target` verifica que el origen sea el baseline 17 esperado y copia sus datos a un destino vacío del mismo PostgreSQL mayor, codificación y colación. La copia mantiene el origen en lectura, conserva las revisiones clínicas y valida las referencias antes de confirmar. El destino conserva las publicaciones heredadas sin emitir aprobaciones. Se puede volver al origen conservado con el binario compatible con 17, siguiendo las precauciones de respaldo y almacenamiento de [la transición anterior](baseline-16-to-17.md).

La regresión de copia usa el SQL original del baseline 17 con su checksum fijado. Configurar `DOCTORCHART_BASELINE17_FIXTURE_SQL` con la ruta de ese artefacto y `BASELINE_TEST_ADMIN_DSN` con el PostgreSQL descartable autorizado para ejecutar `go test ./db -run TestBaseline17Copy`.
