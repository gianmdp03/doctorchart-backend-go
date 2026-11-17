# Deuda previa a habilitación clínica y producción

Registro separado de SP-03F. No se implementan aquí cambios de permisos,
conservación, auditoría ni compatibilidad de datos históricos. Las dieciséis
plantillas posteriores a las tres iniciales siguen como PROVISIONAL_TEST_ONLY;
esa marca es documental y no bloquea una publicación administrativa.

## Auditoría, conservación y trazabilidad

Los registros conservan valores, formulario y contexto de evaluación. Eso permite
interpretar consultas anteriores cuando se agregan consultas o se publican otros
formularios. No equivale a una revisión inmutable de cada edición ni a un registro
completo de las lecturas clínicas. El código actual admite UPDATE y DELETE de
consultas y borrado de fichas, con control de propiedad/contexto y versión.

Antes del uso clínico real deben definirse y evaluarse conservación, correcciones,
borrado, trazabilidad de accesos, recuperación, identidad del autor y autenticidad
documental. El versionado optimista y el fingerprint del esquema no prueban por
sí solos cumplimiento clínico o normativo. No se reescriben registros en SP-03F.

## Acceso y copias de historias clínicas

Las fichas son independientes por propietario/contexto. No existe acceso clínico
compartido automático por DNI, registro de cuidador o función administrativa.
Debe evaluarse el procedimiento de acceso/entrega de una copia integral de la
historia clínica, con identificación del solicitante y alcance autorizado.
Los PDF actuales de recetas/órdenes no constituyen una exportación integral de
consultas, antecedentes y archivos. La operación institucional puede requerir
resolver la relación entre fichas contextuales y la historia del establecimiento.

## Datos sensibles y canales de salida

Secretaría tiene acceso administrativo por consultorio y operación, sin ficha,
diagnóstico ni notas clínicas. Puede ver identidad/contacto y especialidad del
turno. Las ofertas públicas, constancias y correos también pueden revelar la
especialidad. La política actual no distingue Psiquiatría u Obstetricia.
Los documentos clínicos pueden incluir diagnóstico, indicaciones y medicación.
Una copia impresa o descargada requiere tratamiento adecuado fuera de la aplicación.

No hay permisos por campo/sección. Ocultar campos es presentación y no convierte
su contenido en notas reservadas. Cualquier necesidad de lectores diferenciados,
redacción de metadatos, terceros autorizados o acceso compartido requiere una
decisión funcional y un diseño de autorización completo; no se implementa por
anticipación ni mediante CSS. Las obligaciones y condiciones aplicables deben
validarse para la jurisdicción, modalidad de prestación y proveedores efectivos.

## Compatibilidad histórica separada

Medicina General conserva `followUp`, sin proyección canónica de ese texto a
`followUpInstructions`. SP-03F utiliza la clave correcta en Clínica Médica y
mantiene intacta la especialidad anterior. Cualquier compatibilidad futura debe
tratar precedencia, vaciados explícitos, visibilidad, resumen e impresión sin
renombrar snapshots ni ejecutar backfills por conveniencia.

Ningún resultado de laboratorio constituye suficiencia médica, cumplimiento
normativo, firma clínica válida ni autorización de despliegue.
