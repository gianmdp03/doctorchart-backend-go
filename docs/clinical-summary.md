# Resumen clínico y registro de alergias

La primera pestaña de la ficha se llama **Resumen clínico**. Reúne alergias,
problemas activos, medicación vigente, pendientes, última consulta y vacunación.
Los datos filiatorios y su edición siguen disponibles debajo del resumen.
No se usa IA ni se considera vigente un diagnóstico o un medicamento por el
solo hecho de aparecer en una consulta o receta histórica.

## Datos y contratos

- `GET /api/doctorchart/patients/{id}/clinical-summary`: perfil clínico con sus
  registros originales, antecedentes previos sin fecha, última consulta y su
  seguimiento, calendario por edad y hasta ocho medicamentos de recetas
  recientes, identificados expresamente como de uso actual a confirmar.
- `PUT /api/doctorchart/patients/{id}/clinical-profile`: requiere `expectedVersion`
  y una operación `ADD`, `CONFIRM`, `RESOLVE` o `NO_KNOWN_ALLERGIES`.
  `ADD` recibe `kind` (`ALLERGY`, `PROBLEM`, `MEDICATION`, `PENDING`), `text`,
  `details`, `date` ISO y `certainty` (`REPORTED` o `CONFIRMED`). `CONFIRM` y
  `RESOLVE` reciben `entryId`. La respuesta es el paciente actualizado.
- Ambos endpoints verifican propiedad. Las escrituras requieren `EDIT_PATIENT`,
  bloquean la fila del paciente y verifican su versión. Las vacunas y recetas
  respetan la disponibilidad de sus módulos en la especialidad.

El perfil se guarda como datos de negocio en
`patients.personal_history.clinicalProfile`, usando el documento JSONB existente.
No cambia ningún objeto PostgreSQL, baseline, checksum, catálogo ni mecanismo de
inicialización. Las ediciones filiatorias preservan este documento. No hay
migración ni reconstrucción de bases que ejecutar para esta funcionalidad.

Cada entrada conserva ID, tipo, texto, detalle, fecha clínica, origen informado o
confirmado, fecha de registro y autor. Confirmar vigencia agrega `verifiedAt` sin
reemplazar la fecha clínica original. Cerrar agrega `resolvedAt` y conserva el
registro; no implica automáticamente ausencia de alergias. La ausencia conocida
requiere una confirmación explícita y se rechaza si existen alergias activas o
antecedentes de alergias aún registrados. Agregar una alergia invalida la
confirmación previa de ausencia. Los antecedentes previos no reciben fechas
inventadas ni se promueven automáticamente a problemas o tratamientos activos.

El resumen sólo toma el seguimiento de la última consulta, incluso si ésta
elimina el seguimiento anterior. El estado vacunal reutiliza el calendario y las
aplicaciones existentes; una dosis sin aplicación registrada exige revisar el
carnet. No se incorporan recomendaciones nuevas ni se infieren aplicaciones.

Los enlaces abren la consulta, la receta o la dosis exacta, o el registro clínico
original dentro de la ficha. Las fechas visibles usan `dd/MM/aaaa` y el selector
compartido. Cambiar de paciente o de usuario cancela lecturas y descarta borradores
de ese contexto; un conflicto de versión conserva el borrador para revisar.

## Verificación

- Frontend: `npx ng test --watch=false` (703 pruebas aprobadas) y compilación
  optimizada de producción con configuración pública sintética aprobada.
- Vista representativa renderizada por Angular: revisión en Chrome a 1440 px y
  390 px, en temas claro y oscuro, sin desbordamiento horizontal. Artefactos locales en
  `doctorchart-frontend/.angular/clinical-summary/`.
- Go 1.27.3: pruebas y vet del modelo de perfil aprobados:
  `go test internal/clinical/clinical_profile.go internal/clinical/clinical_profile_test.go`
  y el mismo comando con `go vet`.
- Prueba PostgreSQL preparada en `summary_integration_test.go`, usando únicamente
  el fixture descartable con opt-in existente. No se ejecutó sobre ninguna base.
- La puerta completa de Go está bloqueada por la dependencia preexistente
  `../bycode-image-email-service-go/pkg/media`, ausente en este checkout. El
  detector de carreras requiere CGO, no disponible en este entorno. La ejecución
  amplia también mostró un fallo de temporización ajeno a esta funcionalidad en
  `TestRulesRefreshCancelsInflightFetch`. No se modificaron dependencias ni se
  sustituyeron paquetes para ocultar estos resultados; no se verificó una
  integración completa con PostgreSQL.

Restaurada la dependencia compartida, ejecutar la puerta de Go de la skill y la
prueba de integración contra un recurso descartable autorizado antes de publicar.
No se realizaron commits, pushes ni despliegues.
