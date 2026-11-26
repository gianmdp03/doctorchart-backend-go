# Inventario de campos y contrato del ZIP de Doctor Chart

Este borrador cubre las 57 tablas locales. Las rutas son relativas al ZIP. Las columnas indican el contrato propuesto de salida, sujeto a confirmar la estructura de los campos anidados y las fuentes externas antes de implementar su sección. [Plan de exportación](doctor-backup-plan.md) · [Esquema de PostgreSQL](../db/schema.sql)

## Cómo se relacionan los registros

- Cada fila conserva su clave primaria en el espacio de su entidad. Las relaciones usan las claves de su origen y conservan el contexto, también al abrir el ZIP en otro equipo. Los identificadores enteros y decimales exactos se escriben como texto en los campos documentados, las fechas civiles como `AAAA-MM-DD` y los instantes en RFC 3339.
- La ficha y la consulta conservan el formulario que usaron, sus valores y los resultados guardados. El contexto entrega por separado la definición actual y su borrador. La versión de catálogo vigente se registra con su procedencia cuando corresponda. Si se desconoce una definición histórica, el índice informa esa ausencia y muestra lo que sí quedó guardado.
- En las secciones siguientes, `datos/` precede a cada ruta y cada conjunto de registros usa NDJSON. `INDICE.html` enlaza a páginas estáticas por especialidad y ficha. El manifiesto registra la versión, el corte de cada fuente, el conteo de registros, las rutas, el tamaño y SHA-256 de cada archivo incluido. Los estudios y documentos se leen desde sus archivos dentro del ZIP.

## Proyección de PostgreSQL

Definir listas de campos explícitas en código. Los documentos anidados se verifican según el productor que los escribe y la versión de su estructura. Aplicar el mismo tratamiento en el índice, los datos y el manifiesto.

### Perfil, especialidades y wizards

| Tabla | Destino y alcance | Campos del esquema |
| --- | --- | --- |
| `doctor_workspaces` | `configuracion/espacio`. Por `user_id` del médico. | Incluir `id`, `created_at`, `patient_columns_config`, `quick_notes`, `updated_at`, `user_id`, `version`. |
| `doctor_specialty_contexts` | `configuracion/contextos`. Por espacio. Conservar contextos incompletos, `definition` y `draft_definition` como documentos distintos. | Incluir `id`, `workspace_id`, `specialty_id`, `definition`, `draft_definition`, `source_wizard_version`, `draft_source_wizard_version`, `version`, `current_step_key`, `onboarding_completed`, `created_at`, `updated_at`, `teleconsultation_enabled`. |
| `specialty_context_preferences` | `configuracion/preferencias`. Por contexto del médico. Conservar independientemente de las preferencias del espacio. | Incluir `specialty_context_id`, `version`, `patient_columns_config`, `quick_notes`. |
| `doctor_chart_configs` | `configuracion/graficos`. Por `user_id` del médico. Incluir configuraciones inactivas. | Incluir `id`, `chart_code`, `field_mappings`, `is_active`, `user_id`. |
| `doctor_calculator_preferences` | `configuracion/calculadoras`. Por espacio y especialidad. Incluir datos guardados aunque el módulo esté deshabilitado. | Incluir `id`, `created_at`, `updated_at`, `specialty_id`, `workspace_id`. |
| `doctor_calculator_selections` | `configuracion/selecciones-calculadoras`. Por preferencia del médico. | Incluir `preference_id`, `calculator_id`. |
| `specialties` | `referencias/especialidades`. Sólo especialidades referenciadas. Conservar el código y la versión. | Incluir `id`, `active`, `code`, `created_at`, `description`, `display_name`, `navigation`, `patient_record_actions`, `patient_record_tabs`, `updated_at`, `version`. |
| `chart_definitions` | `referencias/graficos`. Sólo definiciones referenciadas por una configuración o dato propio. | Incluir `id`, `chart_code`, `chart_type`, `description`, `is_system`, `required_slots`, `title`. |
| `clinical_calculation_definitions` | `referencias/calculadoras`. Por códigos e IDs usados en selecciones, fórmulas y cálculos. Mantener los valores históricos registrados si difieren del catálogo. | Incluir `id`, `category`, `category_label`, `code`, `description`, `display_order`, `evidence_reference`, `is_system`, `name`, `required_parameters`, `result_unit`, `short_name`, `summary_formula`. |
| `specialty_calculator_defaults` | `referencias/calculadoras-por-especialidad`. Por especialidad propia, para explicar valores por defecto. | Incluir `id`, `calculator_id`, `specialty_id`. |
| `wizard_defaults` | `referencias/wizards`. Por especialidad y referencia publicada que corresponda. El borrador global de administración queda fuera. | Incluir `published_approval_id`, `id`, `published_definition`, `published_version`, `specialty_id`. Proyección especial o exclusión de `draft_experience`, `draft_definition`, `version`, `updated_at`. |
| `wizard_approvals` | `referencias/versiones-wizard`. Por referencia de aprobación o versión identificable. Proyectar el contenido relevante de la definición y experiencia. Conservar la huella y el tipo como procedencia. | Incluir `id`, `specialty_id`, `revision`, `content`, `fingerprint`, `kind`, `occurred_at`. Proyección especial o exclusión de `actor`, `reference`. |
| `wizard_approval_revocations` | `referencias/revocaciones-wizard`. Sólo revocaciones vinculadas a la versión de referencia. Incluir motivo cuando forme parte de la información visible para el titular. | Incluir `approval_id`, `reason`, `occurred_at`. Proyección especial o exclusión de `actor`. |

### Pacientes, historia clínica y documentos

| Tabla | Destino y alcance | Campos del esquema |
| --- | --- | --- |
| `patients` | `clinica/fichas`. Por espacio y contexto del titular. Incluir fichas archivadas y conservar su identidad aunque el DNI coincida. | Incluir `id`, `annulled_at`, `address`, `allergies`, `birth_date`, `blood_type`, `chronic_conditions`, `email`, `family_history`, `form_definition_snapshot`, `form_values`, `formula_context`, `formula_results`, `full_name`, `gender`, `guardian_info`, `insurance_info`, `national_id`, `personal_history`, `phone`, `updated_at`, `user_id`, `version`, `workspace_version`, `workspace_id`, `specialty_context_id`. Proyección especial o exclusión de `media_identity`. |
| `clinical_encounters` | `clinica/consultas`. Por ficha. Conservar el snapshot literal del formulario, sus valores y los resultados guardados. | Incluir `id`, `annulled_at`, `chief_complaint`, `clinical_impression`, `consultation_type`, `diagnostic_orders`, `encounter_date`, `follow_up_date`, `follow_up_instructions`, `form_definition_snapshot`, `form_values`, `formula_context`, `formula_results`, `head_circumference_cm`, `height_cm`, `history_of_present_illness`, `physical_examination`, `prescriptions`, `primary_diagnosis_code`, `primary_diagnosis_description`, `secondary_diagnoses`, `treatment_plan`, `updated_at`, `version`, `vital_signs`, `weight_kg`, `workspace_version`, `patient_id`, `specialty_id`. |
| `patient_calculation_logs` | `clinica/calculos`. Por ficha. Resolver la consulta cuando `encounter_id` tenga valor. | Incluir `id`, `calculation_code`, `calculation_name`, `created_at`, `formatted_result`, `input_snapshot`, `plain_text_evolution`, `result_interpretation`, `result_unit`, `result_value`, `severity`, `user_id`, `encounter_id`, `patient_id`. |
| `patient_medical_studies` | `clinica/estudios`. Por ficha. Agregar la ruta del adjunto dentro del ZIP y su SHA-256. Leer los bytes mediante el almacenamiento autorizado. | Incluir `annulled_at`, `version`, `id`, `study_name`, `content_type`, `description`, `file_size`, `original_file_name`, `uploaded_at`, `patient_id`. Proyección especial o exclusión de `media_asset_id`, `stored_file_name`. |
| `patient_prescription_records` | `clinica/recetas-y-ordenes`. Por ficha. Conservar tipos, autoría y anulaciones. | Incluir `annulled_at`, `version`, `issuance_zone`, `id`, `author_user_id`, `created_at`, `diagnosis_description`, `general_instructions`, `type`, `patient_id`. |
| `prescription_diagnostic_orders` | `clinica/items-ordenes`. Por registro de receta u orden, respetando el orden original. | Incluir `record_id`, `study_description`, `item_position`. |
| `prescription_medications` | `clinica/items-medicacion`. Por registro de receta, respetando el orden original. | Incluir `record_id`, `administration_route`, `dosage`, `duration_days`, `frequency`, `instructions`, `medication_name`, `item_position`. |
| `patient_vaccinations` | `clinica/vacunas`. Por ficha. Resolver dosis y calendario. | Incluir `annulled_at`, `version`, `id`, `applied_at`, `dose_id`, `patient_id`. |
| `vaccination_doses` | `referencias/dosis`. Por dosis referenciada por vacunaciones propias. | Incluir `id`, `calendar_id`. |
| `vaccination_calendars` | `referencias/calendarios`. Por calendario de las dosis referenciadas. | Incluir `id`, `definition`. |
| `clinical_revisions` | `clinica/revisiones`. Por titular, contexto y ficha. Aplicar la proyección de cada entidad también a `previous_content`. | Incluir `id`, `entity`, `entity_id`, `patient_id`, `workspace_id`, `specialty_context_id`, `owner_user_id`, `actor_user_id`, `occurred_at`, `operation`, `previous_version`, `new_version`, `reason`, `previous_content`. |
| `clinical_access_events` | `auditoria/accesos`. Por titular y contexto verificados. Excluir sucesos de alcance indeterminado y los identificadores operativos de petición. | Incluir `id`, `occurred_at`, `actor_user_id`, `actor_kind`, `operation`, `entity_id`, `patient_id`, `record_version`, `owner_user_id`, `specialty_context_id`, `outcome`. Proyección especial o exclusión de `request_id`. |
| `clinical_document_issues` | `clinica/emisiones`. Por titular y contexto. Conservar código, origen y versión de emisión. Exportar `pdf` como binario en `documentos/`, vinculado por ID, y comprobar `sha256`. | Incluir `id`, `code`, `owner_user_id`, `specialty_context_id`, `patient_id`, `source`, `source_id`, `source_version`, `document_type`, `issuer_specialty`, `issued_at`, `sha256`. Proyección especial o exclusión de `pdf`. |
| `clinical_document_annulments` | `clinica/anulaciones-documentales`. Por emisión y alcance del titular. | Incluir `document_id`, `owner_user_id`, `specialty_context_id`, `annulled_at`, `reason`. |
| `clinical_export_evidence` | `auditoria/exportaciones-clinicas`. Por ficha y contexto. Verificar el alcance del manifiesto histórico antes de incluirlo. | Incluir `id`, `owner_user_id`, `specialty_context_id`, `patient_id`, `prepared_at`, `sha256`, `manifest`. |

### Consultorios, agenda y equipo

| Tabla | Destino y alcance | Campos del esquema |
| --- | --- | --- |
| `clinics` | `agenda/consultorios`. Por espacio. Incluir archivados y teleconsulta. | Incluir `id`, `workspace_id`, `name`, `country`, `province`, `city`, `address`, `timezone`, `status`, `version`, `created_at`, `updated_at`, `modality`. |
| `clinic_schedules` | `agenda/horarios`. Por consultorio y espacio. | Incluir `clinic_id`, `min_notice_minutes`, `max_advance_days`, `version`, `workspace_id`. |
| `clinic_specialty_contexts` | `agenda/consultorio-contextos`. Por consultorio y contexto propios. | Incluir `clinic_id`, `workspace_id`, `specialty_context_id`, `modality`. |
| `schedule_blocks` | `agenda/bloques`. Por consultorio, espacio y contexto. Mantener el vínculo a su excepción si existe. | Incluir `id`, `clinic_id`, `workspace_id`, `specialty_context_id`, `weekday`, `exception_id`, `start_minute`, `end_minute`, `slot_minutes`. |
| `schedule_exceptions` | `agenda/excepciones`. Por consultorio y espacio. | Incluir `id`, `clinic_id`, `workspace_id`, `from_date`, `through_date`, `label`. |
| `booking_publications` | `agenda/publicacion`. Por espacio. Revisar las estructuras de preguntas antes de exportar. | Incluir `workspace_id`, `public_id`, `name`, `description`, `published`, `version`, `questions`. |
| `clinic_publications` | `agenda/publicaciones-consultorios`. Por consultorio y espacio. | Incluir `clinic_id`, `workspace_id`, `public_id`, `enabled`, `address`, `modality`. |
| `publication_specialty_contexts` | `agenda/publicacion-contextos`. Por espacio y contexto propios. | Incluir `workspace_id`, `specialty_context_id`. |
| `bookings` | `agenda/turnos`. Por espacio. Conservar estado, vínculo opcional a ficha, instantáneas y procedencia. | Incluir `id`, `workspace_id`, `clinic_id`, `patient_id`, `start_at`, `end_at`, `status`, `version`, `contact_name`, `contact_phone`, `created_at`, `updated_at`, `attendee_name`, `specialty_context_id`, `specialty_snapshot`, `clinic_snapshot`, `provenance`. |
| `booking_events` | `agenda/historial-turnos`. Por turno y versión. Revisar `before_document` y `after_document` con una proyección explícita. | Incluir `booking_id`, `version`, `kind`, `actor_id`, `occurred_at`, `before_document`, `after_document`. |
| `booking_public_details` | `agenda/respuestas-reservas`. Por turno. Conservar preguntas y respuestas, contacto y referencia visible. | Incluir `booking_id`, `reference`, `form_version`, `form_snapshot`, `answers`, `document_country`, `document_type`, `document`, `email`, `doctor_name`, `clinic_name`, `address`, `timezone`. Proyección especial o exclusión de `challenge_hash`. |
| `staff_access` | `equipo/delegaciones`. Por espacio. Una cuenta de secretaria puede servir a varios titulares. Las delegaciones propias fijan el alcance. | Incluir `id`, `workspace_id`, `account_id`, `email`, `name`, `phone`, `permissions`, `active`, `version`. |
| `staff_access_clinics` | `equipo/delegaciones-consultorios`. Por delegación y consultorio dentro del espacio. | Incluir `access_id`, `workspace_id`, `clinic_id`. |
| `staff_accounts` | `equipo/contactos`. Sólo contactos alcanzables desde una delegación propia. El estado de la cuenta compartida se determina en su fuente. | Incluir `id`, `email`, `name`. Proyección especial o exclusión de `password_hash`, `recovery_hash`, `created_at`. |
| `staff_security_events` | `equipo/historial-accesos`. Sólo eventos atribuibles por `access_id` a una delegación propia. El alcance de un evento de cuenta compartida requiere evidencia adicional. | Incluir `id`, `account_id`, `access_id`, `kind`, `occurred_at`. |
| `notification_outbox` | `agenda/historial-notificaciones`. Sólo estado de notificaciones de agenda y equipo atribuibles al médico por turno o delegación. Excluir recuperación de cuenta y códigos de portal como categorías. | Incluir `id`, `kind`, `booking_id`, `booking_version`, `access_id`, `due_at`, `state`, `created_at`, `sent_at`. Proyección especial o exclusión de `dedup_key`, `recovery_id`, `payload`, `attempts`, `lease_until`, `lease_id`, `last_code`, `portal_code_id`. |

### Medios e infraestructura

| Tabla | Destino y alcance | Campos del esquema |
| --- | --- | --- |
| `media_assets` | `clinica/estudios`. Proyectar metadatos útiles del documento vinculado al estudio y comprobar titularidad y referencia. Exportar tamaño, tipo y huella disponibles y el archivo conservado. Omitir claves y ubicaciones internas. | Proyectar según el alcance descrito. |
| `media_events` | Diario operativo de archivos. El historial clínico del estudio y sus anulaciones se exporta desde sus registros clínicos. | Tabla excluida. |
| `app_schema_state` | Configuración y huellas internas del esquema. | Tabla excluida. |
| `booking_confirmation_access` | Credenciales efímeras de confirmación. La referencia de negocio se obtiene de la reserva. | Tabla excluida. |
| `booking_idempotency` | Estado técnico de repetición de solicitudes. Los turnos y su historial cubren los datos de negocio. | Tabla excluida. |
| `booking_portal_codes` | Pruebas, códigos y sesiones de acceso de pacientes. | Tabla excluida. |
| `clinic_entitlement_epoch` | Generación de caché de permisos comerciales. | Tabla excluida. |
| `public_booking_rate_windows` | Contadores internos de seguridad. | Tabla excluida. |
| `staff_email_recovery` | Credenciales y estado de recuperación de cuentas. | Tabla excluida. |
| `staff_invitations` | Tokens de invitación. El estado visible del equipo se obtiene de la delegación. | Tabla excluida. |
| `staff_rate_windows` | Contadores internos de seguridad. | Tabla excluida. |
| `staff_sessions` | Credenciales y sesiones de acceso. | Tabla excluida. |
| `study_file_deletions` | Intenciones operativas de borrado de archivos. | Tabla excluida. |

## Fuentes externas y datos locales

| Fuente comprobada | Contrato propuesto |
| --- | --- |
| Perfil profesional y cuenta compartida | Tomar los campos que usa el perfil, como nombre, correo, DNI y matrícula, y los datos de presentación que usa la impresión profesional. Resolver nombre y teléfono desde el perfil central donde corresponda. Determinar si la foto está disponible como archivo propio. Usar una proyección explícita de los metadatos de autenticación. [Identidad impresa](../../doctorchart-frontend/src/app/core/models/doctor-print-identity.ts) · [Perfil de cuenta](../../bycode-backend-go/internal/billing/account_profile.go) |
| Billing | Inventariar transferencias y comprobantes accesibles al titular. Identificar Doctor Chart por el producto de la operación. En una compra compartida, verificar el alcance de los datos y comprobantes antes de exportar. Determinar los campos autorizados antes de incorporar la sección. [Lecturas de transferencias](../../bycode-backend-go/internal/billing/raya_http.go) |
| Archivos de estudios | Usar el lector de archivos administrados y legados. Verificar tamaño y huella conservada cuando exista y generar la huella del archivo exportado. Asociar una ruta local con identificadores opacos. [Lector de almacenamiento](../internal/storage/store.go) |
| Navegador de origen | Tema y tamaño de letra son del navegador y carecen de titularidad de cuenta en su almacenamiento actual. Contexto, selección de paciente y borrador de agenda están en la sesión. Antes de incorporar datos locales, definir cómo confirmar su titularidad y dejar registrada su procedencia. [Almacenamiento de preferencias](../../doctorchart-frontend/src/app/core/services/browser-ui-storage.service.ts) |
| Estado en memoria | Borradores clínicos y conversación del asistente existen en memoria durante la sesión. Para incluirlos con garantía de cobertura, se necesita una decisión sobre guardado y alcance. [Borradores clínicos](../../doctorchart-frontend/src/app/features/patient-record/patient-record-drafts.service.ts) · [Asistente](../../doctorchart-frontend/src/app/core/services/ai-chat.service.ts) |

## Protección y apertura

Probar ZIP AES con una clave aleatoria por exportación y una herramienta de extracción compatible. La extensión AES del ZIP deja visibles los nombres de las entradas. Usar nombres genéricos sin datos personales para el ZIP y sus rutas. Mostrar títulos de estudios y nombres de pacientes sólo dentro del contenido cifrado. La prueba incluye descifrado local, extracción completa y navegación sin conexión. [Especificación ZIP AES de WinZip](https://www.winzip.com/en/support/aes-encryption/) · [Metadatos visibles](https://kb.winzip.com/hc/en-us/articles/55550892076564-About-Encryption)

## Primera prueba de punta a punta

1. Preparar datos sintéticos de un médico con dos especialidades, una con wizard completo y la otra con borrador. En la primera, guardar una consulta con una definición histórica distinta de la actual y adjuntar un estudio. Crear además un PDF emitido. Agregar una ficha y un adjunto de otro médico para comprobar aislamiento.
2. Verificar que el exportador capture ambos contextos, relaciones, versiones, nulos y valores exactos. Comparar SHA-256 del PDF y del estudio contra el origen. Comprobar que cada enlace local resuelva al archivo correcto.
3. Extraer el ZIP fuera del servicio y recorrerlo desde `INDICE.html`. Confirmar que la ficha muestra la definición histórica y que se pueden abrir el PDF y el adjunto. Comprobar aislamiento con la otra cuenta, falla ante un adjunto faltante y ausencia de datos personales en los nombres de las entradas.

La implementación incluye Billing y avatar, los ajustes y borradores de agenda de la pestaña cuando el titular los selecciona, y acceso autenticado con plan vencido. Los borradores clínicos y el chat en memoria quedan fuera. La proyección de los estudios incluye sus metadatos clínicos, los archivos y sus huellas. El catálogo de medios se usa para validar la titularidad, la asociación al paciente y el contenido del archivo administrado. [Generación del ZIP](../internal/dataexport/archive.go) · [Validación de archivos](../internal/storage/store.go) · [Datos de facturación](../../bycode-backend-go/internal/billing/doctorchart_export.go)

