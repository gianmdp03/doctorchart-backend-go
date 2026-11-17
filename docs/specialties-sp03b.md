# DoctorChart — Informe SP-03B

Fecha: 16/11/2026. Alcance: Endocrinología, Gastroenterología y Neumonología.
Clínica Médica y SP-03C quedan fuera de esta entrega.

## 1. Implementación técnica

Se incorporan tres archivos reutilizables en `templates/`, con códigos estables
`endocrinology`, `gastroenterology` y `pulmonology`. Siguen el formato de Cardiología:
`status: PROVISIONAL_TEST_ONLY` fuera de la definición v1, cuerpo `createSpecialty`
y configuración `experience`. Su publicación es explícita y se ensayó exclusivamente
con datos ficticios en el laboratorio propio. No se agregan al seed.

## 2. Contenido clínico provisional

| Especialidad | Ficha | Consulta |
| --- | --- | --- |
| Endocrinología | Antecedentes endocrinológicos personales/familiares, procedimientos/cirugías y observaciones | Motivo, evolución, cambios desde el control anterior, peso (kg), talla (cm), examen, laboratorio/imágenes revisados y evaluación |
| Gastroenterología | Antecedentes digestivos, cirugías/procedimientos, familia y hábitos | Motivo, evolución, localización/características de molestias, tránsito intestinal, relación con alimentación cuando corresponda, examen abdominal/otros hallazgos y estudios revisados |
| Neumonología | Antecedentes respiratorios, tabaquismo/exposiciones, familia y procedimientos/tratamientos previos | Motivo, síntomas/evolución, saturación opcional con unidad visible %, soporte de oxígeno informado, examen y estudios funcionales/imágenes revisados |

Las tres consultas incluyen diagnóstico, tratamiento, indicaciones de seguimiento
y fecha de próximo control. Se reutilizan identidad protegida y antecedentes
comunes de alergias, problemas actuales y medicación habitual. Todos los nuevos
campos clínicos son opcionales, sin respuestas preseleccionadas ni interpretación
automática. La tabla completa de claves y tipos está en `templates/README.md`.

## 3. Infraestructura reutilizada

Catálogo y publicación administrativa, esquema/validación Go v1, editor y renderer
Angular, contextos por médico, fichas/consultas, borradores, snapshots, resumen,
recetas/órdenes e impresión, adjuntos de Estudios, agendas presenciales/virtuales,
reservas y autorización existente. Los campos específicos se guardan en el formulario
y su snapshot; solo las claves comunes reconocidas alimentan sus proyecciones.
`oxygenSaturation` es un valor personalizado, sin nueva columna ni cálculo.

## 4. Código adicional

Tres JSON y documentación. Se parametriza el test Go de Cardiología y su recorrido
Playwright; un coordinador de pruebas SP-03B comprueba coexistencia y desactivación.
`prepare --sp03b` reutiliza las cuentas sintéticas del laboratorio SP-02B. No se
agregan endpoints, tipos de campo, renderizadores, listas funcionales fijas, tablas
ni cambios del motor. Cada configuración persiste de forma independiente.

## 5. Validación

Compilación Go, `go vet` y toda la suite Go: PASS con Go 1.27.3 y PostgreSQL 17
descartable. Angular: 864 tests en 106 archivos, PASS; build `m6-lab`: PASS.
En Node 26 se desactivó Web Storage experimental únicamente para ejecutar los tests
con el almacenamiento del entorno DOM; no se modificó código por ese ajuste.

Cardiología SP-02B independiente: PASS, 11 grupos y 61 llamadas API verificadas.
Validación conjunta M6 + Cardiología + SP-03B: PASS, 51 grupos y 396 llamadas API
verificadas. Incluye los 18 grupos históricos M6 y las tres nuevas especialidades
conviviendo con las cuatro anteriores en la misma cuenta.

Los recorridos por especialidad comprueban alta/publicación, contexto, ficha,
primera consulta, seguimiento sin mediciones, personalización completa y borrador,
republicación sin reemplazar la configuración, snapshots anteriores, recetas,
órdenes/PDF, adjuntos, resumen, reservas presenciales/virtuales, enlace a ficha,
aislamiento entre médicos/contextos, desactivación con historial conservado y
restricciones de secretaría. La cuenta del segundo médico recibe la publicación
v2 mientras la primera conserva su configuración basada en v1.

La prueba conjunta conserva simultáneamente las siete especialidades y compara
las configuraciones anteriores después de cada incorporación, incluida la de
Endocrinología. Luego verifica los rechazos cruzados y desactiva cada plantilla
provisional sin perder fichas, consultas, configuración ni turnos vinculados.

Angular, Go, gateway, PostgreSQL, convertidor y Mailpit son reales. Auth/Billing
usan el adaptador sintético. Teleconsulta se refiere a agenda/reserva, sin probar
una videollamada real. La evidencia y logs permanecen en
`%TEMP%/doctorchart-specialty-m6-20261110/`.

## 6. Regresiones

M6 completó sus 18 grupos, incluido «Abrir ficha de Dermatología» y agenda virtual.
No se identificaron nuevas regresiones del producto. Durante la parametrización
se corrigieron entradas de las pruebas que enviaban campos inexistentes o quitados;
se mantiene la comprobación de rechazo de esos campos. No se cambió el motor.

## 7. Limitaciones y deuda

El resumen no incorpora automáticamente campos personalizados. No se añaden
mediciones repetidas, gráficos, escalas, interpretación de laboratorio/espirometría
ni un sistema nuevo de procedimientos. Los informes se conservan en Estudios.
El problema histórico de Medicina General (`followUp` frente a la proyección
`followUpInstructions`) queda documentado por separado en `templates/README.md`;
no se modifica su seed ni se reescriben registros.

## 8. Habilitación clínica real

Pendiente de aprobación profesional: suficiencia de campos, etiquetas, unidades,
opciones, separación entre antecedentes específicos y problemas actuales, y uso
clínico previsto. La implementación técnica no equivale a habilitación clínica.
El estado provisional es documental y no agrega una barrera al catálogo actual.
No se publica ni habilita automáticamente ninguna plantilla en una instancia real.

## 9. Estado de las 19 especialidades

| Estado técnico | Especialidades | Cantidad |
| --- | --- | --- |
| Iniciales existentes, regresión comprobada | Pediatría, Medicina General, Dermatología | 3 |
| Plantilla provisional SP-02B | Cardiología | 1 |
| Plantillas provisionales SP-03B | Endocrinología, Gastroenterología, Neumonología | 3 |
| Pendientes | Clínica Médica, Ginecología, Obstetricia, Traumatología y Ortopedia, Neurología, Psiquiatría, Urología, Oftalmología, Otorrinolaringología, Reumatología, Alergia e Inmunología, Geriatría | 12 |

Total: siete desarrolladas técnicamente (cuatro provisionales), doce pendientes.
Clínica Médica conserva pendiente su diferenciación de Medicina General.
Sin commits, pushes ni despliegues; sin modificaciones de bases existentes ajenas
al laboratorio. No se avanza a SP-03C.
