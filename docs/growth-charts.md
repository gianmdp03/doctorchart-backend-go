# Curvas pediátricas OMS — implementación y verificación

Referencia de presentación: las 20 láminas de las 11 páginas de `curvas_oms.pdf`, suministrado por el usuario. Fuente científica: libros originales publicados por la OMS; no se extrajeron valores de los píxeles del PDF. Importación fijada el 09/10/2026.

## 1. Arquitectura implementada

Angular 22.1 / TypeScript 6 conserva la ficha del paciente y su pestaña Evolución y Curvas. Un único `GrowthChartComponent` genera SVG con `d3-scale` y `d3-shape`; Angular controla el DOM, los eventos y la accesibilidad. El catálogo declarativo contiene 20 definiciones independientes, siete indicadores y dos sexos. Chart.js se conserva para las calculadoras existentes, que siguen desactivadas.

Go 1.27.2 centraliza el catálogo, los parámetros LMS, la selección científica, el IMC y la evaluación en `internal/growth`. `internal/clinical/growth.go` adapta las consultas PostgreSQL existentes. No se agregó DDL ni se cambiaron registros, consultas, definiciones guardadas o snapshots históricos.

El nuevo `GET /api/doctorchart/patients/{id}/growth` entrega fecha de nacimiento, fecha local de evaluación, edad exacta en días, sexo de referencia, observaciones originales y gráficos del sexo elegido, con sus curvas, procedencia, puntos, evaluación y aplicabilidad. Las referencias anteriores se pueden consultar mediante «Consultar también períodos históricos». Cada punto usa su propia fecha y edad. El endpoint anterior `/measurements` continúa disponible; su evaluación deja de inventar sexo, limitar edades o asignar diagnósticos.

`PatientGrowthStore` pertenece a la ficha: cancela solicitudes anteriores, comprueba ID y contexto, descarta respuestas tardías y reinicia confirmaciones al cambiar de paciente o usuario. Actualiza la consulta cuando cambia el día de Argentina. Las pestañas y la selección efectiva se derivan de las referencias disponibles.

## 2. Archivos principales

Backend:

- `internal/growth/catalog.go`: inventario y ejes del PDF; selección por dominio científico.
- `internal/growth/reference.go`: carga verificada, LMS, percentiles y curvas.
- `internal/growth/measurement.go`: puntos históricos, posiciones e indicadores derivados.
- `internal/growth/data/`: 18 CSV normalizados, manifiesto, ejemplos independientes y 36 XLSX originales.
- `scripts/import_who_growth.py`: importación reproducible con hashes fijados.
- `cmd/growth-reference/main.go`: exportación científica para pruebas visuales, sin acceder a pacientes.
- `internal/clinical/growth.go`, `patient.go`: integración del dominio con los datos y permisos existentes.
- `internal/growth/growth_test.go`, `internal/clinical/growth_integration_test.go`, `clinical_test.go`: verificación matemática, selección, HTTP y PostgreSQL.

Frontend, en el repositorio vecino `doctorchart-frontend`:

- `src/app/core/models/growth-chart.model.ts` y `core/services/patient-growth.service.ts`: contrato tipado y transporte HTTP.
- `src/app/features/patient-record/patient-growth/`: contexto, navegación, carga, estados y confirmaciones.
- `src/app/features/patient-record/growth-chart/`: componente SVG reutilizable, resumen, tooltip accesible, tabla y ampliación del marco.
- `src/app/features/form-field-editor/`: opción para agregar posición de medición a nuevas consultas.
- `src/app/core/testing/growth-catalog.json`: exportación del backend exclusivamente para pruebas; no es un segundo motor científico en producción.
- `scripts/verify-growth-visuals.mjs` y los tests de gráficos, servicio y ficha: geometría y revisión reproducible en Chromium.

Se retiraron las antiguas constantes OMS del frontend y los seis CSV duplicados de `internal/clinical/who`.

## 3. Inventario completo y ejes de impresión

Los límites de Y incluyen el margen exterior de la cuadrícula del PDF: por ejemplo, algunas láminas empiezan debajo de su primera etiqueta. No son límites fisiológicos ni límites para aceptar una medición. X se expresa en meses para edad, y en centímetros para longitud/estatura. Todas las láminas tienen P3, P15, P50, P85 y P97, con rojo/naranja/verde/naranja/rojo; las mediciones del paciente usan corazones rosas en la referencia femenina y estrellas azules en la masculina, con un marco de 9 × 9 unidades SVG. Las mediciones sin evaluación conservan el símbolo delineado.

El IMC OMS 2006 que abarca ambas etapas identifica longitud (0–2 años) y estatura (2–5 años), señala la transición con una línea discontinua y atenúa la cuadrícula secundaria. Tanto las curvas de referencia como la trayectoria del paciente se separan en esa transición, sin cambiar los valores científicos. La verificación posterior de presentación pasó 560 pruebas de frontend y comprobó en Chromium las 20 referencias, tres vistas con mediciones de prueba y cuatro diseños móviles, incluyendo el IMC.

| N.º | Página | Identificador | Indicador | Sexo | Edad del PDF | X | Y | Revisión visual |
|---|---|---|---|---|---|---|---|---|
| 1 | 1 | wfa06_girls | Peso/edad | Niñas | 0–5 años | 0–60 meses | 1–25 kg | Revisado |
| 2 | 2 | wfa06_boys | Peso/edad | Niños | 0–5 años | 0–60 meses | 1–25 kg | Revisado |
| 3 | 2 | lfa06_girls | Longitud/edad | Niñas | 0–2 años | 0–24 meses | 45–95 cm | Revisado |
| 4 | 3 | lfa06_boys | Longitud/edad | Niños | 0–2 años | 0–24 meses | 45–95 cm | Revisado |
| 5 | 3 | hfa06_girls | Estatura/edad | Niñas | 2–5 años | 24–60 meses | 77–120 cm | Revisado |
| 6 | 4 | hfa06_boys | Estatura/edad | Niños | 2–5 años | 24–60 meses | 80–120 cm | Revisado |
| 7 | 4 | hcfa06_boys | Perímetro cefálico/edad | Niños | 0–5 años | 0–60 meses | 32–54,5 cm | Revisado |
| 8 | 5 | hcfa06_girls | Perímetro cefálico/edad | Niñas | 0–5 años | 0–60 meses | 31–53,5 cm | Revisado |
| 9 | 5 | wfa07_girls | Peso/edad | Niñas | 5–10 años | 60–120 meses | 13–47 kg | Revisado |
| 10 | 6 | wfa07_boys | Peso/edad | Niños | 5–10 años | 60–120 meses | 13–45 kg | Revisado |
| 11 | 6 | hfa07_girls | Estatura/edad | Niñas | 5–19 años | 60–228 meses | 95–180 cm | Revisado |
| 12 | 7 | hfa07_boys | Estatura/edad | Niños | 5–19 años | 60–228 meses | 100–195 cm | Revisado |
| 13 | 7 | wfl06_girls | Peso/longitud | Niñas | 0–2 años | 45–110 cm | 1–23 kg | Revisado |
| 14 | 8 | wfl06_boys | Peso/longitud | Niños | 0–2 años | 45–110 cm | 1–23 kg | Revisado |
| 15 | 8 | wfh06_girls | Peso/estatura | Niñas | 2–5 años | 65–120 cm | 5–29 kg | Revisado |
| 16 | 9 | wfh06_boys | Peso/estatura | Niños | 2–5 años | 65–120 cm | 5–28 kg | Revisado |
| 17 | 9 | bfa06_girls | IMC/edad | Niñas | 0–2 años | 0–24 meses | 9,6–21,4 kg/m² | Revisado |
| 18 | 10 | bfa06_boys | IMC/edad | Niños | 0–5 años | 0–60 meses | 9,6–21,4 kg/m² | Revisado |
| 19 | 10 | bfa07_girls | IMC/edad | Niñas | 5–19 años | 60–228 meses | 12–29,5 kg/m² | Revisado |
| 20 | 11 | bfa07_boys | IMC/edad | Niños | 5–19 años | 60–228 meses | 12–30 kg/m² | Revisado |

Cuadrícula: edad menor de cinco años subdividida por mes; longitud/estatura por 1 cm; peso/edad escolar por mes y etiquetas trimestrales; estatura/IMC escolar por trimestre. Y usa subdivisiones de 1 kg en peso/edad, 1 cm en longitud/estatura infantil, 0,5 cm en perímetro cefálico, 5 cm en estatura escolar, 0,5 kg en peso/longitud y peso/estatura, 0,2 kg/m² en IMC infantil y 0,5 kg/m² en IMC escolar. Ambos lados del eje Y tienen etiquetas. Los años completos se distinguen de los meses dentro del año.

## 4. Fuentes, versiones e importación

Fuentes primarias de los 18 datasets, cada uno con LMS y una tabla independiente de percentiles para niñas y niños:

- [Peso para la edad, patrones infantiles](https://www.who.int/tools/child-growth-standards/standards/weight-for-age): `wfa06`, 1857 filas por sexo, días 0–1856; kg.
- [Longitud/estatura para la edad](https://www.who.int/tools/child-growth-standards/standards/length-height-for-age): `lhfa06`, 1857 filas por sexo; cm; cambio entre días 730 y 731.
- [Perímetro cefálico](https://www.who.int/tools/child-growth-standards/standards/head-circumference-for-age): `hcfa06`, 1857 filas por sexo; cm.
- [IMC, patrones infantiles](https://www.who.int/toolkits/child-growth-standards/standards/body-mass-index-for-age-bmi-for-age): `bfa06`, 1857 filas por sexo; kg/m².
- [Peso/longitud y peso/estatura](https://www.who.int/tools/child-growth-standards/standards/weight-for-length-height): `wfl06`, 651 filas, 45–110 cm; `wfh06`, 551 filas, 65–120 cm; incrementos de 0,1 cm; Y en kg.
- [Peso/edad 5–10 años](https://www.who.int/tools/growth-reference-data-for-5to19-years/indicators/weight-for-age-5to10-years): `wfa07`, 60 filas por sexo, meses 61–120; kg.
- [Estatura/edad 5–19 años](https://www.who.int/tools/growth-reference-data-for-5to19-years/indicators/height-for-age): `hfa07`, 168 filas por sexo, meses 61–228; cm.
- [IMC/edad 5–19 años](https://www.who.int/tools/growth-reference-data-for-5to19-years/indicators/bmi-for-age): `bfa07`, 168 filas por sexo, meses 61–228; kg/m².

`WHO 2006` identifica la familia de patrones infantiles, incluyendo indicadores adicionales publicados después de 2006, como perímetro cefálico. `WHO 2007` identifica la referencia escolar. No se mezclan con CDC. El manifiesto retiene URL exacta del archivo oficial, hoja, coordenada, filas, versión de referencia, fecha de incorporación y SHA-256 del original y del CSV. La OMS denomina `hfa-...` algunos archivos de peso escolar: se conservaron sus URL reales y se comprobó el contenido de la hoja.

```powershell
# Desde doctorchart-backend-go; requiere Python con openpyxl.
python scripts/import_who_growth.py
python scripts/import_who_growth.py --download
go test ./internal/growth -count=1
```

El primer comando reconstruye desde los originales guardados. El segundo descarga de nuevo y rechaza cualquier cambio de hash. Ambos validan columnas, números finitos, M/S positivos, orden, dominio y cantidad de filas. `--initialize` se reservó para la incorporación científica inicial; cambiar hashes requiere revisar las fuentes. La carga del servicio vuelve a comprobar integridad y presencia de los 18 datasets. Los XLSX originales no se incluyen en el binario ejecutable.

## 5. LMS, edades y compatibilidad de medidas

Se usa `z = ((valor/M)^L − 1)/(L·S)`; para `L = 0`, `z = ln(valor/M)/S`. La inversa calcula las cinco curvas. Los cuantiles normales son ±1,8807936081512509 para P3/P97, ±1,0364333894937898 para P15/P85 y 0 para P50. No se confunden con ±1/±2 DE.

Para indicadores basados en peso, fuera de ±3 DE se aplica la extensión OMS con distancia fija entre las curvas de 2 y 3 DE. El percentil numérico se calcula con la CDF normal únicamente dentro de ±3 DE; fuera se comunica un límite `<0,135` o `>99,865`, junto con Z. Se conserva precisión `float64`; el redondeo ocurre al mostrar datos. Véase la [metodología OMS de cálculo](https://cdn.who.int/media/docs/default-source/child-growth/growth-reference-5-19-years/computation.pdf).

La edad cronológica es la diferencia entre fechas civiles, con fecha de consulta en `America/Argentina/Buenos_Aires`. Un mes OMS equivale a 30,4375 días. Los patrones infantiles usan la fila diaria exacta. La referencia escolar interpola linealmente LMS entre filas mensuales, y peso/longitud o estatura entre filas de 0,1 cm. Esta interpolación de LMS y el tratamiento de posición están descritos en el [manual oficial AnthroPlus, apartados 3.2.1 y 3.3.1](https://www.who.int/docs/default-source/child-growth/growth-reference-5-19-years/who-anthroplus-manual.pdf).

- Días 0–730: longitud; desde el día 731: estatura. Se evita superposición para un mismo indicador.
- Días 0–1856: patrones infantiles. Desde el día 1857 y al menos 61 meses OMS: referencia escolar.
- El máximo escolar incorporado es exactamente 120 meses para peso y 228 para estatura/IMC, según el último parámetro publicado en los XLSX elegidos. Una fecha posterior queda fuera; no se arrastra la última fila. Por ejemplo, 6940 días supera 228 meses, aunque el rótulo calendario diga 19 años. Tampoco se extiende hasta 121/229 meses a partir de otra versión del catálogo.
- El marco escolar empieza en el mes 60 como el PDF, pero las curvas empiezan en el 61, donde comienza el archivo oficial. No se inventa el tramo anterior. El pequeño excedente infantil hasta el día 1856 puede quedar fuera del marco impreso de 60 meses; se conserva en el detalle y puede ampliarse el eje.
- Posición documentada `STANDING` antes del día 731: +0,7 cm para longitud; `RECUMBENT` entre días 731–1856: −0,7 cm para estatura. Se conservan valor original, conversión y posición. No se convierte longitud recumbente escolar ni se evalúan conversiones no positivas.
- Si falta posición, se aplica el flujo de medición estándar de DoctorChart: acostado hasta el día 730 y de pie desde el día 731. Es un valor predeterminado de cálculo, no una observación adicional ni una modificación del historial. Los indicadores de talla, IMC y peso/longitud o estatura se calculan sin pedir confirmaciones en la ficha. Una posición explícitamente documentada prevalece sobre el valor predeterminado.
- IMC = kg/(cm/100)². Los dos valores proceden de la misma observación. Peso/longitud o estatura también utiliza exclusivamente esa observación; X es la longitud/estatura, nunca la edad.
- Se rechazan valores no finitos o no positivos y se excluyen fechas futuras o anteriores al nacimiento. Las consultas excluidas no se borran de la base. Las mediciones fuera del dominio científico conservan su valor sin extrapolación ni evaluación.

El sexo automático requiere el campo canónico `sys_sex` (Sexo) y su proyección coherente `gender`, con valor `MALE` o `FEMALE`. Un género antiguo arbitrario o una referencia desconocida requiere confirmación explícita. `referenceSex` sigue siendo una opción de vista; se reinicia al cambiar de paciente/usuario. La ficha no pregunta por la posición. El backend conserva las posiciones previamente documentadas en `heightMeasurementMode` y la opción validada `measurementModes` de la API; ninguna lectura modifica el historial.

Si la respuesta de la API aún entrega posiciones `UNKNOWN`, la ficha suministra automáticamente la posición estándar por edad mediante `measurementModes` y recibe de la API los indicadores recalculados. Las posiciones documentadas no se sobreescriben. La segunda lectura pertenece al mismo paciente y contexto de cuenta, se cancela al cambiar de ficha y no modifica datos ni calcula LMS/IMC en Angular. Cuando la API ya resuelve la posición, se utiliza una sola lectura.

## 6. Pruebas y evidencia

El registro concreto de esta ejecución, hashes y comprobaciones de las veinte vistas está en [growth-verification.json](growth-verification.json). El fixture científico de tests se comprobó ausente de los bundles de producción.

Verificaciones realizadas en Windows con Go 1.27.2, PostgreSQL 17.11 y Chromium de Chrome:

- Importación offline reproducida: 18 pares de libros OMS y verificación de los hashes.
- Comparación con **96 filas de tablas independientes de percentiles**, cinco percentiles por fila: 480 comparaciones; tolerancia absoluta de 0,0011 unidades por precisión de publicación. Inversa LMS/CDF: tolerancia 1e−10 puntos porcentuales.
- Ambos sexos a 1, 6, 12, 23, 24, 25, 48, 60, 72, 120, 132, 180, 228 y 240 meses; bordes exactos 730/731 y 1856/1857 días y máximos de tablas; ausencia de referencias superpuestas o sexo supuesto.
- Interpolación mensual, L=0, valores extremos, posiciones, conversión inválida, medidas faltantes y prohibición de combinar consultas.
- HTTP y PostgreSQL reales en base local desechable: middleware de autenticación, propiedad del paciente, módulo deshabilitado, confirmaciones, fecha de nacimiento futura, medianoche argentina, historia, compatibilidad de `/measurements` y hash de registros antes/después para demostrar que la lectura no altera datos.
- `check_go.py --target go1.27.2 --require-vulncheck`: formato, build, vet, tests, race y análisis de vulnerabilidades ejecutados y aprobados. El escaneo no encontró vulnerabilidades alcanzables ni en paquetes importados; informa un aviso en el módulo indirecto `golang.org/x/crypto/openpgp`, que esta aplicación no importa.
- Suite activa Angular: **558 tests aprobados en 58 archivos**, excluyendo solamente el módulo de calculadoras desactivado y preservado. Incluye aislamiento entre pacientes/usuarios y sus confirmaciones, revisión de una referencia confirmada, actualización al cruzar medianoche argentina, errores sin datos ficticios, fechas, pestañas y transporte HTTP.
- Pruebas de los 20 componentes parametrizados: SVG real, geometría D3, colores, clip-path, dominios, vacío sin inventar puntos, interacción por foco, ampliación del marco y discontinuidad de IMC a los 730/731 días.
- Chromium: **20 capturas de escritorio y 3 de móvil**, con comprobación de cinco curvas, etiquetas dentro del SVG, ausencia de NaN/Infinity y ausencia de desbordamiento de documento. Las 20 láminas se inspeccionaron visualmente contra el PDF y también las tres vistas móviles. En móvil, la gráfica se desplaza horizontalmente para mantener legibilidad.
- Build Angular de producción aprobado; informa advertencia de presupuesto del bundle inicial. `npm install` informó 21 avisos de dependencias del proyecto, que no se corrigieron mediante una actualización general fuera de este módulo.
- Pruebas de configuración pública: 3 aprobadas. Pruebas del servidor de producción y sus hosts permitidos: 3 aprobadas.

Las pruebas emplean fixtures aislados. La aplicación obtiene exclusivamente los datos clínicos persistidos por el servicio. No se desplegó ni se operó una base clínica existente. Las pruebas de HTTP utilizan los handlers y el middleware del backend, no un despliegue completo del gateway JWT externo.

## 7. Diferencias y límites clínicos explícitos

El PDF no contiene IMC femenino de 2–5 años, aunque la OMS sí publica esos parámetros. Se conservó el catálogo exacto de 20, se informa ese vacío y no se agregó una lámina 21 silenciosamente. La lámina masculina de IMC 0–5 tiene una etiqueta naranja superior repetida como «97»; se muestra correctamente P85, verificándolo con la tabla oficial. El cambio de longitud a estatura en IMC conserva su discontinuidad científica.

Las mediciones históricas siguen completas en la base y en su ficha; esta vista solo representa las compatibles con sus referencias. «Sin registros», «sin evaluación», omitidas y fuera de marco se distinguen. No se aplica un estado diagnóstico «Normal» o «Adecuado». No existe una política validada de edad corregida ni un dato estructurado suficiente de edad gestacional: se utiliza edad cronológica y se informa esta limitación. Tampoco se deriva un diagnóstico nutricional o se pretende sustituir una evaluación de edema u otras circunstancias clínicas.

## 8. Ejecutar y reproducir la revisión

Backend: usar la configuración local habitual documentada en el README y ejecutar `go run ./cmd/api`. El nuevo endpoint está bajo los mismos permisos del gateway y del workspace. No requiere migración SQL. Los datasets se cargan del binario sin acceso a Internet durante una consulta.

Frontend: `npm install` y `npm start`, con la configuración pública habitual apuntando al gateway que enruta al backend actualizado. Entrar a Pacientes → ficha → Evolución y Curvas. Confirmar referencia o posición cuando la información antigua resulte insuficiente. Agregar posición opcional desde Personalizar fichas para registrarla en futuras historias clínicas.

```powershell
# Backend: pruebas matemáticas y calidad.
go test ./internal/growth -count=1
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...

# Integración: exclusivamente base PostgreSQL LOCAL desechable con este nombre.
$env:DOCTORCHART_GROWTH_TEST_DSN = 'postgres://postgres@127.0.0.1:54329/doctorchart_growth_test?sslmode=disable'
go test ./internal/clinical -run TestGrowthHTTPPostgres -v -count=1
# El test crea su propio schema, usa las tablas de referencia y lo elimina al salir.

# Exportar fixture científico si se cambian catálogo o datos.
go run ./cmd/growth-reference | Set-Content ../doctorchart-frontend/src/app/core/testing/growth-catalog.json -Encoding utf8

# Desde doctorchart-frontend.
npx ng test --watch=false --exclude='src/app/features/medical-calculators/**'
npx ng build --configuration production

# Visuales: Chromium instalado con Playwright, o Chrome existente.
npx playwright install chromium
npm run test:growth:visual
# Alternativa con Chrome: $env:PLAYWRIGHT_CHROME_CHANNEL = 'chrome'
# Genera test-results/growth/*.html, *.png y checks.json; revisar contra curvas_oms.pdf.
```

Si no se define `DOCTORCHART_GROWTH_TEST_DSN`, ese test de integración informa SKIP; no equivale a haber probado PostgreSQL. El runner visual produce geometría del componente Angular real y referencias OMS sin mediciones de un paciente ficticio. Los PNG son evidencia de pruebas y no se utilizan como gráficos en la aplicación.
