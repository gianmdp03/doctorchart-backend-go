# DoctorChart — Informe SP-03C

Fecha: 16/11/2026. Alcance: Urología, Otorrinolaringología, Ginecología y Alergia
e Inmunología. Las cuatro propuestas son `PROVISIONAL_TEST_ONLY`, pendientes de
revisión profesional. La implementación técnica no habilita su uso clínico real.

## 1. Especialidades implementadas

Cuatro archivos completos e independientes en `templates/`, con códigos estables
`urology`, `otolaryngology`, `gynecology` y `allergy_immunology`. El último sigue
la convención de palabras en inglés separadas por guion bajo. Conservan el wrapper
de las plantillas anteriores: estado documental fuera de `WizardDefinition`,
`createSpecialty` y `experience`. No se agregan al seed ni se publican en instancias
existentes. No se incorpora un sistema nuevo de aprobación clínica.

## 2. Contenido provisional

| Plantilla | Ficha | Consulta |
| --- | --- | --- |
| Urología | Antecedentes urológicos, procedimientos/cirugías, familia y observaciones | Motivo/evolución, síntomas urinarios y otros pertinentes, dolor/localización, examen genitourinario narrativo y estudios |
| Otorrinolaringología | Antecedentes otológicos/nasales/faríngeos, procedimientos, exposiciones y familia | Motivo/evolución, región evaluada, lateralidad, examen de oído/nariz/garganta y estudios auditivos/vestibulares/imágenes |
| Ginecología | Antecedentes ginecológicos/reproductivos pertinentes, procedimientos, anticoncepción y datos menstruales opcionales | Motivo/evolución, síntomas, antecedentes de la consulta, examen narrativo y estudios; sin seguimiento obstétrico |
| Alergia e Inmunología | Antecedentes atópicos/familiares, exposiciones, episodios previos e inmunológicos | Episodio/evolución, posible desencadenante, relación temporal, manifestaciones, certeza de la hipótesis, examen y pruebas revisadas |

Todas reutilizan identidad y `sys_clinical_history` y agregan evaluación,
diagnóstico, tratamiento e indicaciones/fecha de seguimiento. Los nuevos campos
clínicos son opcionales, sin respuestas preseleccionadas, fórmulas, alertas ni
interpretaciones automáticas. Tipos usados: TEXT, TEXTAREA, SELECT y DATE.
`templates/README.md` contiene todas las claves y opciones.

Lateralidad distingue RIGHT, LEFT, BILATERAL, UNKNOWN y NOT_APPLICABLE; vacío
permanece sin respuesta. La información menstrual distingue KNOWN, UNKNOWN,
NOT_REVIEWED y NOT_APPLICABLE; no condiciona campos por sexo ni impone exámenes.
Urología no agrega antecedentes sexuales o reproductivos en este piloto.

`suspectedExposure` y `allergicHypothesisCertainty` describen la evaluación actual,
sin convertirse en alergias conocidas. Incluso la opción CONFIRMED de la hipótesis
es independiente del bloque común: registrar una alergia conocida requiere una
entrada explícita del profesional en `sys_clinical_history`.

## 3. Reutilización

Catálogo dinámico y publicación administrativa; validadores Go y componentes
Angular v1; personalización por médico/contexto; borradores, fichas, consultas y
snapshots; resumen; recetas, órdenes/PDF y Estudios; agendas presenciales/virtuales,
reservas, enlace a ficha y autorización. Las claves comunes se verificaron contra
`internal/wizard/form.go`: `chiefComplaint`, `presentIllness`, `physicalExam`,
`diagnosis`, `treatment`, `followUpInstructions` y `followUpDate`.

## 4. Archivos y código

Cuatro JSON nuevos, ampliación de `templates/README.md` y este informe. Se extiende
`internal/wizard/cardiology_template_test.go`, el recorrido compartido
`scripts/cardiology-sp02b.cjs`, su coordinador `scripts/specialty-batch-sp03b.cjs`
y el runner M6. `ops/testing/specialty-m6-lab.mjs` acepta `prepare --sp03c`.
No se agregan tablas, endpoints, dependencias, renderizadores ni listas funcionales
fijas. Los códigos de las pruebas son fixtures, no fuentes del catálogo.

## 5. Validación ejecutada

| Comprobación | Resultado |
| --- | --- |
| `go test ./internal/wizard -run TestProvisionalSpecialtyTemplates -count=1` | PASS; ocho plantillas y casos específicos |
| `node ops/testing/specialty-m6-lab.mjs go-gates 17 race` | PASS; Go 1.27.3, formato, build, vet y suite completa con `-race` sobre PostgreSQL 17 descartable |
| `npx ng test --watch=false` | PASS; 864 tests en 106 archivos, sin variación respecto del baseline |
| `npx ng build --configuration m6-lab` | PASS |
| Playwright `--sp03c`, incluyendo M6, Cardiología y SP-03B | PASS; 93 grupos y 778 llamadas API verificadas; once especialidades coexistiendo en una cuenta |

Angular se probó con `NODE_OPTIONS=--no-experimental-webstorage` para que el entorno
DOM de tests gestione el almacenamiento en Node 26. No se cambió código por ello.

El recorrido compartido comprueba por especialidad publicación, selección/cambio
de contexto, ficha y campos propios, primera consulta y seguimiento sin mediciones,
personalización, borrador recuperable, republicación sin reemplazar configuraciones,
snapshots, resumen, recetas/órdenes/PDF/adjuntos, agenda presencial y virtual,
reservas/enlace a ficha, aislamiento entre médicos/contextos, restricciones de
secretaría y desactivación con conservación de registros. La segunda cuenta recibe
la publicación v2 mientras la primera conserva su configuración derivada de v1.

La ejecución conjunta incorpora las ocho provisionales a las tres iniciales,
compara todas las configuraciones anteriores tras cada alta y prueba rechazos
cruzados con los once contextos. Las regresiones anteriores se ejecutan mediante
las mismas funciones parametrizadas; no se duplican scripts.

Casos específicos: lateralidad desconocida/no aplicable en consultas distintas;
estados menstruales explícitos y antecedentes reproductivos vacíos, sin inferencia
por sexo; exposición sospechada y posterior hipótesis marcada CONFIRMED sin alta
automática en alergias comunes; registro manual posterior de alergia confirmada,
con resumen y snapshot inicial intacto. Go prueba además todas las opciones de
lateralidad, rechazo de valores inválidos, los cuatro estados menstruales y las
tres certezas, y formularios de consulta vacíos sin respuestas implícitas.

La evidencia está en `%TEMP%/doctorchart-specialty-m6-20261110/`, con logs prefijados
`sp03c-`, `sp03c.log` y `browser/sp03c-results.json`. El primer intento fallido se
conserva por separado en `sp03c-attempt1.log`.

## 6. Regresiones

M6 pasó sus 18 grupos históricos, incluido el enlace a Dermatología y teleconsulta.
No se identificaron nuevas regresiones del producto en la ejecución completa.
La primera prueba unitaria de ficha omitía `referenceDate`, requerido por el
contrato existente para validar fecha de nacimiento; se corrigió el fixture.
El primer recorrido Playwright también asumía valor HTML vacío para un SELECT
sin respuesta. Angular representa su opción nula como `0: null`; la prueba ahora
comprueba la opción visible «Seleccionar». No se cambiaron validadores,
componentes ni reglas funcionales para resolver esos fallos de pruebas.

## 7. Limitaciones y deuda

Los campos particulares se guardan en valores y snapshots; no aparecen de forma
automática en el resumen clínico. Los antecedentes comunes y claves reconocidas
mantienen sus proyecciones. No hay nuevas mediciones repetidas, gráficas, escalas,
interpretación de pruebas ni seguimiento por embarazo. Medicina General y su deuda
histórica `followUp`/`followUpInstructions` permanecen sin cambios.

Los permisos siguen siendo por médico/contexto, sin nuevos permisos por campo
sensible. Medidas adicionales de privacidad y una barrera formal de aprobación
clínica requieren decisión de producto independiente.

Ejecutado con Angular, Go, gateway, PostgreSQL, convertidor de adjuntos, almacenamiento
local y Mailpit reales, con Auth y Billing sintéticos. No se verifican proveedores
productivos de identidad/facturación, almacenamiento S3 externo, videollamada real,
despliegue ni uso clínico. La teleconsulta probada es agenda/reserva contextual.

## 8. Revisión médica pendiente

Validar suficiencia, terminología, opciones, pertinencia y minimización de datos
sensibles, límites entre hipótesis y alergia confirmada, antecedentes específicos
y problemas actuales, y alcance ambulatorio. No se crean criterios diagnósticos ni
recomendaciones terapéuticas. El estado provisional es documental y no constituye
una barrera de autorización del catálogo.

## 9. Estado de las 19 especialidades

| Estado técnico | Especialidades | Cantidad |
| --- | --- | --- |
| Iniciales existentes | Pediatría, Medicina General, Dermatología | 3 |
| Provisionales anteriores | Cardiología, Endocrinología, Gastroenterología, Neumonología | 4 |
| Provisionales SP-03C | Urología, Otorrinolaringología, Ginecología, Alergia e Inmunología | 4 |
| Pendientes | Clínica Médica, Obstetricia, Traumatología y Ortopedia, Neurología, Psiquiatría, Oftalmología, Reumatología, Geriatría | 8 |

Total: once desarrolladas técnicamente, ocho de ellas provisionales; ocho pendientes.
No se realizan commits, pushes, despliegues ni cambios en bases ajenas al laboratorio.
SP-03D queda fuera de alcance.
