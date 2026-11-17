# DoctorChart — Informe SP-03D

Fecha: 16/11/2026. Implementación técnica y ensayos con datos ficticios, sin
habilitación clínica real. Las cuatro nuevas plantillas mantienen el estado
documental `PROVISIONAL_TEST_ONLY` y requieren aprobación profesional.

## 1. Especialidades incorporadas

Cuatro JSON completos e independientes: `neurology`, `rheumatology`,
`traumatology_orthopedics` y `geriatrics`. El código compuesto de Traumatología y
Ortopedia sigue las convenciones existentes de inglés y guion bajo, como
`allergy_immunology`. Cada archivo contiene `createSpecialty`, definición v1,
experiencia y estado provisional fuera del esquema estricto de definición.
No se agregan al seed ni se publican en bases ajenas al laboratorio.

## 2. Contenido de los wizards

| Plantilla | Ficha | Consulta |
| --- | --- | --- |
| Neurología | Antecedentes personales/familiares, eventos, tratamientos/procedimientos previos y observaciones | Inicio opcional, cronología de episodios, manifestaciones, repercusión funcional, examen neurológico narrativo y estudios |
| Reumatología | Antecedentes reumatológicos/autoinmunes, familiares y tratamientos/procedimientos previos | Distribución, dolor, rigidez descriptiva y duración opcional en minutos, manifestaciones extraarticulares, función, examen y laboratorio/imágenes |
| Traumatología y Ortopedia | Lesiones previas, procedimientos/cirugías, limitaciones previas y antecedentes musculoesqueléticos | Regiones múltiples narrativas, lateralidad, fecha opcional del evento, mecanismo, evolución, dolor/limitaciones, examen regional y estudios |
| Geriatría | Antecedentes, convivencia, apoyo, función habitual y persona informante/cuidadora, vínculo, contacto y fuente | Cambios funcionales, movilidad/caídas, cognición y ánimo descriptivos, nutrición, apoyo, fuente de la consulta, examen y estudios |

Todas incluyen motivo/evolución, antecedentes comunes, evaluación, diagnóstico,
tratamiento e indicaciones/fecha de seguimiento. Los campos clínicos nuevos son
opcionales y no tienen respuestas preseleccionadas. Se usan TEXT, TEXTAREA, DATE,
SELECT y NUMBER de v1. Las claves completas están en `templates/README.md`.

Fechas desconocidas permanecen vacías; la cronología admite narrativa. Rigidez
usa un número con unidad visible `min`, sin umbrales ni interpretación; vacío y
cero explícito son distintos. Lateralidad admite RIGHT, LEFT, BILATERAL, UNKNOWN
y NOT_APPLICABLE, sin selección inicial. No hay cálculos, escalas ni alertas.

Informante, convivencia y función son descripciones, sin inferencia por edad.
Los datos de otra persona no sustituyen la identidad protegida del paciente ni
crean cuentas, invitaciones, permisos o representación legal.

## 3. Infraestructura reutilizada

Motor/validador Go y editor/renderer Angular v1; catálogo y publicación;
configuraciones por médico/contexto, borradores y snapshots; fichas, consultas,
resumen, recetas/órdenes/PDF, Estudios; agendas presenciales/virtuales, reservas,
enlace a ficha y autorización existente. `sys_clinical_history` mantiene la única
captura común de alergias, medicación habitual y problemas actuales.

Se comprobaron en `internal/wizard/form.go` las proyecciones de `chiefComplaint`,
`presentIllness`, `physicalExam`, `diagnosis`, `treatment`, `followUpInstructions`
y `followUpDate`. Los campos específicos quedan en valores/snapshots y no se
proyectan automáticamente al resumen. La evolución entre visitas se registra en
consultas nuevas; no se crea un modelo longitudinal adicional.

## 4. Archivos y código modificados

Cuatro archivos `templates/*.provisional.json`, README y este informe.
Se amplían el test Go `internal/wizard/cardiology_template_test.go`, el recorrido
`scripts/cardiology-sp02b.cjs`, su coordinador `scripts/specialty-batch-sp03b.cjs`
y el runner `scripts/specialty-contexts-m6-real.cjs`. El laboratorio admite
`prepare --sp03d`. No se agregan dependencias, tablas, endpoints, tipos ni
renderizadores. Los códigos enumerados en pruebas son fixtures, no fuentes del
catálogo funcional. Se verificaron por SHA-256 las ocho plantillas provisionales
anteriores: no cambiaron. Tampoco se modifica el seed de las tres iniciales.

## 5. Resultados de validación

| Comprobación ejecutada | Resultado |
| --- | --- |
| Test Go de plantillas y casos específicos | PASS, doce plantillas |
| `go-gates 17 race` | PASS; Go 1.27.3, formato, build, vet y suite completa con PostgreSQL 17 descartable y `-race` |
| Angular unit tests | PASS; 864 tests en 106 archivos, sin variación respecto del baseline |
| Build Angular `m6-lab` | PASS |
| Playwright `--sp03d`, incluyendo M6, SP-02B, SP-03B y SP-03C | PASS; 135 grupos, 1.239 llamadas API verificadas, quince especialidades coexistiendo |

Angular usa `NODE_OPTIONS=--no-experimental-webstorage` únicamente en el proceso
de tests para utilizar el almacenamiento del entorno DOM en Node 26.

El mismo recorrido verifica los veinte puntos solicitados: catálogo/publicación,
contexto, ficha, primera consulta y seguimiento sin mediciones, personalización y
borrador recuperable, republicación sin reemplazo, snapshots, resumen, documentos
y adjuntos, agendas/reservas/enlace a ficha, aislamiento, desactivación con historial
y acceso restringido de secretaría. Las regresiones anteriores usan sus mismas
funciones, sin scripts duplicados.

Pruebas específicas: fecha inicial desconocida y cronología posterior en
Neurología; minutos visibles, rigidez opcional y cambios funcionales en Reumatología;
varias regiones, lateralidad explícita y fecha opcional en Traumatología; datos
de informante y cambios descriptivos en Geriatría. Se comparan todos los valores
y la definición del primer registro después del seguimiento y la republicación.

En Geriatría se registra como contacto a la segunda cuenta médica del laboratorio:
su autenticación no debe permitirle leer esa ficha o resumen ni obtener acceso
de secretaría. Se comparan cuentas, contextos, accesos, invitaciones y sesiones
antes/después de registrar al informante, verificando ausencia de concesiones.

La prueba conjunta mantiene quince especialidades y quince contextos de un médico,
compara configuraciones después de cada incorporación, verifica rechazos cruzados
y luego desactiva cada provisional conservando sus registros. Las nuevas agendas
se distribuyen entre sábado y domingo para evitar solapamientos del profesional.
El incremento frente a los 93 grupos/778 llamadas de SP-03C es de 42 grupos y 461
llamadas: cuatro recorridos, casos específicos y una matriz de aislamiento más amplia.

Evidencia: `%TEMP%/doctorchart-specialty-m6-20261110/`, logs `sp03d-*`,
`sp03d.log` y `browser/sp03d-results.json`, con resultado PASS.

## 6. Regresiones

M6 completó sus 18 grupos, incluida la navegación a Dermatología y teleconsulta.
En toda la ejecución no se identificaron regresiones del producto ni
fue necesario modificar wizards anteriores o código funcional.

## 7. Limitaciones y deuda

Sin gráficos, modelos de lesiones longitudinales, mapas corporales, escalas,
índices de actividad ni interpretación automática. La historia se consulta por
registros/snapshots y resumen existente; un seguimiento estructurado futuro
requiere necesidad clínica comprobada y diseño independiente.

La protección actual es por médico/contexto, sin permisos específicos por campo
sensible. Registrar cuidador no equivale a consentimiento ni autorización. Revisar
minimización y privacidad de sus datos antes de uso real. El estado provisional
es documental; una barrera formal de aprobación clínica sigue pendiente de producto.
Se conserva la deuda `followUp`/`followUpInstructions` de Medicina General, sin
modificar su plantilla ni reescribir datos.

Las ejecuciones usan Angular, Go, gateway, PostgreSQL, convertidor, almacenamiento
local y Mailpit reales. Auth y Billing son adaptadores sintéticos. No se verifican
servicios productivos de identidad/facturación, S3 externo, videollamada real,
despliegue ni uso clínico. Teleconsulta se refiere a agenda/reserva contextual.

## 8. Aprobación profesional pendiente

Revisar suficiencia, etiquetas, unidades, opciones de lateralidad, pertinencia
de duración de rigidez y fechas, distinción entre antecedentes y problemas activos,
descripción neutral de función/cognición/ánimo, datos necesarios del informante
y alcance ambulatorio. No se definen criterios diagnósticos ni terapéuticos.

## 9. Catálogo de 19 especialidades

| Estado técnico | Especialidades | Cantidad |
| --- | --- | --- |
| Iniciales existentes | Pediatría, Medicina General, Dermatología | 3 |
| Provisionales anteriores | Cardiología, Endocrinología, Gastroenterología, Neumonología, Urología, Otorrinolaringología, Ginecología, Alergia e Inmunología | 8 |
| Provisionales SP-03D | Neurología, Reumatología, Traumatología y Ortopedia, Geriatría | 4 |
| Pendientes | Clínica Médica, Obstetricia, Psiquiatría, Oftalmología | 4 |

Total: quince desarrolladas técnicamente, doce provisionales y cuatro pendientes.
Sin commits, pushes, despliegues ni modificaciones de bases ajenas al laboratorio.
No se avanza a SP-03E.
