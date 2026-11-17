# Documentos, exportaciones y recuperación SP-04E

## Identidad y emisión

La generación anterior aceptaba nombre del profesional, especialidad y datos del consultorio desde los parámetros de la URL. Go ahora obtiene la cuenta del actor autenticado, el registro y su versión, el paciente y la especialidad desde los recursos autorizados del servidor. Los valores de atribución suministrados por el cliente quedan fuera del PDF. El cuerpo de una solicitud de emisión acepta únicamente el tipo documental.

DoctorChart todavía carece de un padrón verificado de identidad profesional, matrícula y sede de emisión. El documento técnico identifica al responsable por su cuenta autenticada y conserva la especialidad de la emisión. Requiere una decisión operativa sobre acreditación profesional antes de usar el sistema para emitir documentación legalmente autenticada. Los documentos llevan una indicación expresa sobre la ausencia de firma digital. La interfaz ofrece estas capacidades como copia y emisión técnica. El flujo institucional autenticado permanece pendiente.

Se incorporaron tres tablas al baseline 20. `clinical_document_issues` conserva código aleatorio, cuenta responsable, paciente, contexto, origen, versión, tipo, especialidad y fecha de emisión, junto con los bytes exactos del PDF y su SHA-256. PostgreSQL comprueba la coincidencia del hash. Guardar PDFs de hasta 4 MiB en la misma base hace atómica la conservación del archivo y su evidencia, y permite reutilizar el respaldo PostgreSQL sin incorporar otro servicio. Los estudios mantienen su almacenamiento protegido existente.

Cada nueva emisión recibe otro identificador. Su PDF usa la fecha de emisión y los datos clínicos vigentes que se obtuvieron bajo bloqueo de la ficha. El registro de acceso se confirma en la misma transacción. Las reimpresiones descargan el PDF guardado y verifican su hash. La anulación se agrega una sola vez a `clinical_document_annulments`, con actor, fecha y motivo. El PDF original se conserva y el panel muestra la anulación. Cuando se corrige o anula el registro de origen, el listado muestra también ese estado. Se puede consultar la emisión histórica dentro del contexto autorizado aunque el origen haya sido anulado.

Las rutas anteriores de PDF permanecen como vistas previas técnicas, con identidad del servidor. La acción de emitir desde Angular usa el nuevo registro persistente antes de abrir el PDF original.

## Ficha contextual exportable

El botón de exportación prepara una ficha identificada por su ID interno y el contexto autorizado. Incluye identificación, antecedentes y valores guardados, consultas y sus snapshots, diagnósticos, tratamientos, seguimiento, recetas y órdenes con sus elementos, estudios, vacunas, mediciones en consultas, cálculos persistidos, revisiones, anulaciones y emisiones originales. El paquete incluye los archivos de estudios conservados por DoctorChart, que pueden haber pasado por conversión al cargarse. El sistema sólo puede entregar esos bytes conservados.

El ZIP contiene `INDICE.txt`, `LECTURA.html`, datos NDJSON por sección, cronología, PDFs originales, adjuntos y `MANIFIESTO.json`. La vista HTML escapa los datos y carece de scripts y dependencias externas. Los nombres de entrada se generan en el servidor. El manifiesto identifica el alcance, la preparación y el tamaño y hash de cada archivo de contenido. Su propia integridad se comprueba mediante la huella del ZIP, entregada en `Content-Digest` y conservada en la evidencia.

La preparación toma una instantánea consistente de la base para la ficha y todos sus registros. Los adjuntos se leen después desde claves privadas obtenidas de esa instantánea y se comprueban contra tamaño y, en almacenamiento administrado, el checksum ya guardado. En almacenamiento anterior sin checksum, la primera huella verificable del archivo se obtiene al exportar. Se conservan literalmente los valores y estados históricos.

Un archivo faltante, un tamaño incorrecto o un checksum discrepante rechaza toda la entrega. Los límites técnicos son 128 MiB de contenido, 4.096 entradas, dos preparaciones concurrentes por instancia y 45 segundos. El temporal usa permisos privados y se elimina al finalizar. `clinical_export_evidence` conserva la huella y el manifiesto tras la preparación. El ZIP se sirve en la solicitud y puede prepararse nuevamente a partir del estado posterior de la ficha. Carece de cifrado adicional del archivo. La protección del canal y los permisos son los del acceso clínico autenticado y TLS. Quien lo descargue debe custodiarlo adecuadamente.

Se registran por separado preparación del archivo y autorización de descarga. El sistema carece de evidencia de recepción efectiva por el destinatario o impresión física. Los fallos de auditoría bloquean la entrega de datos clínicos. No se registra contenido de los documentos en los logs operativos.

El alcance implementado es una ficha contextual por solicitud. Para preparar varias fichas autorizadas, el médico debe solicitar cada una de forma independiente. La entrega institucional completa exige resolver custodia, identificación del paciente entre fichas independientes, legitimación del solicitante y autoridad que autentica la copia. La búsqueda por DNI carece de efectos sobre permisos y agrupación.

## Conservación y permisos

Las tres tablas nuevas usan RLS por cuenta y contexto. El usuario de ejecución puede leer e insertar dentro de ese ámbito, con actualización y borrado excluidos de sus privilegios. El rol de medios carece de acceso. Los operadores con control de infraestructura conservan sus facultades administrativas. Los documentos y evidencias carecen de una operación de purga ordinaria.

La conservación técnica es continua hasta aprobar una política jurídica y operativa. Corresponde definir plazos, respaldos cifrados, custodia de claves, responsables, procedimiento de entrega, control de copias descargadas y retiro de versiones anuladas de circuitos administrativos. Una anulación conserva la evidencia y requiere revisar cómo comunicarla a quienes recibieron la emisión.

## Respaldo y recuperación

El ensayo se ejecuta con `node ops/testing/specialty-m6-lab.mjs restore-sp04e` exclusivamente en el laboratorio M6 identificado por etiqueta y topología. Requiere datos sintéticos representativos y la evidencia generada por el recorrido SP-04E.

1. Detener Gateway y API del laboratorio, incluidos sus jobs, y comprobar el baseline e inventario de origen.
2. Crear un `pg_dump -Fc` de la base y copiar el árbol de almacenamiento mientras los escritores están detenidos. Conservar el dump, los archivos, el baseline SQL exacto y un manifiesto con hashes como una unidad. El manifiesto incluye cantidades y huellas de las 55 tablas.
3. Custodiar separadamente las claves y configuración necesarias para Auth, Billing, notificaciones, Gateway, conversión y almacenamiento. En el ensayo se reutilizan exclusivamente claves sintéticas de ese laboratorio.
4. Comprobar el manifiesto antes de restaurar. Verificar existencia, versión esperada, integridad del dump y todos los archivos.
5. Crear otra instancia PostgreSQL descartable sin puertos publicados, recrear los roles de ensayo y restaurar el dump en una base nueva mediante una transacción. El dump conserva propietarios, grants, RLS, triggers y secuencias. Antes del arranque, recrear exclusivamente los siete checks y el índice identificados por el ensayo, a partir de sus definiciones del baseline verificado; PostgreSQL vuelve a validar los checks. Comparar después el catálogo completo contra la huella original conservada, sin modificar el marcador ni aceptar una huella nueva.
6. Comparar cantidades y huellas de datos, verificar integridad de PDFs, referencias y políticas, copiar a otra carpeta de almacenamiento y comprobar todos sus hashes.
7. Arrancar el API contra esa base y esa carpeta. El arranque vuelve a validar identidad, checksum, catálogo, privilegios y claves de notificaciones. Comprobar mediante el Gateway autenticado fichas, documentos originales, adjuntos históricos, auditoría, exportación, agenda, reservas y creación de datos nuevos.

La rutina detecta un manifiesto incompatible, un dump con hash incorrecto y un respaldo incompleto. El resultado del ensayo debe informar duración y pérdida observada. La medición corresponde al laboratorio. Los objetivos operativos de producción requieren otra evaluación.

El primer arranque tras un `pg_restore` sin este paso rechazó el catálogo: el deparser de PostgreSQL reescribió casts de arrays y el agrupamiento de un check. La diferencia afectaba siete checks y el índice parcial de notificaciones, aunque los datos coincidían. La corrección queda limitada al procedimiento explícito sobre el destino descartable de restauración. El arranque normal sigue rechazando cualquier drift y carece de reparación automática.

El baseline 20 es consolidado. `baseline-copy --confirm-empty-target` verifica un origen 19 y construye un destino vacío compatible. Los argumentos para orígenes 16, 17 y 18 siguen disponibles. La copia conserva el historial anterior y crea las tablas documentales vacías. La restauración de un respaldo del baseline 20 conserva también las emisiones y exportaciones nuevas.

## Fuentes y decisiones jurídicas

La Ley 26.529 regula integridad, titularidad, custodia y copia autenticada de la historia clínica. La reglamentación contempla la intervención de la autoridad del establecimiento para autenticar la entrega y la acreditación del solicitante. La aplicación concreta depende del prestador y de su jurisdicción. [Ley 26.529 actualizada](https://www.argentina.gob.ar/normativa/nacional/160432/actualizacion), [Decreto 1089/2012](https://www.argentina.gob.ar/normativa/nacional/decreto-1089-2012-199296/texto)

Los artículos 14, 17 y 18 fijan entrega de copia autenticada dentro de 48 horas salvo emergencia, unicidad dentro del establecimiento y custodia mínima de diez años desde la última actuación. La política institucional deberá contemplar esas obligaciones y las reglas jurisdiccionales; el sistema no programa una purga al vencer un plazo. La Ley 25.326 considera sensibles los datos de salud y exige medidas de seguridad y confidencialidad. [Ley 26.529, capítulos IV y V](https://www.argentina.gob.ar/normativa/nacional/ley-26529-160432/actualizacion), [Ley 25.326, artículos 2, 8, 9 y 10](https://www.argentina.gob.ar/normativa/nacional/ley-25326-64790/actualizacion)

La Ley 27.706 y su reglamentación establecen el marco del sistema de historias clínicas electrónicas. Corresponde evaluar los estándares exigibles y la firma que debe refrendar las actuaciones según la modalidad y el establecimiento. [Ley 27.706](https://www.argentina.gob.ar/normativa/nacional/ley-27706-380710/texto), [Decreto 393/2023](https://www.argentina.gob.ar/normativa/nacional/decreto-393-2023-387475/texto)

La firma digital y su eficacia requieren el mecanismo y las condiciones de la Ley 25.506. Las huellas técnicas permiten comprobar igualdad de bytes. El proyecto aún requiere decisiones sobre acreditación profesional, certificación, firma y autenticación institucional. [Ley 25.506](https://www.argentina.gob.ar/normativa/nacional/ley-25506-70749/texto)

## Seguridad del runtime

El proyecto y sus scripts de ensayo usan Go 1.27.4. El aviso GO-2026-6859 del runtime anterior indica esa versión como corregida. El resultado vigente de `govulncheck` debe leerse junto con su distinción entre funciones alcanzadas y dependencias requeridas sin llamadas alcanzables. [Aviso oficial](https://pkg.go.dev/vuln/GO-2026-6859)

El análisis de DoctorChart informó cero vulnerabilidades alcanzables y el aviso GO-2026-5932 en el módulo requerido `golang.org/x/crypto`, cuyo paquete OpenPGP no se importa. El Gateway también informó cero vulnerabilidades alcanzables. En sus módulos requeridos señaló GO-2026-5932, GO-2026-6860 y GO-2026-6866, estos últimos relacionados con HTML en `golang.org/x/net`. La actualización del runtime corrigió el aviso de Go. Los avisos de módulos siguen documentados para revisión de dependencias.

## Validación ejecutada el 17/11/2026

| Comprobación | Resultado nuevo |
| --- | --- |
| Go 1.27.4, DoctorChart | Build, vet y suite completa con PostgreSQL 17: PASS; suite completa con `-race` y PostgreSQL 18: PASS. |
| Inicialización y copia | Bases vacías, catálogo, reinicio, inicialización concurrente, rollback, RLS, seeds y copias preservadoras 16/17/18/19 hacia 20: PASS. El origen 19 conserva también auditoría SP-04D. |
| Go 1.27.4, Gateway | Build, vet y tests: PASS. |
| govulncheck | Cero vulnerabilidades alcanzables o en paquetes importados en ambos módulos; avisos de módulos requeridos detallados arriba. |
| Angular | 869 tests en 107 archivos: PASS; builds de desarrollo y del laboratorio M6: PASS. |
| Playwright general | 185 grupos y 1.909 llamadas API verificadas: PASS. Incluye las 19 especialidades, M6, SP-04B/B.1/C/D/E, agenda, reservas, recetas y estudios. |
| Playwright focal SP-04E | Dos grupos y 14 llamadas API: PASS. |
| API después de la restauración | Dos grupos y 14 llamadas API: PASS. PDF y adjunto histórico idénticos, exportación, auditoría, revisiones y agenda legibles; nueva ficha y reserva creadas; acceso de otro médico rechazado. |
| Recuperación de tablas y archivos | Cantidad y hash de cada una de las 55 tablas y de los 882 archivos: coincidencia exacta. Catálogo original, integridad de PDFs, roles y RLS: PASS. |
| PDF | Caso de UID y código largos renderizado e inspeccionado visualmente; una página legible: PASS. |

Antes del respaldo había 39 fichas, 43 consultas, 36 contextos de dos médicos, 38 reservas, 21 estudios, 21 activos de medios, seis revisiones, 1.752 eventos de acceso, 36 aprobaciones, una revocación, dos emisiones, una anulación y una evidencia de exportación. Las cantidades y huellas coincidieron antes de las escrituras funcionales posteriores a la restauración. Todas las claves foráneas quedaron validadas.

El dump ocupó 476.161 bytes. La ejecución final midió 25,325 segundos para el procedimiento y 10,609 segundos desde el comienzo de la restauración hasta las comprobaciones y solicitud de arranque del API. Estas duraciones excluyen el recorrido funcional posterior y no constituyen un RTO productivo. La pérdida observada respecto del punto lógico con escritores detenidos fue cero registros y cero archivos; tampoco constituye un compromiso de RPO productivo.

La evidencia de navegador se conserva en `%TEMP%/doctorchart-specialty-m6-20261110/browser/sp04e-results.json`, `sp04e-only-results.json`, `sp04e-restored-results.json` y `sp04e-recovery-inventory.json`. Los respaldos sintéticos y sus manifiestos permanecen en carpetas `recovery-sp04e-*` de ese laboratorio. Auth y Billing son adaptadores de ensayo; API, Gateway, Angular, PostgreSQL, conversor y SMTP de laboratorio son reales. El resultado no valida los proveedores productivos ni una entrega acreditada al paciente.

## Condiciones pendientes antes de uso real

La revisión médica debe aprobar el contenido documental y cómo comunicar correcciones y anulaciones. La revisión jurídica e institucional debe definir acreditación profesional, autoridad de firma y autenticación, legitimación del paciente o representante, custodia, retención, alcance de la historia institucional y constancia efectiva de entrega. La operación debe aprobar cifrado y custodia externa de respaldos y claves, controles sobre descargas, capacidad, monitorización y objetivos de recuperación. La entrega institucional autenticada y el circuito de firma legal permanecen sin habilitar. El hash acredita igualdad de bytes y la cuenta identifica al actor técnico; ninguno acredita por sí solo identidad profesional verificada, firma digital o recepción del destinatario.

No se aplicaron cambios a la instancia local principal ni al VPS. Se preservaron los cambios locales previos y no se hicieron commits, pushes ni despliegues. SP-04F permanece fuera del alcance.
