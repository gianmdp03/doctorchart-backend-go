# Exportación de la práctica del médico

## Resultado esperado

La exportación está disponible en el código de Doctor Chart mediante **Perfil profesional → Copia de seguridad** y **Mi cuenta → Copia de seguridad**. La entrada de Mi cuenta también funciona para el titular autenticado con plan vencido o configuración incompleta. [Pantalla de copia](../../doctorchart-frontend/src/app/features/practice-export/practice-export-page.component.html)

Desde **Perfil profesional → Copia de seguridad**, el médico puede preparar y descargar un solo ZIP con sus datos, PDFs y adjuntos. Puede conservarlo y consultarlo en su equipo sin conexión a Doctor Chart. El alcance incluye todas sus especialidades, las relaciones entre registros, los borradores guardados y los archivos disponibles, aunque la función que los creó esté deshabilitada.

Cada archivo representa los datos disponibles al momento de preparar la copia. Su índice explica qué contiene cada sección y permite encontrar los registros por especialidad y paciente, con enlaces locales a los documentos y adjuntos correspondientes.

El criterio de entrega es comparar el contenido exportado con la práctica de origen y poder recorrerlo con el servicio desconectado. El plan parte del ZIP contextual que ya existe en el repositorio. [Exportación clínica existente](documents-export-recovery-sp04e.md)

## Qué incluye la copia

| Área | Qué conserva la copia |
| --- | --- |
| Perfil profesional y configuración | Datos profesionales y de presentación, especialidades activas e incompletas, formularios personalizados con sus borradores, pasos de configuración, notas rápidas, columnas, gráficos y preferencias de calculadoras. Se conserva el valor histórico de las definiciones usadas por las fichas. |
| Pacientes e historia clínica | Fichas en todos los contextos, antecedentes, contactos y responsables, consultas, diagnósticos, tratamientos, seguimiento, mediciones, recetas con sus ítems, órdenes, vacunas, estudios, resultados y cálculos persistidos. Se conservan estados, autores, fechas, nulos, snapshots, correcciones y anulaciones. Dos fichas con el mismo DNI conservan su identidad y contexto propios. |
| Documentos y archivos | Los bytes exactos de los adjuntos conservados y de los PDFs emitidos, con sus códigos, metadatos, vínculos, anulaciones y huellas. Si un estudio se convirtió al cargarlo, se entrega la versión conservada por el sistema. Las referencias históricas a una copia se conservan como evidencia. |
| Consultorios y agenda | Consultorios, teleconsulta, horarios, excepciones, bloqueos, datos de publicación, preguntas de los formularios, turnos futuros y pasados, cancelaciones, cambios, respuestas de los pacientes y su vínculo con la ficha. Incluir las configuraciones de notificación y el historial visible que pertenezcan a la práctica. |
| Equipo y permisos | Secretarias, contactos, estado de la delegación, permisos y consultorios asignados al momento de la copia. |
| Auditoría | Historial autorizado de la práctica, incluidos accesos, revisiones y evidencias atribuibles al médico. Se conservan el actor y los identificadores originales como procedencia. La preparación y autorización de descarga de la copia quedan auditadas. |
| Experiencia de uso | Opcionalmente incluye tema y tamaño de letra del navegador, el contexto seleccionado si corresponde al médico y los borradores de agenda de esta pestaña para sus consultorios. Los borradores clínicos sin guardar y las conversaciones del asistente viven en memoria y quedan fuera de esta copia. |
| Cuenta compartida | Perfil profesional y de cuenta, avatar si está disponible, cobertura y datos comerciales de Doctor Chart. En una compra mixta se exportan los ítems de Doctor Chart. Los comprobantes de transferencia disponibles se entregan como datos estructurados. |

El contenido se limita a los datos autorizados de la práctica. Las contraseñas, hashes de credenciales, secretos del servicio, sesiones, enlaces con credenciales y códigos de un solo uso se excluyen del archivo.

## Contenido del ZIP

- Un único ZIP cifrado y versionado. Contiene `INDICE.html` para la consulta local, `MANIFIESTO.json` para el inventario, `datos/` para los registros estructurados, `documentos/` para los PDFs emitidos y `adjuntos/` para los archivos subidos que conserva Doctor Chart. Los PDFs y adjuntos se incorporan como archivos con sus bytes exactos. Usar identificadores y decimales exactos en las representaciones que lo requieran para conservar precisión, también al leer los datos desde JavaScript. [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259.html)
- El manifiesto identifica formato, versión, titular de origen, fecha de corte, inventario, referencias, conteos, tamaños y SHA-256 de entradas. Registra secciones vacías y el alcance de las fuentes locales o externas. Las versiones del formato se mantienen independientemente del esquema de PostgreSQL. Los identificadores de origen permiten vincular datos, PDFs y adjuntos dentro de la copia.
- Validar la apertura local del ZIP cifrado con las herramientas indicadas en las instrucciones. El índice es estático y enlaza a los documentos y adjuntos mediante rutas relativas dentro del ZIP. Los nombres de las entradas se generan con identificadores y el índice muestra nombres de archivos y vínculos en su contexto. Escapar los datos en HTML y usar enlaces locales sin scripts ni dependencias externas. Probar que se pueda abrir una copia anterior con el procedimiento documentado.
- Generar y validar en segundo plano, con lectura y transferencia por bloques, límites de volumen, espacio y tiempo, y temporales privados con vencimiento. Una copia completa exige todos los adjuntos inventariados. Si un archivo falta o no coincide su huella, la preparación falla y el médico ve el problema.
- Tomar una vista consistente de PostgreSQL para todos los contextos del titular, respetando el alcance de cada consulta aunque compartan transacción. Proteger la disponibilidad de las versiones de archivos que referencia esa vista durante la preparación. Si el perfil o datos históricos se obtienen de servicios externos, consignar su origen y corte y validar sus vínculos. [Aislamiento de transacciones de PostgreSQL](https://www.postgresql.org/docs/17/transaction-iso.html)

El código actual de exportación aporta lectura privada de adjuntos, comprobación de huellas y patrones de auditoría. Revisar otros límites para respaldos de prácticas completas. Hoy el ZIP de una ficha dispone de 45 segundos, admite hasta 128 MiB de contenido y tiene un máximo de 4.096 entradas. [Implementación existente](../internal/clinical/export.go)

## Flujo de descarga

1. El médico titular elige **Preparar copia** con su sesión iniciada. El servicio autentica la sesión y determina el alcance a partir de su cuenta y de los contextos autorizados.
2. La interfaz muestra el estado de preparación. Si falta un archivo, fallan las comprobaciones de integridad o una fuente necesaria está indisponible, informa el problema y permite reintentar.
3. Una vez verificada la copia, el titular ve la fecha de corte, el tamaño y la acción **Descargar copia**, junto con las instrucciones de apertura. El archivo temporal vence después del plazo de descarga.
4. El médico abre la copia en su equipo y navega por especialidad, paciente o área desde el índice local.

El ZIP AES se abre con una clave aleatoria que la pantalla muestra al iniciar la preparación. El ZIP queda disponible durante 20 minutos después de la preparación. La descarga se autoriza nuevamente con la sesión del titular. El archivo temporal permanece en la instancia que lo generó, por lo que la solicitud, los controles de estado y la descarga requieren llegar a esa instancia. Generalmente se puede extraer localmente con 7‑Zip y abrir `INDICE.html` desde la carpeta extraída. [Rutas de interfaz](../../doctorchart-frontend/src/app/app.routes.ts) · [Rutas de exportación](../internal/dataexport/service.go)

El límite actual es 512 MiB de ZIP, 1 GiB de contenido sin comprimir y 40.000 entradas. Si alguna fuente necesaria falla o se supera un límite, la preparación informa el motivo y el ZIP queda indisponible.

## Etapas y criterios para avanzar

| Etapa | Entrega verificable |
| --- | --- |
| 1. Cerrar el inventario y el contrato | Para cada entidad y campo, anotar origen, titularidad, dependencias, representación y comprobación. Cubrir almacenamiento externo, autenticación, Billing, preferencias de navegador y estado en memoria. Fijar el formato v1, el método de apertura local y el acceso con plan vencido. Crear una práctica sintética completa con otro médico como control de aislamiento. |
| 2. Probar el recorrido mínimo | En laboratorio, exportar un espacio con dos especialidades, un wizard con una versión histórica y un borrador, pacientes, una consulta y un adjunto. Comparar datos, referencias y bytes. Abrir el archivo desde otro equipo sin conexión al servicio. |
| 3. Completar la cobertura | Sumar por bloques la historia clínica, documentos, auditoría, agenda, publicaciones, delegaciones, preferencias y datos compartidos. Cada bloque incluye comparación con el origen y comprobación de lectura local. |
| 4. Integrar la descarga | Agregar **Copia de seguridad** a Perfil profesional. Probar el proceso web con una práctica grande, sesión autenticada, descargas interrumpidas, reintentos, expiración del temporal y otro dispositivo. |

La prueba final compara valores y referencias por contexto, hashes de archivos y PDFs, correcciones, auditoría, permisos y agenda. Incluye otra cuenta, dos fichas con el mismo DNI, archivo faltante, interrupción, reintento y actividad concurrente. Con el servicio desconectado, verifica que el médico pueda abrir el archivo, consultar fichas y acceder a sus documentos. Un control de cobertura debe fallar cuando el esquema agregue datos sin una regla explícita de exportación.

## Inventario inicial de PostgreSQL

El esquema leído contiene 57 tablas. El [inventario de campos](doctor-backup-inventory.md) detalla la proyección propuesta, las relaciones y las fuentes externas pendientes de confirmar. La tabla agrupa las fuentes de PostgreSQL por área. [Esquema de Doctor Chart](../db/schema.sql)

| Área | Tablas | Tratamiento propuesto |
| --- | --- | --- |
| Configuración (6) | `doctor_workspaces`, `doctor_specialty_contexts`, `specialty_context_preferences`, `doctor_chart_configs`, `doctor_calculator_preferences`, `doctor_calculator_selections` | Exportar el espacio del titular y la configuración de sus contextos. |
| Clínica (9) | `patients`, `clinical_encounters`, `patient_calculation_logs`, `patient_medical_studies`, `patient_prescription_records`, `patient_vaccinations`, `prescription_diagnostic_orders`, `prescription_medications`, `clinical_revisions` | Exportar cada contexto permitido, preservando el historial. |
| Documentos y auditoría (4) | `clinical_access_events`, `clinical_document_issues`, `clinical_document_annulments`, `clinical_export_evidence` | Conservar eventos atribuibles al titular y documentos exactos. Algunos eventos de acceso carecen de titular o contexto y requieren una regla de atribución explícita. |
| Agenda (11) | `bookings`, `booking_events`, `booking_public_details`, `booking_publications`, `clinic_publications`, `clinic_schedules`, `clinic_specialty_contexts`, `clinics`, `publication_specialty_contexts`, `schedule_blocks`, `schedule_exceptions` | Exportar configuración, agenda e historial autorizado. Excluir las credenciales efímeras incrustadas. |
| Equipo (4) | `staff_access`, `staff_access_clinics`, `staff_accounts`, `staff_security_events` | Exportar delegaciones, datos de contacto autorizados y eventos atribuibles. |
| Medios (2) | `media_assets`, `media_events` | Exportar los archivos de los estudios, sus metadatos clínicos y sus huellas. El catálogo valida su asociación. El diario técnico es interno. |
| Notificaciones (1) | `notification_outbox` | Exportar el historial de negocio autorizado del titular, con una selección explícita de campos. |
| Catálogos y plantillas (9) | `chart_definitions`, `clinical_calculation_definitions`, `specialties`, `specialty_calculator_defaults`, `vaccination_calendars`, `vaccination_doses`, `wizard_defaults`, `wizard_approvals`, `wizard_approval_revocations` | Capturar las definiciones y versiones necesarias para interpretar los datos propios. Resolver las referencias con códigos y versiones. |
| Infraestructura y seguridad efímera (11) | `app_schema_state`, `booking_confirmation_access`, `booking_idempotency`, `clinic_entitlement_epoch`, `booking_portal_codes`, `public_booking_rate_windows`, `staff_email_recovery`, `staff_invitations`, `staff_rate_windows`, `staff_sessions`, `study_file_deletions` | Identificar evidencia de negocio por campo si la hubiera. Excluir datos operativos internos y credenciales. |

También hay fuentes fuera de PostgreSQL, entre ellas los adjuntos gestionados y legados, los metadatos de cuenta de Supabase, Billing, las preferencias del navegador y los borradores. La prueba de integridad incluye estas fuentes y las dependencias embebidas en JSON, además de las claves foráneas declaradas.

### Referencias en el código

- [Almacenamiento de estudios y medios](../internal/storage/store.go) · [Alcance de transacciones clínicas](../db/clinical_scope.go) · [Autorización de planes](../internal/billing/entitlements.go)
- [Ruta de Perfil profesional](../../doctorchart-frontend/src/app/app.routes.ts) · [Metadatos de perfil](../../doctorchart-frontend/src/app/features/auth/user-profile-page.component.ts) · [Preferencias del navegador](../../doctorchart-frontend/src/app/core/services/browser-ui-storage.service.ts)
- [Borradores clínicos](../../doctorchart-frontend/src/app/features/patient-record/patient-record-drafts.service.ts) · [Estado del asistente](../../doctorchart-frontend/src/app/core/services/ai-chat.service.ts)
