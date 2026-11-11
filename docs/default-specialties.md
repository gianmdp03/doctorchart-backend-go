# Especialidades predeterminadas

El catálogo embebido `internal/bootstrap/seed.json` incluye Pediatría
(`pediatrics`), Medicina General (`general_medicine`) y Dermatología
(`dermatology`). Cada nueva especialidad tiene borrador y publicación inicial
idénticos, con `schemaVersion: 1` y `published_version: 1`.

El bootstrap busca cada código sin distinguir mayúsculas. Crea únicamente las
especialidades ausentes y su wizard dentro de la transacción y el advisory lock
de `db.Ensure`, antes de habilitar HTTP/jobs. No altera especialidades presentes,
incluso inactivas, sus borradores, publicaciones, navegación o configuraciones
personales. Una instalación compatible que sólo contiene Pediatría recibe las
dos nuevas al iniciar esta versión. Reinicios e inicios concurrentes no duplican
los registros. Los controles de identidad, checksum y catálogo siguen vigentes;
un esquema incompatible continúa rechazándose.

No hay cambios de estructura ni migraciones por este catálogo de negocio.
El seed y wizard originales de Pediatría se conservan sin modificaciones.

## Valores iniciales

Ambas especialidades conservan los datos personales del sistema, incluyendo
nombre, apellido, DNI, nacimiento, sexo, contacto, cobertura y dirección. Sólo
los cinco campos obligatorios del sistema siguen siendo requeridos.

Medicina General activa antecedentes clínicos, quirúrgicos, internaciones y
familiares; tabaquismo y alcohol; motivo, enfermedad actual, antecedentes
relevantes, presión arterial, frecuencia cardíaca, temperatura, saturación, peso,
talla, examen, impresión diagnóstica, diagnóstico, tratamiento, estudios,
derivaciones y seguimiento/próximo control. Actividad física, alimentación,
sueño, vacunación como antecedente textual y controles preventivos están en un
bloque adicional inicialmente desactivado. No se utiliza el calendario pediátrico.

Dermatología activa antecedentes de enfermedades y tratamientos dermatológicos,
cáncer de piel personal/familiar, exposición y quemaduras solares e
inmunosupresión; motivo, inicio, duración/evolución y síntomas; localización,
cantidad, distribución, morfología, tamaño, color, bordes y cambios de lesiones;
examen e impresión diagnóstica; diagnóstico, tratamiento general, tópico y
sistémico, indicaciones, estudios y próximo control. Fototipo Fitzpatrick,
evaluación adicional (dermatoscopía, registro individual textual y notas),
fotografías/consentimiento documental, procedimientos, biopsias y evolución
textual de lesiones quedan en categorías inicialmente desactivadas. El fototipo
es una selección manual I–VI sin valor predeterminado ni inferencia.

Las dos incluyen un bloque inicial editable de antecedentes para enfermedades
previas/crónicas (`sys_conditions`), alergias y medicación habitual. Es propio de cada especialidad: se puede editar, desactivar o eliminar.
No se agrega automáticamente a la consulta ni se repone después de eliminarlo.
Únicamente los ocho campos originales de datos personales están protegidos.
Los bloqueos históricos de antecedentes se normalizan al leer, sin reescribir
publicaciones, respuestas ni snapshots persistidos.

Pacientes, historia/consultas, recetas y estudios utilizan las pestañas y
acciones existentes. Catálogo de medicamentos y asistente se configuran como
en Pediatría, sujetos a permisos y flags globales; no se ofrecen crecimiento,
vacunación pediátrica ni calculadoras en la navegación de las nuevas especialidades.

## Personalización y límites del contrato existente

El frontend obtiene el catálogo y la publicación por API; selección, pasos,
editor, borrador, reapertura y finalización usan los componentes y endpoints
actuales, sin defaults de producción duplicados en Angular.

`enabled` pertenece a categorías, no a campos. Los bloques se activan/desactivan
con el control existente; los campos individuales, incluidos signos vitales,
pueden editarse o quitarse y volver a agregarse con el editor existente. Los
campos clínicos nuevos son opcionales (`required: false`); estar incluidos en un
bloque activo no obliga a completarlos.

El contrato no soporta archivos fotográficos, firmas, listas repetibles de
lesiones, mapas corporales ni seguimiento visual. Fotografías, consentimiento,
lesiones, procedimientos y biopsias se representan sólo como documentación
textual opcional, con etiquetas explícitas. No se agregaron módulos clínicos.

## Validación

Las regresiones de `internal/bootstrap/specialties_test.go` validan ambos JSON
contra el validador real, incorporación sobre Pediatría administrada, rollback,
reinicios y conservación de especialidades inactivas/publicaciones; también
selección, personalización, persistencia, reapertura y finalización con el store
real sobre PostgreSQL descartable. La suite de bootstrap ya cubre creación
vacía, concurrencia y controles de esquema.

En frontend, `default-specialties.spec.ts` usa copias de prueba de las
publicaciones (`testing/default-specialties.json`) para ejercitar las tarjetas,
controles reales del editor, borradores, reapertura y finalización. Estas copias
no se importan en producción. Actualizarlas si cambian los defaults del backend.
