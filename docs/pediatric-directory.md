# Categorías de edad del directorio pediátrico

Se aplican únicamente cuando el workspace del usuario pertenece a la especialidad
con código `pediatrics` (comparación sin distinguir mayúsculas). No dependen del
nombre editable ni del módulo de crecimiento. La especialidad se consulta en la
misma transacción de lectura que los pacientes; el cliente no puede elegirla por
parámetros.

| Filtro | Edades incluidas |
|---|---|
| NEONATAL | Desde el nacimiento hasta antes de cumplir 28 días |
| INFANT | Desde los 28 días hasta antes del primer cumpleaños |
| EARLY_CHILDHOOD | Desde el nacimiento hasta antes del noveno cumpleaños |
| CHILD | Desde el primer cumpleaños hasta antes del décimo cumpleaños |
| ADOLESCENT | Desde el décimo cumpleaños hasta antes del cumpleaños 20 |
| ADULT | Desde los 20 años, como clasificación clínica del directorio pediátrico |

Primera infancia y niñez son filtros superpuestos. Para la etiqueta de cada
paciente se priorizan, en orden: recién nacido, lactante, primera infancia, niñez,
adolescente y adulto. Por ejemplo, a los 2 años y 3 meses se muestra primera
infancia; a los 9 años, niñez. Las fechas futuras se clasifican como UNKNOWN.

La OMS describe lactantes menores de un año (incluyendo neonatos), adolescencia
de 10 a 19 años y desarrollo en la primera infancia de 0 a 8 años. Para el uso en
este directorio se separan los neonatos del filtro de lactantes y se interpreta
0–8 como edades cumplidas (antes del noveno cumpleaños). Esta es una decisión
operativa explícita, no una tabla universal de etapas clínicas de la OMS.

Fuentes:

- [OMS: salud neonatal](https://www.who.int/health-topics/newborn-health).
- [OMS: definiciones de edad en guías clínicas (2025, «Definitions of key terms»)](https://iris.who.int/bitstream/handle/10665/381892/9789240111608-eng.pdf?sequence=1).
- [OMS: salud adolescente](https://www.who.int/health-topics/adolescent-health/).
- [OMS: desarrollo en la primera infancia](https://cdn.who.int/media/docs/default-source/mca-documents/child/early-child-development/improving-early-childhood-development-who-guideline-summary.pdf).

Fuentes contrastadas el 22/10/2026. La definición de niñez de 1 a menos de 10 años
proviene del glosario de una guía clínica de la OMS; no implica que «niño» tenga
esa única definición en todos los programas de la organización.

## Fechas, consultas y otras especialidades

Se usa el día local de la consulta según la zona del usuario; si no la envía, se
conserva la zona predeterminada existente. La comparación se hace con fechas de
nacimiento, no redondeando meses. En fechas de corte de años se ajusta el día al
último válido del mes, incluyendo el 29 de febrero.

Los mismos rangos `(nacimiento posterior al corte, nacimiento hasta el corte]`
se usan para la etiqueta y como parámetros de SQL. Se filtra antes de contar y
paginar. No se guardan ni actualizan categorías en los pacientes.

Las otras especialidades conservan sus clasificaciones y filtros existentes,
incluidos los límites legales de 18 años para ADULT/MINOR. Los filtros nuevos
exclusivamente pediátricos se rechazan fuera de esa especialidad. La adolescencia
clínica hasta los 19 no altera permisos, consentimiento ni mayoría de edad.
Al cambiar de cuenta en el frontend se restablece a Todos cualquier filtro
exclusivamente pediátrico, para evitar enviarlo a otra especialidad.

## Despliegue y verificación

Actualizar primero el backend DoctorChart y luego su frontend. El frontend nuevo
necesita que el backend reconozca los filtros nuevos. No hay cambios de esquema,
migraciones ni reinicializaciones de bases. Para revertir, publicar primero el
frontend anterior y después el backend anterior.

Pruebas: `TestPediatricAgeBoundaries`, `TestPediatricLeapDayAndOverlappingFilters`
y `TestPediatricDirectoryFiltersAndSpecialtyIsolation` en `internal/clinical`.
La integración usa exclusivamente una base local descartable con opt-in
`DOCTORCHART_INTEGRITY_TEST_DATABASE_URL`; nunca apuntarla a una base real.
El smoke de navegador del frontend (`scripts/verify-route-hydration.mjs`) también
recorre el directorio, comprueba los cinco filtros y la etiqueta de primera
infancia, incluyendo anchos de 1280 y 390 píxeles con datos ficticios.

Verificación del 22/10/2026:

- Frontend: 74 archivos y 666 pruebas aprobadas; build de producción aprobado
  con clave pública ficticia. Smoke aprobado en Chrome, con respuestas inmediatas
  y demoradas, incluyendo el directorio en escritorio y móvil.
- Backend: `go build ./...`, `go vet ./...` y `go test ./...` aprobados con
  Go 1.27.2, la versión existente del módulo. Las tres pruebas pediátricas,
  incluida integración y conservación de filtros de otras especialidades,
  aprobadas contra PostgreSQL 17 en un contenedor descartable propio, eliminado
  después del ensayo. No se usaron bases de desarrollo ni productivas.
- `-race` no disponible en el host Windows porque CGO está deshabilitado.
  `govulncheck ./...` detectó GO-2026-6809 en la biblioteca estándar de Go 1.27.2,
  corregida en 1.27.3. La actualización del toolchain queda fuera de este cambio.
