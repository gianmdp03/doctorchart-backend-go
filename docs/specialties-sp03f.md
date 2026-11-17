# DoctorChart — Informe SP-03F

Fecha: 16/11/2026. Alcance autorizado: piloto técnico con datos ficticios para
Clínica Médica, Oftalmología, Psiquiatría y Obstetricia A. No implica aprobación
clínica, suficiencia regulatoria ni preparación para producción.

## 1. Especialidades implementadas

Se crean `templates/internal_medicine.provisional.json`,
`templates/ophthalmology.provisional.json`, `templates/psychiatry.provisional.json`
y `templates/obstetrics.provisional.json`. Cada archivo es completo e independiente,
con código estable, definición v1, experiencia y marca PROVISIONAL_TEST_ONLY fuera
del esquema estricto. No se agregan al seed ni se publican en bases ajenas al lab.

## 2. Contenido y estructura

| Plantilla | Ficha | Consulta |
| --- | --- | --- |
| Clínica Médica | Identificación, antecedentes comunes, cirugías, internaciones, familia, hábitos, prevención y observaciones | Motivo/evolución, antecedentes pertinentes, signos vitales opcionales, examen/evaluación, diagnóstico, estudios, derivaciones y plan |
| Oftalmología | Antecedentes oculares, procedimientos, corrección habitual y observaciones | Agudeza visual TEXT por ojo; método/notación/distancia y corrección; presión NUMBER en mmHg y método por ojo; condiciones, examen, estudios y plan |
| Psiquiatría | Antecedentes psiquiátricos, tratamientos previos y contexto psicosocial | Motivo/evolución, mentalStatusExam, funcionamiento, respuesta terapéutica, safetyAssessment, evaluación, diagnóstico y plan; sin physicalExam ficticio |
| Obstetricia A | Antecedentes obstétricos, embarazos previos y procedimientos | pregnancyReference, fechas conocidas, documentedGestationalAge TEXT, fuente/fecha de información, evolución, hallazgos, estudios, diagnóstico y seguimiento |

Todos los campos clínicos nuevos son opcionales, sin respuestas preseleccionadas,
conversiones, fórmulas, umbrales de normalidad, alertas ni protocolos inventados.
Clínica Médica deriva del contenido publicado de Medicina General, sin modificar
esa especialidad. Utiliza followUpInstructions y followUpDate canónicos.

Oftalmología conserva expresiones literales y condiciones por ojo, y acepta
mediciones unilaterales. Psiquiatría mantiene el examen mental con significado
propio. Obstetricia conserva referencia y datos gestacionales por consulta, sin
entidad pregnancy, agrupación garantizada, cierre ni seguimiento estructurado por
embarazo. FIRST_VISIT sigue siendo la primera atención de la ficha.

Las claves y tipos completos se documentan en `templates/README.md`.

## 3. Reutilización

Catálogo dinámico, publicación administrativa, validadores Go v1, renderer/editor
Angular, personalización por médico, contextos, fichas/consultas, borradores,
snapshots, resumen, recetas/órdenes/PDF/Estudios, agendas y reservas presenciales o
virtuales. La teleconsulta probada es una modalidad de agenda, sin videollamada.
Las cuatro incluyen sys_clinical_history sin listas paralelas de alergias,
medicación habitual o problemas actuales. Las claves comunes conservan sus
proyecciones; no se supone que cualquier campo propio aparece en resumen o PDF.

## 4. Cambios realizados

Cuatro JSON, README, este informe y un documento separado de deuda previa a uso
clínico/producción. Se amplía el test Go parametrizado a dieciséis plantillas y
se corrige la comprobación de límites a minimum/maximum. La proyección física se
prueba cuando el campo existe y el examen mental tiene su comprobación propia.

Se amplían los runners compartidos y `prepare --sp03f`. La personalización de las
pruebas elige un campo narrativo real y una sección de prueba, sin exigir que
physicalExam exista o esté en una categoría aislada. Un helper específico de
privacidad de Psiquiatría comprueba canales de salida y Mailpit; no duplica el
recorrido clínico. No se modifican tablas, endpoints, renderizadores, tipos,
autorización funcional ni el motor de formularios.

## 5. Validación ejecutada

| Comprobación | Resultado |
| --- | --- |
| Test Go de plantillas | PASS; dieciséis definiciones, claves, opcionalidad, unidades y casos particulares |
| Go build, vet y suite con PostgreSQL 17 y -race | PASS; Go 1.27.3, laboratorio descartable |
| Angular unit tests | PASS; 865 tests en 106 archivos |
| Build Angular m6-lab | PASS |
| Playwright SP-03F, con M6 y regresiones SP-02B, SP-03B, SP-03C y SP-03D | PASS. 176 grupos y 1.794 llamadas API verificadas |

La diferencia de Angular frente a 864 es un test preexistente en el checkout que
prueba la recuperación del calendario de vacunación mediante «Reintentar carga».
Ese archivo fue modificado antes de iniciar SP-03F; no se agregaron ni eliminaron
tests Angular en esta etapa. Se utilizó NODE_OPTIONS=--no-experimental-webstorage
para el entorno DOM de tests de Node 26, sin cambiar la aplicación por ello.

El recorrido nuevo prueba primero las cuatro incorporaciones y luego las doce
provisionales anteriores, todas en la misma cuenta/instancia, comparando cada
configuración precedente. Se mantienen las funciones compartidas de alta,
publicación, contextos, fichas, primera consulta/seguimiento, personalización,
borradores, republicación, valores/snapshots originales, resumen, documentos y
adjuntos, agenda/reservas, aislamiento, secretaría y desactivación con historial.
Las dieciséis agendas provisionales usan bloques separados de sábado y domingo.
Las diecinueve especialidades coexistieron en la misma cuenta. Las dieciséis
desactivaciones conservaron fichas, consultas, configuraciones y turnos vinculados.
El incremento frente a SP-03D fue de 41 grupos y 555 llamadas API, por los cuatro
recorridos nuevos, los casos específicos y la matriz de aislamiento ampliada.

Casos nuevos comprobados: independencia de Clínica Médica y sus claves canónicas;
ojo derecho solo y ojo izquierdo solo, métodos/correcciones y mmHg; Psiquiatría
con examen mental real, control de actores y contenido de salidas; dos referencias
de embarazo para una ficha, fechas desconocidas, expresión gestacional sin cálculo,
conservación de snapshots y rechazo de segunda FIRST_VISIT.

La evidencia se conserva en `%TEMP%/doctorchart-specialty-m6-20261110/`, con los
logs `sp03f-*`, `sp03f.log` y `browser/sp03f-results.json` con resultado PASS.

## 6. Regresiones

M6 completó sus 18 grupos en el checkout actual, incluida la navegación a
Dermatología, reservas públicas y agenda virtual. Pasaron las regresiones de las
doce provisionales anteriores. La comparación SHA-256 confirmó que sus archivos y
el seed son idénticos a los del inicio. La ejecución completa no detectó nuevas
regresiones del producto. La compatibilidad histórica de `followUp` sigue pendiente.

## 7. Privacidad verificada en Psiquiatría

| Canal / actor | Datos observados y restricciones |
| --- | --- |
| Propietario, detalle clínico | Recibe los valores de mentalStatusExam y safetyAssessment del registro original, con contexto propio; ocultar en una personalización posterior no los convierte en notas reservadas |
| Otros médicos y visitante público | La batería comprueba rechazos clínicos; no se agrega acceso compartido |
| Agenda del propietario | Identidad/contacto del turno, especialidad y vínculo patientId cuando corresponde; sin copiar narrativas clínicas al booking |
| Secretaría asignada | Identidad/contacto, horario, estado, consultorio y especialidad; sin patientId, ficha, consulta, resumen ni PDF clínico; se comprueban rechazos reales |
| Oferta y reserva pública | Se puede publicar/seleccionar Psiquiatría; contiene metadatos administrativos; reservar no crea una ficha clínica |
| Constancia con código | Profesional, consultorio, especialidad y datos administrativos del turno; sin ficha o narrativas clínicas |
| Correo real de Mailpit | El cuerpo incluye Psiquiatría y referencia administrativa; no incluye las narrativas clínicas de prueba; destinatario exclusivamente @local.test |
| Resumen del propietario | Diagnóstico, seguimiento, antecedentes y medicación proyectados; no incorpora automáticamente mentalStatusExam o safetyAssessment |
| Impresión del propietario | La prueba inspecciona el JSON de impresión y comprueba que se genera un PDF. El JSON incluye especialidad, diagnóstico, indicaciones y medicación explícitos. Las narrativas de examen mental y seguridad no figuran en el documento que usa el generador |

Las pruebas usan marcadores clínicos ficticios para comprobar su ausencia en los
canales administrativos. Los datos del resumen y del documento se verifican como
salidas clínicas autorizadas. La publicación pública temporal del ensayo se restaura
al terminar. No se envían avisos a destinatarios externos.

La exposición administrativa permitida de especialidad se documenta como riesgo
a revisar antes del uso real, no como una filtración de notas clínicas bajo los
permisos actuales. No hay notas reservadas, permisos por campo/sección ni un nuevo
modelo de acceso compartido. No se hacen cambios generales de privacidad.

## 8. Limitaciones y aprobación clínica

Pendiente de revisión profesional: suficiencia de las cuatro consultas, alcance de
Clínica Médica, notaciones/métodos/distancias/correcciones oftalmológicos y mmHg,
documentación psiquiátrica de seguridad, fechas/datos gestacionales y pertinencia
de campos. Las opciones vacías no significan normalidad, respuesta negativa ni
información evaluada. Ninguna especialidad incorpora recomendaciones automáticas.

Auth/Billing son adaptadores sintéticos. Angular, Go, gateway, PostgreSQL,
convertidor, almacenamiento local y Mailpit son reales. No se verifican servicios
productivos de identidad/facturación, S3 externo, videollamada, suficiencia clínica
ni operación productiva. La marca provisional no es una barrera de autorización.

## 9. Deuda pendiente

Se registra aparte en `docs/clinical-production-readiness-debt.md`: auditoría
clínica completa, conservación, trazabilidad, correcciones/borrado, autenticidad,
acceso/entrega de copias integrales de historia clínica, privacidad de terceros y
política de canales sensibles. Los snapshots y la versión optimista no acreditan
por sí solos esas garantías. La compatibilidad de followUp de Medicina General
queda fuera de este milestone. No se declara cumplimiento normativo mediante tests.

## 10. Estado final de las diecinueve especialidades

Tres iniciales: Pediatría, Medicina General y Dermatología.
Dieciséis provisionales: Cardiología, Endocrinología, Gastroenterología,
Neumonología, Urología, Otorrinolaringología, Ginecología, Alergia e Inmunología,
Neurología, Reumatología, Traumatología y Ortopedia, Geriatría, Clínica Médica,
Oftalmología, Psiquiatría y Obstetricia.

Las diecinueve tienen definiciones técnicas; las dieciséis posteriores requieren
aprobación clínica y evaluación previa a producción. No se realizan commits,
pushes, despliegues ni cambios en bases ajenas al laboratorio. No se inician
tareas posteriores sin nuevas instrucciones.
