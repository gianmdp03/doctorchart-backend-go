# Auditoría de acceso clínico SP-04D

## Qué se registra

Go registra un evento por respuesta en las rutas clínicas auditadas. Cubre búsqueda y apertura de fichas, resumen y antecedentes, consultas y tablero, crecimiento, mediciones y vacunas, recetas y órdenes, generación de PDF, preparación de datos para imprimir, estudios y descargas vigentes o archivadas, fichas anuladas y revisiones. Incluye historial, autocompletado y ejecución persistida de cálculos, y lecturas de la propia bitácora. Un listado registra la operación dentro del contexto sin enumerar todos los registros de la página.

El evento contiene hora PostgreSQL, identidad autenticada, tipo de actor, operación, resultado, correlación y referencias numéricas disponibles. Cuando Go confirma el ámbito se registran el propietario y el contexto. Los intentos sin contexto propio comprobado quedan en la bitácora operativa sin ámbito asignado y no aparecen en cuentas médicas. Un registro rechazado puede contener el identificador numérico solicitado, que sólo identifica el intento. Los registros persistidos antes de SP-04D carecen de auditoría retrospectiva.

Se guarda el evento antes de habilitar cabeceras y cuerpo de una respuesta exitosa. Si falla esa escritura, Go devuelve 503 sin contenido clínico y registra en el log operativo la operación y la correlación. Si ya se había rechazado la solicitud, conserva su error y deja la misma señal operativa. `AUTHORIZED` indica que se habilitó responder, `ARTIFACT_READY` que se generó el PDF, `REJECTED` que el handler devolvió 4xx y `FAILED` que devolvió 5xx. Estos estados no confirman entrega, impresión física ni recepción del archivo. Al ejecutar un cálculo, el resultado persistido puede existir aunque el acceso a la respuesta falle después.

La bitácora excluye texto clínico, consultas de búsqueda, nombres de archivos, nombres de pacientes, valores, credenciales y cuerpos HTTP. El Gateway reemplaza el identificador de solicitud proporcionado por el cliente para DoctorChart con un identificador aleatorio propio. Los logs de su proxy usan etiquetas fijas de rutas y tipos de error. El API también genera una correlación cuando recibe una llamada autenticada sin identificador válido.

El rol ordinario puede insertar sólo las columnas de evento, con ID y fecha asignados por PostgreSQL. La lectura está sujeta a RLS por propietario y contexto y a un filtro explícito en Go. La tabla carece de vínculos que borren eventos al anular fichas. El rol de medios carece de acceso. El propietario del esquema y operadores con privilegios de infraestructura pueden administrar la tabla. El API carece de una operación de borrado de auditoría.

Angular permite consultar los eventos del contexto desde el directorio y por paciente desde una ficha. Las lecturas pendientes se cancelan cuando cambia la cuenta o el contexto. Los eventos conservan sus permisos aunque se revoque una aprobación de plantilla o se anule un registro.

Quedan fuera de esta bitácora los intentos que el Gateway rechaza antes de autenticar al actor, el contenido que devuelve una mutación ordinaria, los catálogos estáticos, la previsualización de cálculos con entradas proporcionadas por el solicitante y las operaciones de agenda sin acceso clínico. SP-04B conserva su historial separado de correcciones y anulaciones.

## Datos visibles por actor

| Actor | Datos y controles comprobables en código y laboratorio |
| --- | --- |
| Médico propietario | Fichas, búsquedas, consultas, órdenes, PDF, adjuntos e historial de su contexto. Puede ver sus eventos de acceso en ese ámbito. Puede consultar datos administrativos y respuestas de reserva en el detalle privado del turno propio. |
| Otro médico | Accede sólo a sus recursos. Coincidir en DNI, especialidad o consultorio no amplía su ámbito. Se aplica el mismo control a documentos, adjuntos e historial. |
| Secretaría autorizada | Opera turnos de consultorios delegados. Recibe nombres administrativos del solicitante y de la persona atendida, teléfono, especialidad, consultorio, modalidad, fecha, hora y estado. La proyección excluye ID de ficha, enlaces clínicos, respuestas libres, notas, archivos y enlaces privados de reunión. |
| Secretaría sin alcance | La sesión y el permiso por consultorio se verifican en Go. Carece de autorización para las operaciones fuera de la delegación y para endpoints clínicos o de auditoría. |
| Admin del catálogo | Administra plantillas y decisiones de SP-04C. El rol no concede pertenencia a un contexto clínico. |
| Paciente o solicitante | Recibe datos administrativos de su reserva y una referencia. La respuesta inmediata a quien cargó la reserva incluye los nombres que ingresó. La constancia posterior exige un código válido y devuelve una proyección propia. |
| Público | Ve ofertas que el médico publicó y siguen habilitadas, su especialidad y la disponibilidad pública. Carece de acceso a fichas, históricos y respuestas de otros solicitantes. |
| Servicios internos y Gateway | El Gateway verifica identidad y acceso vigente y reemplaza encabezados internos. Billing autentica sus callbacks por separado. El conversor trata el archivo para su función y el rol de medios tiene permisos operativos acotados. La herramienta de IA preexistente arma consultas sin el contexto clínico explícito y el API actual las rechaza en ese punto. |

Los correos administrativos se arman con datos del profesional, especialidad, consultorio, modalidad o dirección cuando corresponde, fecha, referencia y acceso cuando aplica. Las respuestas libres se conservan para el dueño del turno y quedan fuera de las proyecciones de secretaría, las constancias y el mensaje construido para SMTP. Los errores del worker se clasifican con códigos operativos. Los adaptadores de autenticación, facturación y correo usados en laboratorio no son evidencia de una integración productiva.

## Transición y operación

SP-04E actualiza el runtime a Go 1.27.4 y el esquema a 20. El procedimiento vigente se describe en [documentos, exportaciones y recuperación](documents-export-recovery-sp04e.md).

El baseline 19 consolida el esquema. `baseline-copy --confirm-empty-target` valida el origen 18 con su checksum y catálogo fijados y crea un destino vacío del mismo PostgreSQL mayor, codificación y colación. Conserva revisiones, aprobaciones y revocaciones, y deja vacía la bitácora de accesos. Para la prueba, `DOCTORCHART_BASELINE18_FIXTURE_SQL` identifica el artefacto original 18 y `BASELINE_TEST_ADMIN_DSN` una instancia descartable. El runtime rechaza un baseline ajeno y exige los roles restringidos. La guía VPS distingue la observación histórica del contrato actual de roles.

La retención inicial es continua. Antes de fijar plazos o habilitar purgas, corresponde acordar conservación, acceso institucional, monitoreo de fallos, procedimientos de incidentes y exigencias jurídicas. La revisión del VPS, la configuración efectiva de TLS y de logs de Nginx/Cloudflare, los permisos reales de almacenamiento y la recuperación de backups siguen pendientes.

`govulncheck` señaló un aviso de disponibilidad de `net/http` en la herramienta Go 1.27.3 del proyecto. El aviso describe una condición de concurrencia y se corrigió en Go 1.27.4. Queda pendiente actualizar el runtime y repetir el escaneo antes de producción. [Aviso oficial GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859)

## Decisiones de privacidad pendientes

- **Correo y constancia.** Mantener por ahora los metadatos administrativos existentes. Definir si la especialidad debe omitirse por canal o si el profesional puede personalizar su visibilidad, especialmente para Psiquiatría y Obstetricia.
- **Consultorios compartidos.** La delegación actual es por consultorio. Evaluar si el equipo necesita recibir un subconjunto por especialidad, profesional u operación antes de cambiar el modelo.
- **Solicitante y paciente distintos.** Mantener separados ambos roles. Decidir quién puede recibir actualizaciones y si el mensaje debe usar únicamente una referencia neutra cuando el contacto pertenece a un tercero.
- **Texto libre.** Los formularios públicos permiten preguntas administrativas. El producto debe definir si acepta únicamente opciones estructuradas y cómo debe responder cuando un solicitante escribe información clínica.
- **Canales externos.** Revisar con los responsables los logs y la conservación en autenticación, proxy, correo, conversión, almacenamiento e IA. Para una auditoría institucional independiente, definir un rol explícito, su ámbito y las responsabilidades de acceso antes de crearlo.

## Evidencia de laboratorio

La revisión del 17/11/2026 se hizo sobre los repositorios locales con cambios previos pendientes. Sus HEAD de partida eran `1763153` en Go, `90d10ec` en Angular y `ec878ce` en Gateway. Los datos y las cuentas de prueba fueron sintéticos.

| Comprobación | Resultado |
| --- | --- |
| DoctorChart con Go 1.27.3 | `go build`, `go vet` y suite PostgreSQL 17 aprobados. Suite con `-race` aprobada en PostgreSQL 18. |
| Transición 18 → 19 | Probada en PostgreSQL 17 y 18 con revisiones y decisiones de plantilla. Conservó las filas y dejó vacía la nueva bitácora. |
| Gateway | Suite Go aprobada. Pruebas de correlación y redacción de identificadores, valores y errores aprobadas. |
| Angular | 869 tests aprobados. Compilaciones de desarrollo y laboratorio aprobadas. |
| Playwright | 183 grupos y 1.897 llamadas API verificadas. Incluye M6, 19 especialidades, SP-04B, SP-04B.1, SP-04C y SP-04D. |
| Dependencias y entorno desplegado | Aviso de Go abierto. Semgrep, Gitleaks, Trivy y ZAP no ejecutados. Roles, TLS, logs y proveedores del VPS sin comprobación en vivo. |

La implementación queda disponible para revisión. La habilitación productiva requiere resolver el aviso del runtime y verificar la infraestructura y las decisiones de privacidad pendientes.
