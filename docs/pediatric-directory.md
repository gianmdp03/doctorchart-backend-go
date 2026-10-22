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
| EARLY_CHILDHOOD | Desde el primer cumpleaños hasta antes del quinto cumpleaños (1–4 años) |
| CHILD | Desde el quinto cumpleaños hasta antes del décimo cumpleaños (5–9 años) |
| ADOLESCENT | Desde el décimo cumpleaños hasta antes del cumpleaños 20 |
| ADULT | Desde los 20 años, como clasificación clínica del directorio pediátrico |

Los filtros de edad no se superponen ni dejan huecos: cada fecha de nacimiento
válida pertenece a una única categoría. El filtro y la etiqueta usan exactamente
los mismos límites. Un paciente de 2 años aparece en primera infancia; a partir
del quinto cumpleaños aparece en niñez. Las fechas futuras se clasifican como
UNKNOWN. Todos y Vacunación pendiente no son categorías de edad.

La página de etapas del desarrollo de la OMS separa primera infancia (1–4 años)
y niñez posterior (5–9 años). El directorio usa esa división, el período neonatal
menor de 28 días y la adolescencia de 10–19 años. Los menores de un año se dividen
en recién nacidos y lactantes posneonatales para que sus filtros sean exclusivos.

Fuentes:

- [OMS: salud neonatal](https://www.who.int/health-topics/newborn-health).
- [OMS: recursos por etapas del desarrollo (primera infancia 1–4 y niñez posterior 5–9)](https://www.who.int/initiatives/supporting-parents/who-publications).
- [OMS: salud adolescente](https://www.who.int/health-topics/adolescent-health/).

Fuentes contrastadas el 22/10/2026. Se trata de las etapas elegidas para este
directorio, no de una definición única de «niño» en todos los programas de la OMS.

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

Publicar de forma coordinada el backend DoctorChart y su frontend, primero el
backend. Ambos deben usar los mismos rangos; una interfaz nueva no actualiza los
filtros de un backend anterior. No hay cambios de esquema,
migraciones ni reinicializaciones de bases. Para revertir, publicar primero el
frontend anterior y después el backend anterior.

Pruebas: `TestPediatricAgeBoundaries`, `TestPediatricLeapDayAndExclusiveFilters`,
`TestPediatricFiltersPartitionEveryAge` y
`TestPediatricDirectoryFiltersAndSpecialtyIsolation` en `internal/clinical`.
La integración usa exclusivamente una base local descartable con opt-in
`DOCTORCHART_INTEGRITY_TEST_DATABASE_URL`; nunca apuntarla a una base real.
El smoke de navegador del frontend (`scripts/verify-route-hydration.mjs`) también
recorre el directorio, comprueba los cinco filtros y la etiqueta de primera
infancia, incluyendo anchos de 1280 y 390 píxeles con datos ficticios.
Comprueba que el paciente ficticio de 2 años aparezca en primera infancia,
desaparezca de las otras categorías y vuelva a aparecer al recargar el directorio.
Las pruebas HTTP/PostgreSQL verifican ese mismo caso con un paciente sintético
realmente persistido, incluyendo búsqueda, paginación, totales y etiqueta.

En pediatría, una consulta fallida muestra el error de carga y no el mensaje
«No se encontraron pacientes». Una respuesta exitosa vacía sí muestra ese mensaje.

Verificación de los rangos exclusivos (22/10/2026):

- Frontend: 74 archivos y 668 pruebas aprobadas; build de producción aprobado con
  clave pública ficticia para el smoke aislado. Chrome aprobó los cinco filtros,
  la inclusión exclusiva del paciente de 2 años, recarga, escritorio y móvil.
- Backend Go 1.27.3: build, vet y suite Go aprobados; pruebas pediátricas con race
  aprobadas en Linux. Integración HTTP/PostgreSQL 18 aprobada sobre una base
  descartable propia, retirada después del ensayo.
- Se comprueba que cada día de nacimiento hasta 25 años pertenezca a una sola
  categoría para fechas normales y bisiestas, junto con los cortes de cumpleaños.
- Se preservan los filtros y clasificaciones de otras especialidades. No se
  modificaron esquemas ni se consultaron pacientes reales.

Esta corrección es posterior al paquete de Go `go1.27.3-20261022-190524`; ese
archivo conserva la clasificación anterior. Para publicar estos rangos hay que
construir DoctorChart desde esta corrección y publicar también su frontend.
Las pruebas locales no acreditan la versión actualmente desplegada en el VPS.

Verificación histórica anterior a la corrección de rangos exclusivos (22/10/2026):

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
