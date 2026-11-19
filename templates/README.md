# Plantillas clínicas provisionales

Informes de validación: [SP-03B](../docs/specialties-sp03b.md),
[SP-03C](../docs/specialties-sp03c.md), [SP-03D](../docs/specialties-sp03d.md) y
[SP-03F](../docs/specialties-sp03f.md).

Las dieciséis propuestas ambulatorias tienen estado `PROVISIONAL_TEST_ONLY`, fuera
de `createSpecialty.definition` para respetar el contrato estricto v1. Su uso está
limitado a ensayos con datos ficticios hasta que un profesional médico apruebe
campos, etiquetas, unidades, opciones y alcance clínico. El seed las incorpora
al catálogo como borradores inactivos. Las publicaciones administradas
conservan su estado y las plantillas nuevas requieren su aprobación normal.

| Archivo / código estable | Ficha específica | Consulta específica |
| --- | --- | --- |
| `cardiology.provisional.json` / `cardiology` (SP-02B) | Antecedentes cardiovasculares, procedimientos, familia y factores de riesgo | Síntomas, presión arterial, frecuencia cardíaca, peso/talla, examen, estudios y evaluación |
| `endocrinology.provisional.json` / `endocrinology` (SP-03B) | Antecedentes endocrinológicos, familia, procedimientos/cirugías y observaciones | Evolución, cambios desde el control anterior, peso/talla, examen, laboratorio/imágenes y evaluación |
| `gastroenterology.provisional.json` / `gastroenterology` (SP-03B) | Antecedentes digestivos, procedimientos/cirugías, familia y hábitos | Evolución, localización/características, tránsito intestinal, relación con alimentación, examen abdominal y estudios |
| `pulmonology.provisional.json` / `pulmonology` (SP-03B) | Antecedentes respiratorios, tabaquismo/exposiciones, familia y procedimientos/tratamientos previos | Síntomas/evolución, saturación opcional (%), soporte de oxígeno informado, examen y estudios funcionales/imágenes |
| `urology.provisional.json` / `urology` (SP-03C) | Antecedentes urológicos, procedimientos/cirugías, familia y observaciones | Motivo/evolución, síntomas urinarios/otros pertinentes, dolor/localización, examen genitourinario narrativo y estudios |
| `otolaryngology.provisional.json` / `otolaryngology` (SP-03C) | Antecedentes otológicos/nasales/faríngeos, procedimientos, exposiciones y familia | Motivo/evolución, región principal, lateralidad opcional, examen de oído/nariz/garganta y estudios |
| `gynecology.provisional.json` / `gynecology` (SP-03C) | Antecedentes ginecológicos/reproductivos pertinentes, procedimientos, anticoncepción y datos menstruales opcionales | Motivo/evolución, síntomas, antecedentes relevantes para la consulta, examen narrativo y estudios; sin seguimiento obstétrico |
| `allergy_immunology.provisional.json` / `allergy_immunology` (SP-03C) | Antecedentes atópicos/familiares, exposiciones, episodios previos e inmunológicos | Motivo/episodio, posible desencadenante, temporalidad, manifestaciones, certeza de la hipótesis, examen y pruebas revisadas |
| `neurology.provisional.json` / `neurology` (SP-03D) | Antecedentes personales/familiares, eventos previos, tratamientos/procedimientos y observaciones | Inicio opcional, cronología, manifestaciones, repercusión funcional, examen neurológico narrativo y estudios |
| `rheumatology.provisional.json` / `rheumatology` (SP-03D) | Antecedentes reumatológicos/autoinmunes, familia y procedimientos/tratamientos previos | Distribución, dolor, rigidez narrativa/duración opcional en minutos, manifestaciones extraarticulares, función y examen musculoesquelético |
| `traumatology_orthopedics.provisional.json` / `traumatology_orthopedics` (SP-03D) | Lesiones previas, procedimientos/cirugías, limitaciones previas y antecedentes musculoesqueléticos | Múltiples regiones narrativas, lateralidad, fecha opcional, mecanismo, evolución, dolor/limitaciones y examen regional |
| `geriatrics.provisional.json` / `geriatrics` (SP-03D) | Antecedentes, convivencia, apoyo, función habitual e informante/contacto/fuente | Cambios funcionales, movilidad/caídas, cognición y ánimo descriptivos, nutrición, apoyo y fuente de la consulta |
| `internal_medicine.provisional.json` / `internal_medicine` (SP-03F) | Contenido derivado de Medicina General: cirugías, internaciones, familia, hábitos, prevención y observaciones | Motivo/evolución, antecedentes pertinentes, signos vitales opcionales, examen, evaluación, diagnóstico, estudios/derivaciones y plan |
| `ophthalmology.provisional.json` / `ophthalmology` (SP-03F) | Antecedentes oculares, procedimientos, corrección habitual y observaciones | Agudeza visual literal por ojo, método/notación/distancia/corrección, presión intraocular y método por ojo, condiciones, examen y plan |
| `psychiatry.provisional.json` / `psychiatry` (SP-03F) | Antecedentes psiquiátricos, tratamientos previos y contexto psicosocial | Examen mental propio, funcionamiento, respuesta al tratamiento y evaluación de seguridad narrativa; evaluación, diagnóstico y plan |
| `obstetrics.provisional.json` / `obstetrics` (SP-03F) | Antecedentes obstétricos, embarazos previos y procedimientos | Referencia y datos gestacionales de cada consulta, fechas conocidas, edad gestacional literal y fuente, hallazgos y plan; alternativa A narrativa |

Se conserva la convención de códigos técnicos en inglés, minúsculas y guion bajo
cuando corresponde (`general_medicine`); `pulmonology` es el código estable de
Neumonología. Los nombres visibles son independientes del código.
Para Alergia e Inmunología se elige `allergy_immunology`, por la misma convención
de palabras en inglés separadas con guion bajo; permanece independiente de
Dermatología y del bloque de antecedentes comunes.
`traumatology_orthopedics` conserva la misma convención: nombre técnico compuesto
en inglés, minúsculas y guion bajo, independiente de su nombre visible en español.

Las nuevas fichas reutilizan sin cambios la identidad protegida de Cardiología
y el bloque `sys_clinical_history` en ficha y consulta. La medicación habitual,
alergias y problemas actuales se registran en ese bloque, sin listas paralelas.
Los textos específicos contextualizan los antecedentes de cada especialidad.

| Formulario / sección SP-03B | Claves | Tipo y características |
| --- | --- | --- |
| Ficha Endocrinología | `endocrinePersonalHistory`, `endocrineFamilyHistory`, `endocrineProcedures`, `endocrineNotes` | TEXTAREA opcional |
| Ficha Gastroenterología | `digestivePersonalHistory`, `digestiveProcedures`, `digestiveFamilyHistory`, `digestiveHabits` | TEXTAREA opcional |
| Ficha Neumonología | `respiratoryPersonalHistory`, `respiratorySmokingAndExposures`, `respiratoryFamilyHistory`, `respiratoryPriorProceduresAndTreatments` | TEXTAREA opcional; tratamientos previos, sin duplicar medicación habitual |
| Consulta / motivo y evolución | `chiefComplaint`, `presentIllness` | TEXTAREA opcional; proyecciones existentes |
| Consulta Endocrinología / cambios | `intervalChanges` | TEXTAREA opcional |
| Consulta Endocrinología / mediciones | `weightKg`, `heightCm` | NUMBER opcional; kg y cm; proyecciones existentes |
| Consulta Gastroenterología / síntomas | `digestiveSymptomLocation`, `bowelChanges`, `foodRelation` | TEXTAREA opcional; relación alimentaria solamente cuando corresponda |
| Consulta Neumonología / mediciones | `oxygenSaturation`, `reportedOxygenSupport` | NUMBER opcional con unidad visible %, y TEXTAREA opcional; sin valor preseleccionado ni interpretación |
| Consulta / examen | `physicalExam` | TEXTAREA opcional; proyección existente |
| Consulta / estudios | `reviewedStudies` | TEXTAREA opcional; informes/archivos y órdenes mediante Estudios y recetas existentes |
| Consulta / evaluación | `clinicalAssessment`, `diagnosis` | TEXTAREA opcional; solo `diagnosis` tiene proyección común |
| Consulta / plan | `treatment`, `followUpInstructions`, `followUpDate` | TEXTAREA, TEXTAREA y DATE opcionales; proyecciones existentes |

Las cuatro plantillas SP-03C reutilizan las mismas secciones de motivo/evolución,
antecedentes comunes, examen, estudios, evaluación, diagnóstico y plan, con las
claves compartidas de la tabla anterior. Sus campos específicos son:

| Formulario / especialidad | Claves | Tipo y características |
| --- | --- | --- |
| Ficha Urología | `urologicPersonalHistory`, `urologicProcedures`, `urologicFamilyHistory`, `urologicNotes` | TEXTAREA opcional; no se agregan antecedentes sexuales o reproductivos a este piloto |
| Consulta Urología | `urinarySymptoms`, `urologicPainAndLocation` | TEXTAREA opcional |
| Ficha Otorrinolaringología | `entPersonalHistory`, `entProcedures`, `entExposures`, `entFamilyHistory` | TEXTAREA opcional |
| Consulta Otorrinolaringología | `evaluatedRegion`, `laterality` | TEXT y SELECT opcionales; lateralidad RIGHT, LEFT, BILATERAL, UNKNOWN o NOT_APPLICABLE, sin selección inicial |
| Ficha Ginecología | `gynecologicPersonalHistory`, `reproductiveHistory`, `gynecologicProcedures`, `contraceptionHistory`, `menstrualHistory` | TEXTAREA opcional; registrar solo información pertinente |
| Ficha Ginecología | `menstrualDataStatus` | SELECT opcional: KNOWN, UNKNOWN, NOT_REVIEWED, NOT_APPLICABLE; vacío permanece sin respuesta, distinto de cada estado explícito |
| Consulta Ginecología | `gynecologicSymptoms`, `gynecologicVisitHistory` | TEXTAREA opcional; examen narrativo también opcional; sin inferencias desde la identidad o el sexo |
| Ficha Alergia e Inmunología | `atopicPersonalHistory`, `atopicFamilyHistory`, `allergyRelevantExposures`, `priorEvaluatedEpisodes`, `immunologicHistory` | TEXTAREA opcional; no es una segunda lista de alergias conocidas |
| Consulta Alergia e Inmunología | `suspectedExposure`, `exposureSymptomTiming`, `clinicalManifestations` | TEXTAREA opcional; hipótesis y exposición contextualizadas en esta consulta |
| Consulta Alergia e Inmunología | `allergicHypothesisCertainty` | SELECT opcional: SUSPECTED, CONFIRMED, UNDETERMINED; describe la evaluación registrada por el profesional, sin clasificar automáticamente pruebas ni antecedentes |

Una exposición sospechada y su grado de certeza no se convierten en alergias
conocidas. Incluso marcar CONFIRMED en la hipótesis deja intacto
`sys_clinical_history`: las alergias conocidas requieren la entrada explícita
correspondiente en ese bloque. `sys_historySource` indica origen de información
común y no es una regla automática para diagnosticar alergias. No se implementan
interpretaciones de pruebas ni recomendaciones terapéuticas.

Los estados de información menstrual no condicionan campos por sexo ni imponen
exámenes o datos reproductivos. No hay seguimiento por embarazo. No se agregan
condiciones incompatibles con v1: `visibleWhen` admite control BOOLEAN, no
comparaciones nuevas por SELECT. Los textos y estados siguen siendo editables;
su pertinencia clínica requiere revisión profesional.

SP-03D reutiliza las mismas claves comunes de motivo, evolución, examen, estudios,
evaluación y plan. Sus campos específicos son:

| Formulario / especialidad | Claves | Tipo y características |
| --- | --- | --- |
| Ficha Neurología | `neurologicPersonalHistory`, `neurologicFamilyHistory`, `priorNeurologicEvents`, `priorNeurologicTreatmentsAndProcedures`, `neurologicNotes` | TEXTAREA opcional; antecedentes específicos, sin otra lista de medicación habitual |
| Consulta Neurología | `symptomOnsetDate` | DATE opcional; dejar vacío si no se conoce, sin estimación automática |
| Consulta Neurología | `symptomTimeline`, `neurologicManifestations`, `functionalImpact` | TEXTAREA opcional; cronología de varios episodios y repercusión descriptiva |
| Ficha Reumatología | `rheumatologicPersonalHistory`, `autoimmuneHistory`, `rheumatologicFamilyHistory`, `priorRheumatologicTreatmentsAndProcedures` | TEXTAREA opcional |
| Consulta Reumatología | `symptomDistribution`, `painAndSymptoms`, `stiffnessDescription`, `extraArticularManifestations`, `functionalImpact` | TEXTAREA opcional; sin índices de actividad ni clasificación |
| Consulta Reumatología | `stiffnessDurationMinutes` | NUMBER opcional con unidad visible min; sin valor inicial, umbrales, interpretación o proyección nueva; cero explícito es distinto de vacío |
| Ficha Traumatología y Ortopedia | `priorInjuries`, `orthopedicProcedures`, `priorFunctionalLimitations`, `musculoskeletalHistory` | TEXTAREA opcional |
| Consulta Traumatología y Ortopedia | `evaluatedRegion`, `eventMechanism`, `painAndFunctionalLimitations` | TEXTAREA opcional; `evaluatedRegion` admite describir varias regiones |
| Consulta Traumatología y Ortopedia | `laterality`, `injuryEventDate` | SELECT opcional con RIGHT, LEFT, BILATERAL, UNKNOWN, NOT_APPLICABLE, sin selección inicial; DATE opcional si se conoce la fecha |
| Ficha Geriatría | `geriatricPersonalHistory`, `geriatricLivingSituation`, `supportNetwork`, `baselineFunction`, `informationSourceNotes` | TEXTAREA opcional; descripción habitual y fuente de información, sin inferencia por edad |
| Ficha Geriatría | `informantName`, `informantRelationship`, `informantContact` | TEXT opcional; otra persona, separada de la identidad protegida del paciente; no crea cuenta, invitación, permiso ni representación legal |
| Consulta Geriatría | `functionalChanges`, `mobilityAndFalls`, `cognitiveObservations`, `moodObservations`, `nutritionNotes`, `supportNeeds`, `visitInformationSource` | TEXTAREA opcional, descriptivo y neutral; ningún dato vacío equivale a deterioro, dependencia o respuesta negativa |

Las fechas de inicio y evento son datos de la consulta, sin cálculo de duración ni
recordatorios nuevos. Cuando son desconocidas, la narrativa puede indicarlo sin
introducir una fecha ficticia. El registro de seguimiento crea otra consulta y
conserva los valores y snapshots anteriores; no hay un modelo longitudinal nuevo.
`functionalImpact` comparte significado entre Neurología y Reumatología, y
`laterality` comparte opciones con Otorrinolaringología; cada contexto persiste
su propia definición y valores. No se modifica ninguna plantilla anterior.

En Geriatría, `informationSourceNotes` y `visitInformationSource` describen quién
informa y en qué contexto; `sys_historySource` conserva su significado de origen
de los antecedentes comunes. No son mecanismos de autenticación, consentimiento
ni autorización. Un informante puede seguir sin acceso aunque tenga una cuenta
médica independiente. Revisar con profesionales qué datos de contacto son pertinentes
y cómo minimizar información de terceros.

SP-03F completa las diecinueve definiciones técnicas (tres iniciales en el seed y
dieciséis provisionales). Clínica Médica es una copia independiente del contenido
publicado de Medicina General, con campos clínicos opcionales, bloque común y
`followUpInstructions` canónico; no hay herencia dinámica ni cambios en Medicina
General. Las cuatro nuevas mantienen los módulos AI_ASSISTANT y GROWTH_CHARTS
desactivados y reutilizan MEDICATION_CATALOG.

| Formulario / especialidad | Claves propias o derivadas | Tipo y características |
| --- | --- | --- |
| Ficha Clínica Médica | `surgicalHistory`, `hospitalizations`, `familyHistory`, `smoking`, `alcohol`, `physicalActivity`, `nutrition`, `sleep`, `vaccinationHistory`, `preventiveCare`, `generalClinicalNotes` | TEXTAREA opcional; contenido derivado de Medicina General, sin diferencias clínicas inventadas |
| Consulta Clínica Médica | `relevantHistory`, `assessment`, `studies`, `referrals` | TEXTAREA opcional; conserva motivo/evolución, examen, diagnóstico y plan compartidos |
| Consulta Clínica Médica | `bloodPressure`, `heartRate`, `temperature`, `oxygenSaturation`, `weightKg`, `heightCm` | TEXT para presión; NUMBER para los restantes, con unidades de la definición derivada; todos opcionales, sin umbrales nuevos |
| Ficha Oftalmología | `ocularPersonalHistory`, `ocularProcedures`, `usualVisualCorrection`, `ocularNotes` | TEXTAREA opcional |
| Consulta Oftalmología | `visualAcuityRight`, `visualAcuityLeft` | TEXT opcional; expresión literal por ojo, sin conversión ni valor normal |
| Consulta Oftalmología | `visualAcuityMethodRight`, `visualAcuityMethodLeft`, `visualCorrectionRight`, `visualCorrectionLeft` | TEXT opcional; método/notación/distancia y corrección ligados al registro |
| Consulta Oftalmología | `intraocularPressureRight`, `intraocularPressureLeft` | NUMBER opcional, unidad visible mmHg; un ojo puede quedar vacío |
| Consulta Oftalmología | `tonometryMethodRight`, `tonometryMethodLeft`, `ocularMeasurementConditions` | TEXT, TEXT y TEXTAREA opcionales; sin interpretación automática |
| Ficha Psiquiatría | `psychiatricPersonalHistory`, `priorPsychiatricTreatments`, `psychosocialContext` | TEXTAREA opcional; la medicación habitual permanece en el bloque común |
| Consulta Psiquiatría | `mentalStatusExam`, `dailyFunctioning`, `treatmentResponse`, `safetyAssessment` | TEXTAREA opcional, descriptivo; el examen mental no se denomina `physicalExam`; sin puntuación, inferencia de peligrosidad ni protocolo automático |
| Ficha Obstetricia | `obstetricPersonalHistory`, `priorPregnancies`, `obstetricProceduresAndHistory` | TEXTAREA opcional |
| Consulta Obstetricia | `pregnancyReference`, `documentedGestationalAge`, `gestationalAgeSource` | TEXT opcional; datos documentados en cada consulta, sin cálculo gestacional ni identificación relacional de embarazo |
| Consulta Obstetricia | `obstetricRelevantDate`, `gestationalAgeReferenceDate`, `obstetricDateNotes` | DATE, DATE y TEXTAREA opcionales; las fechas desconocidas quedan vacías; el texto describe su significado/fuente |

Todas reutilizan los campos comunes cuando coinciden en significado. En Psiquiatría
la evaluación mental conserva una clave propia. Los campos específicos se muestran
en su registro/snapshot, sin asumir proyección al resumen o impresión. Las cuatro
plantillas no incluyen fórmulas ni respuestas clínicas preseleccionadas.

Oftalmología requiere aprobación profesional de formatos, métodos, distancias,
correcciones y unidades antes de uso real. Psiquiatría usa únicamente el modelo
existente de propietario/contexto: ocultar un campo no crea notas reservadas ni
permisos por sección. Los datos administrativos de agenda/secretaría, oferta pública,
constancia y correo pueden revelar la especialidad, según el comportamiento actual.
El resumen y los documentos incluyen las proyecciones clínicas correspondientes.
La política de esas exposiciones requiere revisión antes de la habilitación real.

Obstetricia A no garantiza agrupación, seguimiento estructurado ni cierre por
embarazo. Cada visita conserva su referencia, expresión gestacional, fuente y
fechas; otra visita no actualiza esas respuestas. FIRST_VISIT sigue siendo la
primera atención de la ficha, no de cada embarazo. No se introduce entidad ni FK.

Todos los nuevos campos clínicos son inicialmente opcionales, sin respuestas
negativas/normalidad, rangos, fórmulas, calculadoras ni recomendaciones automáticas.
Se mantienen las restricciones técnicas de identidad y del ciclo de primera
consulta. En antecedentes comunes, marcar PRESENT exige el detalle correspondiente;
esto es coherencia de captura, no un criterio diagnóstico. No se agregan nuevas
condiciones de visibilidad; cada médico puede usar las condiciones BOOLEAN de v1.
Puede agregar/quitar campos editables, ocultar, reordenar, cambiar etiquetas/opciones
y ajustar obligatoriedad permitida mediante el editor actual. Cada contexto guarda
su configuración independiente; una publicación posterior no la reemplaza.

Los campos específicos quedan en `formValues` y snapshots, visibles en su registro.
No se proyectan automáticamente al resumen clínico. El resumen usa las claves
comunes reconocidas por `internal/wizard/form.go`, antecedentes comunes y los
medicamentos registrados mediante recetas. Saturación y estudios narrativos no
se convierten en datos longitudinales estructurados.

El objeto `createSpecialty` es el cuerpo para `POST /api/doctorchart/admin/specialties`.
El alta deja un borrador inactivo. `experience` contiene los valores de navegación,
pestañas y acciones para las operaciones administrativas, bajo `/api/doctorchart`.
Los destinos son
`PUT /admin/specialties/{id}/experience`, y los sufijos `/patient-record-tabs`
y `/patient-record-actions`. Cada operación usa el `specialtyVersion` vigente como
`expectedVersion`. La publicación se hace desde el editor o con
`POST /admin/specialties/{id}/wizard/publish`, usando la versión del borrador.

Para verificar SP-03B en un laboratorio aislado, ejecutar desde el backend
`node ops/testing/specialty-m6-lab.mjs prepare --sp03b`, seguido del mismo comando
con las operaciones `build` y `up`. En Angular, compilar con
`npx ng build --configuration m6-lab` y ejecutar
`node scripts/specialty-contexts-m6-real.cjs --sp03b`.
El recorrido ejecuta M6 y después carga, publica, personaliza, republica y desactiva
las cuatro plantillas por la API del laboratorio. Antes de desactivar verifica las
siete especialidades activas en una misma cuenta y el aislamiento de sus registros.
Para SP-03C usar `prepare --sp03c` y, tras `build`, `up` y la compilación Angular,
`node scripts/specialty-contexts-m6-real.cjs --sp03c`. Reutiliza el mismo recorrido
parametrizado, ejecuta M6 y las ocho plantillas provisionales, y comprueba once
especialidades activas en una sola cuenta antes de desactivar las provisionales.
Incluye las regresiones SP-02B y SP-03B en esa ejecución conjunta, sin duplicar
scripts. Los horarios de las ocho especialidades ocupan bloques separados en la
agenda del sábado. La evidencia se escribe en `browser/sp03c-results.json`.
Para SP-03D usar `prepare --sp03d`, luego `build`, `up`, build Angular de laboratorio
y `node scripts/specialty-contexts-m6-real.cjs --sp03d`. El mismo coordinador incluye
M6 y las doce plantillas provisionales, y prueba quince contextos en una cuenta.
Las ocho primeras provisionales usan bloques del sábado y las cuatro nuevas usan
domingo, evitando solapamientos en la agenda global. La evidencia se escribe en
`browser/sp03d-results.json`. Se ejecutan las regresiones SP-02B, SP-03B y SP-03C
mediante sus funciones compartidas, sin scripts duplicados. Las pruebas de Geriatría
comparan cuentas/contextos/permisos antes y después de cargar al informante y
verifican que la cuenta médica indicada como contacto no pueda leer la ficha ni
el resumen clínico ni acceder a asignaciones de secretaría.
Para SP-03F usar `prepare --sp03f`, `build`, `up`, el build Angular `m6-lab` y
`node scripts/specialty-contexts-m6-real.cjs --sp03f`. Reutiliza M6 y las dieciséis
provisionales, comprueba diecinueve contextos y después desactiva las provisionales
con conservación del historial. Los bloques se distribuyen entre sábado y domingo.
Las cuatro nuevas se prueban primero para detectar antes problemas específicos;
también se comparan las configuraciones al incorporar cada una de las anteriores.
La evidencia queda en `browser/sp03f-results.json`. La comprobación de Psiquiatría
incluye una reserva pública ficticia, constancia con código y un correo real de
Mailpit a `@local.test`, sin destinatarios externos. El resto del recorrido usa la
misma batería de publicación, personalización, documentos, agenda y autorización.
Para repetir por separado Cardiología, reinicializar únicamente el laboratorio
propio con `reset-e2e` y ejecutar el recorrido con `--sp02b`. Los recorridos requieren
una base de ensayo vacía al inicio. `go-gates 17` ejecuta build, vet y tests contra
PostgreSQL 17 descartable. En Angular ejecutar `npx ng test --watch=false`; con
Node 26 usar `NODE_OPTIONS=--no-experimental-webstorage` en el proceso para que
el entorno DOM de tests gestione su almacenamiento. Después,
`node ops/testing/specialty-m6-lab.mjs down` retira sus
contenedores y bases descartables.

La evidencia se conserva en `%TEMP%/doctorchart-specialty-m6-20261110/browser`.
Auth y Billing usan el adaptador sintético del laboratorio; Angular, backend,
gateway, PostgreSQL, convertidor de adjuntos y Mailpit son reales. La teleconsulta
comprobada es agenda/reserva y navegación contextual, no una videollamada real.

## Compatibilidad histórica de Medicina General

Las plantillas nuevas usan `followUpInstructions`. SP-04F reconoce `followUp`
en los snapshots históricos cuando el campo es válido y visible y el snapshot
carece de la clave canónica. El resumen expone el resultado en
`lastEncounter.followUp`. Las publicaciones y respuestas existentes se
conservan. El [informe SP-04F](../docs/clinical-projections-sp04f.md) describe
la precedencia y las verificaciones.

## Revisión clínica y alcance pendiente

La aprobación médica debe revisar lenguaje, suficiencia del formulario, unidades,
uso de antecedentes personales/familiares y pertinencia de cada medición. El estado
provisional es documental: el catálogo actual no implementa una barrera de
habilitación clínica por ese estado. Publicar en una instancia real requiere una
decisión independiente tras esa revisión.

No se añaden mediciones repetidas, gráficos, interpretación de estudios, escalas ni
un sistema de procedimientos. Permanecen como mejoras futuras sujetas a diseño.
Los permisos actuales se aplican por médico/contexto, sin permisos nuevos por
campo sensible. Revisar si el uso clínico futuro necesita medidas adicionales de
privacidad antes de ampliar antecedentes sexuales/reproductivos. La creación de
una barrera formal de aprobación clínica es una decisión de producto pendiente,
fuera de estas tandas. El registro de un cuidador tampoco resuelve la autorización
de terceros: cualquier acceso futuro exige un diseño y una decisión independientes.

La deuda transversal de auditoría clínica, conservación, trazabilidad y acceso o
exportación integral se documenta en
[preparación clínica/productiva pendiente](../docs/clinical-production-readiness-debt.md).
La validación técnica de diecinueve especialidades no constituye aprobación clínica
ni preparación para producción. No iniciar tareas posteriores sin nuevas instrucciones.
