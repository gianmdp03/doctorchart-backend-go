# Resumen y antecedentes clínicos

El resumen es una vista de lectura alimentada por los datos del paciente y sus
consultas. No tiene un formulario paralelo de alergias, enfermedades ni medicación.

## Bloque común a todas las especialidades

`sys_clinical_history` (Antecedentes clínicos) pertenece al sistema y aparece en
los formularios de paciente y consulta. Su categoría, campos, etiquetas y opciones
no pueden modificarse, desactivarse ni eliminarse. El editor permite consultar
su estructura y explica su función. El servidor valida la definición canónica.

Campos estables:

- `sys_allergyStatus` y `sys_allergies`: alergias y reacciones conocidas.
- `sys_conditionStatus` y `sys_conditions`: enfermedades o problemas actuales.
- `sys_medicationStatus` y `sys_medications`: medicación habitual, con dosis y
  frecuencia cuando se conozcan.
- `sys_historySource`: `REPORTED` (paciente o familia) o `CONFIRMED` (corroborado
  por el profesional). En ausencia de selección no se presume confirmación.

Cada sección distingue `UNKNOWN`, `NONE` y `PRESENT`. Un positivo requiere detalle;
no se admite detalle positivo junto a una respuesta negativa. Un registro por
línea, con límite de 4000 caracteres por sección. Dejar una consulta sin información
revisada no borra lo registrado anteriormente. Las respuestas viajan en los
endpoints habituales de creación/edición, dentro de `values`.

Al crear al paciente ya se pueden registrar los tres grupos. La consulta muestra
los antecedentes disponibles como referencia y permite revisarlos con un clic,
sin reescribirlos. No se copian silenciosamente a consultas posteriores ni se
convierten en una nueva confirmación sin una acción del profesional.

## Resumen y procedencia

`GET /api/doctorchart/patients/{id}/clinical-summary` devuelve `history`, con el
estado más reciente por grupo entre paciente y consultas. Cada grupo tiene sus
ítems, origen informado/corroborado, fecha y referencia exacta de origen
(`PATIENT` o `ENCOUNTER`, `sourceId`). Una consulta que sólo actualiza enfermedades
no desplaza los datos anteriores de alergias y medicación. Una revisión negativa
más reciente reemplaza el positivo en el resumen, conservando la consulta original.

Las fechas de revisión del formulario del paciente se guardan en el documento
existente `personal_history.clinicalHistoryDates`. Una edición de teléfono u otro
dato filiatorio no altera esas fechas. Las consultas usan su fecha clínica. Los
antecedentes sin fecha permanecen identificados como tales.

Se muestran además hasta cinco diagnósticos distintos registrados en consultas,
con enlace y fecha, expresamente como de vigencia a revisar. Un diagnóstico
histórico no se convierte automáticamente en un problema activo. Tampoco se
interpreta texto narrativo mediante IA. Las recetas previas aparecen como de uso
actual a confirmar, no como medicación habitual por el solo hecho de ser emitidas.
El seguimiento usa únicamente la última consulta. Las vacunas reutilizan el
calendario por edad y las aplicaciones existentes; una dosis sin aplicación
registrada requiere revisar el carnet.

## Compatibilidad y PostgreSQL

No cambia ninguna tabla, columna, índice, constraint, baseline, checksum ni
fingerprint. No hay migraciones ni reconstrucciones que ejecutar.

Las configuraciones anteriores se complementan en memoria con el bloque común al
leerlas. No se reescriben masivamente las configuraciones de los médicos ni se
alteran los seeds administrados. Los snapshots clínicos existentes se leen sin
reescribirlos. Al guardar una edición explícita se conserva el formulario original
y se incorpora el bloque común si faltaba. Los clientes anteriores que omiten
secciones clínicas en una edición no borran esas respuestas.

Los registros y el endpoint de perfil de la implementación anterior se mantienen
por compatibilidad y conservación de datos. La interfaz ya no usa ese endpoint
para cargar antecedentes; sus registros previos se conservan como fuentes de
lectura. Todas las lecturas/escrituras mantienen propiedad, permisos y versiones
optimistas. No se ejecutaron escrituras sobre bases existentes, commits, pushes
ni despliegues durante esta implementación.

## Verificación

- Frontend: suite completa, 79 archivos y 710 pruebas aprobadas.
- Compilación optimizada de producción con configuración pública sintética.
- Revisión visual del resumen y formulario en Chrome, a 1440 y 390 px, temas
  claro/oscuro y sin desbordamiento horizontal. Artefactos locales en
  `doctorchart-frontend/.angular/clinical-summary/`.
- Go 1.27.3: pruebas y vet de wizard/workspace; pruebas y vet del agregado de
  antecedentes aprobados, con `go test internal/clinical/record_history.go
  internal/clinical/record_history_test.go` y el comando equivalente de vet.
- Pruebas PostgreSQL preparadas en `summary_integration_test.go` para creación
  con antecedentes, combinación por grupo, revisión negativa, propiedad,
  conflictos y preservación. No se ejecutaron sobre una base en este entorno.
- La puerta completa Go sigue bloqueada por la dependencia local preexistente
  `../bycode-image-email-service-go/pkg/media`, ausente. El entorno tampoco dispone
  de CGO para el detector de carreras ni de govulncheck. La suite amplia vuelve a
  detectar el fallo ajeno `TestRulesRefreshCancelsInflightFetch`. Registro local:
  `.migration/clinical-history/go-quality.log`.

Restaurada la dependencia compartida, ejecutar la puerta de Go de la skill y las
pruebas de integración contra un recurso PostgreSQL descartable autorizado antes
de publicar. La integración completa con PostgreSQL todavía no está verificada.
