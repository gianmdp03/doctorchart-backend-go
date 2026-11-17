# SP-04F · Compatibilidad y proyecciones clínicas

Los datos de consulta pueden tener dos representaciones. `form_values` contiene las respuestas originales y el snapshot establece su significado y visibilidad. Las columnas clínicas guardan proyecciones que sirven a las búsquedas y otros recorridos. En Medicina General, el campo histórico `followUp` quedaba en las respuestas, pero la columna de instrucciones, el resumen y la impresión podían omitirlo. Además, varios nombres de campo podían escribir el mismo destino según su orden en la definición. La vista de revisiones mezclaba proyecciones antiguas con respuestas sin evaluar la visibilidad.

## Interpretación y precedencia

- Go elige el campo del snapshot antes de revisar su valor. Usa `followUp` si el snapshot carece de `followUpInstructions` y el campo histórico es de texto, válido y visible. Si el snapshot contiene el campo canónico, respeta el valor guardado, incluido el vacío. Un campo oculto, ausente o inválido carece de proyección.
- Las definiciones iniciales que se instalan en bases nuevas usan el campo canónico. El bootstrap conserva las publicaciones y personalizaciones ya guardadas. Los formularios históricos se evalúan con su snapshot. La fecha de seguimiento se sigue proyectando por separado y se conserva en una corrección cuando el snapshot no representa ese dato. Las indicaciones de una receta u orden independiente conservan su propio origen.
- Para colisiones históricas, los campos de sistema prevalecen, incluido el nombre completo derivado de nombre y apellido protegidos. Luego se prefieren las claves canónicas y las claves que coinciden con el destino de la proyección. Los empates entre alias restantes se resuelven por clave en orden léxico. Esta precedencia describe una interpretación técnica. Los valores contradictorios siguen disponibles en los datos que se registraron.
- Go devuelve `CLINICAL_PROJECTION_COLLISION` y la ruta del campo que repite un destino en una definición nueva. La validación cubre identificación, cobertura, antecedentes familiares, tutor, motivo, examen, diagnóstico, tratamiento, seguimiento y mediciones. Los campos personalizados con destinos diferentes se mantienen independientes.

## Lectura, corrección y conservación

La misma resolución alimenta detalle y listas de consultas, resumen e impresión. Las consultas anuladas quedan fuera de las vistas vigentes. El resumen sigue aplicando la visibilidad de cada snapshot cuando combina antecedentes y distingue entre una negación explícita y un valor vacío. La lectura no crea revisiones. Cuando hay una corrección solicitada, el historial conserva el actor, la fecha y el contenido anterior. Las respuestas históricas desconocidas se conservan también al corregir un registro.

Una nueva emisión usa la consulta interpretada. Las reimpresiones sirven los bytes conservados y verifican su SHA-256. El ZIP sigue incluyendo los snapshots, respuestas originales y revisiones, con estados de anulación y manifiesto verificable. En las secciones de ficha, consultas y revisiones, `derivedProjections` identifica la interpretación calculada a partir del snapshot correspondiente. Angular muestra esa interpretación en la revisión y mantiene el bloque de datos guardados. Los permisos y eventos de auditoría se aplican en las rutas existentes.

La etapa mantiene el baseline 20. No requiere DDL, backfill ni regeneración de documentos. Las pruebas usan datos sintéticos en PostgreSQL descartable.

Las columnas persistidas conservan su función en filtros SQL y agregados existentes, como las series de crecimiento. La interpretación corregida de consultas, resúmenes, impresiones y revisiones se calcula en lectura. Las diferencias históricas quedan trazables al comparar el dato original con la vista derivada.

## Rendimiento del resumen

Se midieron 16 combinaciones de 10, 100, 1.000 y 10.000 consultas, uno o tres snapshots y 0% o 50% de anulaciones. Cada combinación incluyó calentamiento y cinco mediciones del servicio Go con PostgreSQL 18. La comparación empieza con la corrección de compatibilidad aplicada y mide el efecto de reutilizar snapshots y la consulta de historial. El trazador cuenta sentencias SQL sin conservar sus parámetros. La memoria de la tabla es asignación acumulada por llamada. Los logs incluyen también el cambio de heap observado y los extremos de tiempo. El ensayo excluye HTTP, auditoría y preparación de los datos.

| Consultas | Snapshots | Anuladas | Mediana inicial | Mediana final | Asignación inicial → final |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 1 | 0% | 35 ms | 62 ms | 1,03 → 0,79 MB |
| 100 | 1 | 0% | 67 ms | 54 ms | 5,63 → 3,08 MB |
| 1.000 | 1 | 0% | 383 ms | 232 ms | 51,70 → 25,96 MB |
| 10.000 | 1 | 0% | 3.762 ms | 1.567 ms | 512,57 → 254,71 MB |
| 10.000 | 3 | 0% | 4.519 ms | 1.606 ms | 513,08 → 254,78 MB |
| 10.000 | 3 | 50% | 2.400 ms | 822 ms | 256,81 → 127,67 MB |

El resumen pasó de 13 a 12 sentencias, incluidas las transaccionales, y de 11 a 10 SELECT en todos los tamaños. La última consulta surge del mismo recorrido ordenado del historial. Una caché por solicitud guarda hasta ocho snapshots decodificados. Los demás se decodifican cuando aparecen. Se visitan todas las consultas vigentes y cada escenario verifica el antecedente más antiguo y la última consulta vigente. Los tiempos de los escenarios pequeños variaron, con tres aumentos de mediana sobre cuatro casos de diez consultas.

El costo sigue creciendo con el historial activo. Si las métricas de una etapa de operación muestran presión en fichas extensas, conviene medir latencia y memoria bajo concurrencia y con más variantes de snapshots antes de considerar índices, cambios de modelo o proyecciones persistidas. Mantener una prueba de equivalencia que incluya antecedentes antiguos, correcciones y anulaciones.

Para reproducir el experimento en la base de integridad del laboratorio, ejecutar `go test ./internal/clinical -run '^TestClinicalSummaryPerformance$' -count=1 -v` con `DOCTORCHART_SUMMARY_BENCHMARK=1` y las variables de fixture que usa M6. La salida `SP04F_SUMMARY` se conserva en `%TEMP%/doctorchart-specialty-m6-20261110/sp04f-summary-before.log` y `sp04f-summary-after.log`. La primera corrida del fixture de medición se corrigió porque enviaba una fecha que el endpoint de creación no acepta.

## Verificación ejecutada

- Go 1.27.4 pasó build, vet, la suite con PostgreSQL 17, la suite con detector de carreras en PostgreSQL 18 y las pruebas HTTP finales de compatibilidad sobre la base descartable. Se verificaron siete combinaciones de valores, lectura sin escrituras clínicas, corrección y anulación, aislamiento, auditoría, texto de nuevas emisiones, igualdad de bytes de un PDF anterior y manifiesto del ZIP.
- Angular pasó 876 pruebas en 107 archivos y los builds de desarrollo y laboratorio. La primera corrida sin el ajuste de Node 26 recibió `localStorage` sin inicializar y tuvo timeouts con compilaciones concurrentes. La corrida final usó `NODE_OPTIONS=--no-experimental-webstorage` y dos workers de Vitest y Angular.
- Playwright focal pasó siete grupos y 19 llamadas API. Incluyó detalle histórico y canónico, vaciado y visibilidad, revisión, rechazo al consultar desde otra especialidad y rechazo de una nueva definición con colisión de identidad desde la API administrativa.
- Playwright completo pasó 192 comprobaciones y 1.928 llamadas API con las 19 especialidades, M6 y las regresiones SP-04B/B.1/C/D/E/F. En la primera corrida, el test de reserva pública agotó la espera de 20 segundos antes del POST. La repetición pasó con una espera de 60 segundos para ese paso del test y conservó el flujo real de confirmación. Auth y Billing usaron los adaptadores sintéticos del laboratorio. Angular, API, Gateway, PostgreSQL, conversión y Mailpit se ejecutaron como servicios reales.
- `govulncheck` informó cero vulnerabilidades alcanzables y cero en paquetes importados. El aviso de módulo requerido `GO-2026-5932` de OpenPGP sigue registrado en el informe SP-04E.

Los resultados de navegador se conservan en `%TEMP%/doctorchart-specialty-m6-20261110/browser/sp04f-results.json` y `sp04f-only-results.json`. El SHA-256 del baseline SQL coincide con el artefacto conservado en SP-04E.
